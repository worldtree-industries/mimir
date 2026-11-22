package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/worldtree-industries/mimir/models"
)

// errNoObjectStorage means the client has no object storage configured, so
// it can't read Snapshots and every Config stays on its Defaults.
var errNoObjectStorage = errors.New("no object storage configured")

// errLocationNotNamed means MimirClient's Snapshot at a new location
// doesn't name that location, so the client won't move there.
var errLocationNotNamed = errors.New("MimirClient's Snapshot there doesn't name the location")

// errStorageDown means a Config skipped reading its Snapshot at Start because
// object storage didn't answer the process's last read.
var errStorageDown = errors.New("object storage didn't answer the last read; starting on Defaults")

// Runtime is what every Config in a process shares: the process's Deployable
// and Environment, MimirClient (the Config of the client's own poll
// interval, server URL, object storage endpoint and bucket prefix), object
// storage, and the one loop that polls every Config's Snapshot and registers
// them. MimirClient is therefore read, registered and waited on once per
// process, however many Configs it reads.
//
// Start uses the process's own Runtime unless given one with WithRuntime. A
// Runtime does nothing until the first Config starts in it.
type Runtime struct {
	ctx  context.Context
	opts options

	boot sync.Once
	loop sync.Once

	// Set once at boot.
	logger     *slog.Logger
	http       *http.Client
	values     *mimirClientValues
	self       *configPoll
	local      localValues
	located    bool
	deployable string
	env        string
	openStore  func(ctx context.Context, endpoint string) (models.ObjectReader, error)
	kick       chan struct{}

	// selfDue is when MimirClient is next due to register. The loop's alone.
	selfDue time.Time

	mu sync.Mutex
	// configs are the Configs read in this process, in start order.
	configs []*Client
	// at is the location Snapshots are read from, and store the object
	// storage at its endpoint (nil until it opens). They change only in the
	// loop, once every Snapshot has been read at the location MimirClient
	// names.
	at    location
	store models.ObjectReader
	// answering is whether object storage answered the last read of
	// MimirClient's Snapshot.
	answering bool
}

// NewRuntime returns a Runtime for the options, whose background work runs
// until ctx is done. Only options about the process apply to it: object
// storage, clock, logger, environment and build path. Pass it to Start with
// WithRuntime. Most programs never need one: Start shares the process's own.
func NewRuntime(ctx context.Context, opts ...Option) *Runtime {
	return &Runtime{ctx: ctx, opts: newOptions(opts), kick: make(chan struct{}, 1)}
}

// processRuntime is the Runtime every Start without WithRuntime shares. It
// is made from the options of the first such Start and runs for the life of
// the process.
//
//nolint:gochecknoglobals // one Runtime per process is the point
var processRuntime struct {
	mu sync.Mutex
	rt *Runtime
}

func sharedRuntime(opts []Option) *Runtime {
	processRuntime.mu.Lock()
	defer processRuntime.mu.Unlock()

	if processRuntime.rt == nil {
		processRuntime.rt = NewRuntime(context.Background(), opts...)
	}

	return processRuntime.rt
}

// start adds the Config def to the Runtime, booting the Runtime first if it
// is the first, and returns its Client.
func (rt *Runtime) start(ctx context.Context, def Definition, onEvent func(Event)) *Client {
	rt.boot.Do(rt.bootstrap)

	c := &Client{ctx: ctx, onEvent: onEvent}
	logger := rt.logger.With("config", def.Config)
	c.poll = newConfigPoll(def, logger, c.emit)
	c.poll.takeLocalValues(ctx, rt.local)
	c.poll.applyStart(ctx)
	if !rt.located {
		logger.Log(ctx, rt.localLevel(), "Can't determine the Namespace; running on Defaults and Local Values only")
		return c
	}
	c.poll.ns = models.Namespace{Config: def.Config, Deployable: rt.deployable, Environment: rt.env}

	rt.readAtStart(ctx, c)

	rt.mu.Lock()
	rt.configs = append(rt.configs, c)
	rt.mu.Unlock()

	// The loop registers the first Config before it first waits; later ones
	// it registers when kicked.
	started := false
	rt.loop.Do(func() {
		started = true
		go rt.run()
	})
	if !started {
		select {
		case rt.kick <- struct{}{}:
		default:
		}
	}

	return c
}

