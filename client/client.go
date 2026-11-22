package client

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/dfryer1193/mjolnir/utils/log"

	"github.com/worldtree-industries/mimir/models"
)

// DefaultPollInterval is the Default of MimirClient's poll interval: how
// often a client polls its Snapshots unless tuned.
const DefaultPollInterval = 30 * time.Second

// defaultServerURL is the Default of MimirClient's server URL. It names the
// Service without the cluster domain, so it resolves from any namespace
// through the pod's DNS search path whatever the cluster domain is.
const defaultServerURL = "http://mimir-server.mimir-config"

// startupPollTimeout bounds the first poll, which Start waits for so a
// program begins on its real values.
const startupPollTimeout = 5 * time.Second

// Definition is a Config as the generated code hands it to the client.
type Definition struct {
	// Config is the Config's name.
	Config string
	// DisplayName is the Config's optional short display name.
	DisplayName string
	// Items are the Config's Items, each with the setter that applies values
	// to it.
	Items []ItemDefinition
}

// ItemDefinition is one Item of a Definition, as the generated code declares
// it, with the setter that applies its values.
type ItemDefinition struct {
	Name        string
	Type        models.Type
	Description string
	Default     string
	// Set applies a value to the generated Config's field. It returns an
	// error when the value doesn't parse as the Item's type.
	Set func(value string) error
}

// EventKind says what a background step of the client was.
type EventKind string

// The kinds of Event.
const (
	// EventRegistered is a finished Registration attempt.
	EventRegistered EventKind = "registered"
	// EventPolled is a finished poll of the Snapshot.
	EventPolled EventKind = "polled"
	// EventValueChanged is an Item taking a new value: Value is what its
	// getter now returns, as a string.
	EventValueChanged EventKind = "value_changed"
	// EventLocationSwitch is a finished attempt to read Snapshots from a new
	// location (object storage endpoint or bucket prefix) named by the
	// MimirClient Config. Err is set when it failed and the client kept its
	// current location; it tries again at the next poll.
	EventLocationSwitch EventKind = "location_switch"
	// EventValueRejected is a value that doesn't parse as its Item's type:
	// Value is the rejected value, Err why, and the Item keeps its last good
	// value.
	EventValueRejected EventKind = "value_rejected"
)

// Event reports a step the client took. Err is set when the step failed.
// Item and Value are set only for the value events, EventValueChanged and
// EventValueRejected. A poll's value events come before its EventPolled.
type Event struct {
	Kind  EventKind
	Err   error
	Item  models.ItemRef
	Value string
}

// Client keeps one Config's values current. It is what Start returns; its
// work happens in the background, in the Runtime it shares with the
// process's other Configs, until Start's context is done.
type Client struct {
	ctx     context.Context
	onEvent func(Event)
	poll    *configPoll
	// registrationDue is when the Config is next due to register. Only the
	// Runtime's loop touches it.
	registrationDue time.Time
}

// Start applies the Definition's Defaults and reads the Config's Snapshot
// once, and then, in the background until ctx is done, registers the Config
// with mimir-server (again every DefaultRegistrationInterval) and polls its
// Snapshot every poll interval. Nothing it does in the background can stop
// the program reading values: on any failure the Config keeps its current
// values.
//
// Every Config in a process shares one Runtime (see WithRuntime): the
// Namespace's Deployable and Environment, object storage, and the
// MimirClient Config, which the first Start reads (waiting at most 5
// seconds) and which is registered and polled once for the whole process.
// A later Start reads its own Snapshot only if object storage answered the
// process's last read, so a cold start with storage down waits once, not
// once per Config.
//
// MimirClient starts from environment variables or its Defaults whenever a
// process starts, so an Override that moved a client somewhere bad never
// survives a restart: it is adopted again only once it has been read from
// the starting location.
//
// Run from a checkout of a repository, outside staging and prod, a Config
// first takes its Local Values from the repository's .mimir/config file.
// Each replaces its Item's Default and any Override.
func Start(ctx context.Context, def Definition, opts ...Option) *Client {
	o := newOptions(opts)
	rt := o.runtime
	if rt == nil {
		rt = sharedRuntime(opts)
	}

	return rt.start(ctx, def, o.onEvent)
}

// Namespace is the Namespace the client determined, or the zero Namespace if
// it couldn't.
func (c *Client) Namespace() models.Namespace {
	return c.poll.ns
}

func (c *Client) emit(e Event) {
	if c.onEvent != nil {
		c.onEvent(e)
	}
}

// s3Opener opens S3 object storage from the standard AWS environment
// variables, with the endpoint replaced when one is given.
func s3Opener(getenv func(string) string) func(context.Context, string) (models.ObjectReader, error) {
	return func(ctx context.Context, endpoint string) (models.ObjectReader, error) {
		return newS3Reader(ctx, getenv, endpoint)
	}
}

// realClock is the system clock.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Option configures Start or NewRuntime. The defaults suit production; the
// options exist mostly so tests can place a client in a controlled world.
//
// WithEventHandler and WithRuntime are about one Config. The rest are about
// the process, so they configure the Runtime: the one NewRuntime makes, or
// the process's own, made by the first Start not given WithRuntime. Start
// ignores them otherwise.
type Option func(*options)

type options struct {
	runtime   *Runtime
	openStore func(ctx context.Context, endpoint string) (models.ObjectReader, error)
	clock     models.Clock
	logger    *slog.Logger
	onEvent   func(Event)
	getenv    func(string) string
	buildPath string
	// workDir is where the search for the repository root, and so the local
	// config file, begins.
	workDir string
}

func newOptions(opts []Option) options {
	o := options{
		clock:     realClock{},
		logger:    log.GetLogger("mimir-client"),
		getenv:    os.Getenv,
		buildPath: mainPackagePath(),
		workDir:   workingDir(),
	}
	for _, opt := range opts {
		opt(&o)
	}

	return o
}

// WithRuntime makes the Config share rt rather than the process's own
// Runtime, as a test standing in for several processes does.
func WithRuntime(rt *Runtime) Option {
	return func(o *options) { o.runtime = rt }
}

// WithObjectStores sets how the client opens the object storage at an
// endpoint, as named by the MimirClient Config ("" for the default
// endpoint). The default opens S3 from the standard AWS environment
// variables.
func WithObjectStores(open func(ctx context.Context, endpoint string) (models.ObjectReader, error)) Option {
	return func(o *options) { o.openStore = open }
}

// WithClock sets the clock that paces polling.
func WithClock(c models.Clock) Option {
	return func(o *options) { o.clock = c }
}

// WithLogger sets the logger. The default is mjolnir's "mimir-client" logger.
func WithLogger(l *slog.Logger) Option {
	return func(o *options) { o.logger = l }
}

// WithEventHandler sets a function told about each Event: value changes and
// rejections, polls and Registrations. It is called from the client's
// goroutines, one Event at a time, and must not block for long. The startup
// poll's Events arrive before Start (and so the generated constructor)
// returns.
func WithEventHandler(f func(Event)) Option {
	return func(o *options) { o.onEvent = f }
}

// WithGetenv sets how environment variables are read. The default is
// [os.Getenv].
func WithGetenv(f func(key string) string) Option {
	return func(o *options) { o.getenv = f }
}

// WithBuildPath sets the main package path the Deployable name is derived
// from. The default comes from the binary's build info.
func WithBuildPath(path string) Option {
	return func(o *options) { o.buildPath = path }
}
