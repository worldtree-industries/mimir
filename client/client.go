package client

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/dfryer1193/mjolnir/utils/log"

	"github.com/worldtree-industries/mimir/internal/clock"
	"github.com/worldtree-industries/mimir/internal/objectstore"
	"github.com/worldtree-industries/mimir/models"
)

// DefaultPollInterval is how often a client polls its Snapshot.
const DefaultPollInterval = 30 * time.Second

// defaultServerURL is where mimir-server is reached when MIMIR_SERVER_URL
// isn't set.
const defaultServerURL = "http://mimir-server:8080"

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
	Items []Item
}

// Item is one Item of a Definition.
type Item struct {
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
	Item  models.ItemKey
	Value string
}

// Client keeps one Config's values current. It is what Start returns; its
// work happens in the background until Start's context is done.
type Client struct {
	def       Definition
	ns        models.Namespace
	location  models.SnapshotLocation
	store     objectstore.Store
	clock     clock.Clock
	logger    *slog.Logger
	onEvent   func(Event)
	serverURL string
	http      *http.Client

	// The polling state below is touched only by whichever goroutine is
	// polling: Start's until it returns, then the poll loop's.

	// etag is the ETag of the last Snapshot applied.
	etag string
	// good is each Item's last good value: what its getter returns.
	good map[models.ItemKey]string
	// rejected is each Item's last rejected value, so a bad value that stays
	// in the Snapshot is reported once rather than on every poll.
	rejected map[models.ItemKey]string
}

// Start applies the Definition's Defaults, determines the Namespace, reads the
// Namespace's Snapshot once, and then, in the background until ctx is done,
// registers the Config with mimir-server and polls the Snapshot every
// DefaultPollInterval. Nothing it does in the background can stop the program
// reading values: on any failure the Config keeps its current values.
func Start(ctx context.Context, def Definition, opts ...Option) *Client {
	o := newOptions(opts)
	c := &Client{
		def:       def,
		store:     o.store,
		clock:     o.clock,
		logger:    o.logger.With("config", def.Config),
		onEvent:   o.onEvent,
		serverURL: o.getenv("MIMIR_SERVER_URL"),
		http:      &http.Client{Timeout: registrationTimeout},
		good:      make(map[models.ItemKey]string, len(def.Items)),
		rejected:  make(map[models.ItemKey]string),
	}
	if c.serverURL == "" {
		c.serverURL = defaultServerURL
	}

	c.applyDefaults(ctx)

	ns, err := discoverNamespace(def.Config, o.getenv, o.buildPath)
	if err != nil {
		c.logger.ErrorContext(ctx, "Can't determine the Namespace; running on Defaults only", "error", err)
		return c
	}
	c.ns = ns
	c.location = models.SnapshotLocationFor(models.DefaultBucketPrefix, ns)
	c.logger = c.logger.With("deployable", ns.Deployable, "environment", ns.Environment)

	if c.store == nil && objectstore.S3Configured(o.getenv) {
		s3, s3Err := objectstore.NewS3FromEnv(ctx, o.getenv)
		if s3Err != nil {
			c.logger.ErrorContext(ctx, "Can't configure object storage", "error", s3Err)
		} else {
			c.store = s3
		}
	}
	if c.store == nil {
		c.logger.ErrorContext(ctx, "No object storage configured; running on Defaults only")
		return c
	}

	startupCtx, cancel := context.WithTimeout(ctx, startupPollTimeout)
	c.poll(startupCtx)
	cancel()

	go c.register(ctx)
	go c.pollEvery(ctx, DefaultPollInterval)

	return c
}

// Namespace is the Namespace the client determined, or the zero Namespace if
// it couldn't.
func (c *Client) Namespace() models.Namespace {
	return c.ns
}

func (c *Client) emit(e Event) {
	if c.onEvent != nil {
		c.onEvent(e)
	}
}

// Option configures Start. The defaults suit production; the options exist
// mostly so tests can place a client in a controlled world.
type Option func(*options)

type options struct {
	store     objectstore.Store
	clock     clock.Clock
	logger    *slog.Logger
	onEvent   func(Event)
	getenv    func(string) string
	buildPath string
}

func newOptions(opts []Option) options {
	o := options{
		clock:     clock.Real(),
		logger:    log.GetLogger("mimir-client"),
		getenv:    os.Getenv,
		buildPath: mainPackagePath(),
	}
	for _, opt := range opts {
		opt(&o)
	}

	return o
}

// WithObjectStore sets the object storage Snapshots are read from.
func WithObjectStore(s objectstore.Store) Option {
	return func(o *options) { o.store = s }
}

// WithClock sets the clock that paces polling.
func WithClock(c clock.Clock) Option {
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