// bootstrap works out what the whole process shares: the Deployable and
// Environment, MimirClient's start values, and object storage. It then reads
// MimirClient's Snapshot, waiting at most startupPollTimeout.
func (rt *Runtime) bootstrap() {
	ctx, o := rt.ctx, rt.opts
	rt.logger = o.logger
	rt.http = &http.Client{Timeout: registrationTimeout}
	rt.values = &mimirClientValues{}
	rt.self = newConfigPoll(rt.values.definition(), rt.logger.With("config", MimirClientConfig), rt.reportOwnValue)
	rt.local = readLocalValues(ctx, rt.logger, o.getenv, o.workDir)
	rt.self.startFromEnv(ctx, o.getenv)
	rt.self.takeLocalValues(ctx, rt.local)
	rt.self.applyStart(ctx)
	rt.at = rt.values.wantedLocation()

	ns, err := discoverNamespace(MimirClientConfig, o.getenv, o.buildPath)
	if err != nil {
		rt.logger.Log(ctx, rt.localLevel(),
			"Can't determine the Namespace; every Config runs on Defaults and Local Values only", "error", err)
		return
	}
	rt.located, rt.deployable, rt.env = true, ns.Deployable, ns.Environment
	rt.self.ns = ns
	rt.logger = rt.logger.With("deployable", ns.Deployable, "environment", ns.Environment)
	rt.self.logger = rt.logger.With("config", MimirClientConfig)

	rt.openStore = o.openStore
	if rt.openStore == nil && (s3Configured(o.getenv) || rt.at.endpoint != "") {
		rt.openStore = s3Opener(o.getenv)
	}
	if rt.openStore == nil {
		rt.logger.Log(ctx, rt.localLevel(), "No object storage configured; running on Defaults and Local Values only")
		return
	}

	startupCtx, cancel := context.WithTimeout(ctx, startupPollTimeout)
	defer cancel()
	if err = rt.ensureStore(startupCtx); err != nil {
		rt.logger.ErrorContext(ctx, "Can't open object storage; running on Defaults until it opens", "error", err)
		return
	}
	_, _ = rt.pollSelf(startupCtx, nil)
}

// localLevel is how loudly to log running without a Namespace or object
// storage: normal when running from a checkout with a local config file,
// an error otherwise.
func (rt *Runtime) localLevel() slog.Level {
	if rt.local.found {
		return slog.LevelInfo
	}

	return slog.LevelError
}

// readAtStart reads c's Snapshot once, so the program begins on its real
// values, unless object storage didn't answer the process's last read: then
// c starts on Defaults at once and the next poll brings its values.
func (rt *Runtime) readAtStart(ctx context.Context, c *Client) {
	rt.mu.Lock()
	store, at, answering := rt.store, rt.at, rt.answering
	rt.mu.Unlock()

	switch {
	case store == nil:
		return
	case !answering:
		c.emit(Event{Kind: EventPolled, Err: errStorageDown})
		return
	}

	startupCtx, cancel := context.WithTimeout(ctx, startupPollTimeout)
	defer cancel()
	// If the loop moves to a new location meanwhile, c's ETag is from the
	// old one; an ETag names its contents, so at worst the next poll reads
	// the Snapshot afresh.
	c.emit(Event{Kind: EventPolled, Err: c.poll.poll(startupCtx, store, at.prefix)})
}

// run is the Runtime's loop: it registers what is due, then polls every
// poll interval until rt.ctx is done. The interval is read afresh for each
// wait, so an Override on it takes effect from the next wait on. A kick (a
// new Config) registers what is due without moving the next poll.
func (rt *Runtime) run() {
	ctx := rt.ctx
	rt.registerDue(ctx)
	next := rt.clock().After(rt.values.currentPollInterval())
	for {
		select {
		case <-ctx.Done():
			return
		case <-rt.kick:
			rt.registerDue(ctx)
		case <-next:
			rt.poll(ctx)
			rt.registerDue(ctx)
			next = rt.clock().After(rt.values.currentPollInterval())
		}
	}
}

func (rt *Runtime) clock() models.Clock { return rt.opts.clock }

// live returns the Configs still read, dropping those whose Start context is
// done.
func (rt *Runtime) live() []*Client {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	kept := rt.configs[:0]
	for _, c := range rt.configs {
		if c.ctx.Err() == nil {
			kept = append(kept, c)
		}
	}
	clear(rt.configs[len(kept):])
	rt.configs = kept

	return append([]*Client(nil), kept...)
}

// poll reads MimirClient's Snapshot and then every Config's, and applies
// them. Only object storage is read, never mimir-server (ADR 0002). When
// MimirClient names a new location, the client first tries to read every
// Snapshot there, and moves only if it can; otherwise it logs why and stays
// where it is. Every Config is sent exactly one EventPolled per poll.
func (rt *Runtime) poll(ctx context.Context) {
	configs := rt.live()

	if err := rt.ensureStore(ctx); err != nil {
		if !errors.Is(err, errNoObjectStorage) { // logged once, at boot
			rt.logger.WarnContext(ctx, "Can't open object storage; keeping current values", "error", err)
		}
		for _, c := range configs {
			c.emit(Event{Kind: EventPolled, Err: err})
		}
		return
	}

	moved, selfErr := rt.pollSelf(ctx, configs)
	if moved {
		for _, c := range configs {
			c.emit(Event{Kind: EventPolled})
		}
		return
	}

	for _, c := range configs {
		err := c.poll.poll(ctx, rt.store, rt.at.prefix)
		c.emit(Event{Kind: EventPolled, Err: errors.Join(selfErr, err)})
	}
}

// pollSelf reads MimirClient's Snapshot and, if it names a new location,
// tries to move there with configs. It reports whether the client moved, and
// the read's error.
func (rt *Runtime) pollSelf(ctx context.Context, configs []*Client) (bool, error) {
	err := rt.self.poll(ctx, rt.store, rt.at.prefix)
	rt.mu.Lock()
	rt.answering = err == nil
	rt.mu.Unlock()

	if wanted := rt.values.wantedLocation(); wanted != rt.at {
		if rt.switchTo(ctx, wanted, configs) {
			return true, nil
		}
	}

	return false, err
}

// ensureStore opens object storage at the current endpoint if it isn't
// open yet.
func (rt *Runtime) ensureStore(ctx context.Context) error {
	if rt.store != nil {
		return nil
	}
	if rt.openStore == nil {
		return errNoObjectStorage
	}
	store, err := rt.openStore(ctx, rt.at.endpoint)
	if err != nil {
		return fmt.Errorf("open object storage: %w", err)
	}

	rt.mu.Lock()
	rt.store = store
	rt.mu.Unlock()

	return nil
}

// switchTo moves the client to read from loc, if it can read every
// Snapshot there and MimirClient's Snapshot there names loc too. A missing
// Snapshot of a Config counts, since it means no Overrides; but MimirClient's
// must be there to name loc, unless loc is where its start values already
// point. Otherwise a location where nothing was published would drop every
// Config to its Defaults, and then, having lost the Override that pointed
// there, send the client back again. It reports whether it moved. On failure
// the client keeps its current location, so a bad location never strands it
// where it can't read the fix.
func (rt *Runtime) switchTo(ctx context.Context, loc location, configs []*Client) bool {
	store, err := rt.storeFor(ctx, loc)
	var self fetched
	if err == nil {
		self, err = rt.self.fetch(ctx, store, loc.prefix, "")
	}
	if err == nil {
		if named := namedLocation(self.snap, rt.self, loc); named != loc {
			err = fmt.Errorf("%w: it names %s", errLocationNotNamed, named)
		}
	}
	fetchedConfigs := make([]fetched, len(configs))
	for i, c := range configs {
		if err != nil {
			break
		}
		fetchedConfigs[i], err = c.poll.fetch(ctx, store, loc.prefix, "")
	}
	if err != nil {
		err = fmt.Errorf("switch to %s: %w", loc, err)
		rt.logger.ErrorContext(ctx, "Can't read Snapshots from the new location; keeping the current one",
			"current", rt.at.String(), "error", err)
		for _, c := range configs {
			c.emit(Event{Kind: EventLocationSwitch, Err: err})
		}

		return false
	}

	rt.logger.InfoContext(ctx, "Switched Snapshot location", "from", rt.at.String(), "to", loc.String())
	rt.mu.Lock()
	rt.store, rt.at, rt.answering = store, loc, true
	rt.mu.Unlock()
	rt.self.apply(ctx, self)
	for i, c := range configs {
		c.poll.apply(ctx, fetchedConfigs[i])
		c.emit(Event{Kind: EventLocationSwitch})
	}

	return true
}

// storeFor is the object storage at loc's endpoint: the current store if
// the endpoint is unchanged, or else a new one.
func (rt *Runtime) storeFor(ctx context.Context, loc location) (models.ObjectReader, error) {
	if loc.endpoint == rt.at.endpoint {
		return rt.store, nil
	}
	store, err := rt.openStore(ctx, loc.endpoint)
	if err != nil {
		return nil, fmt.Errorf("open object storage: %w", err)
	}

	return store, nil
}

// reportOwnValue logs a change to one of MimirClient's values. They aren't
// the program's values, so the program isn't sent value events for them;
// rejections are logged where they happen.
func (rt *Runtime) reportOwnValue(e Event) {
	if e.Kind == EventValueChanged {
		rt.self.logger.Info("MimirClient value changed", "item", e.Item.Name, "value", e.Value)
	}
}
