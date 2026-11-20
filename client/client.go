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

// DefaultPollInterval is the Default of MimirClient's poll interval: how
// often a client polls its Snapshots unless tuned.
const DefaultPollInterval = 30 * time.Second

// defaultServerURL is the Default of MimirClient's server URL.
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
	// EventLocationSwitch is a finished attempt to read Snapshots from a new
	// location (object storage endpoint or bucket prefix) named by the
	// client's settings. Err is set when it failed and the client kept its
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
	Item  models.ItemKey
	Value string
}

// Client keeps one Config's values current. It is what Start returns; its
// work happens in the background until Start's context is done.
//
// Alongside the Config it reads MimirClient, the Config of its own settings
// (poll interval, server URL, object storage endpoint and bucket prefix), in
// the same Deployable and Environment, so those can be tuned live too.
type Client struct {
	clock     clock.Clock
	logger    *slog.Logger
	onEvent   func(Event)
	openStore func(ctx context.Context, endpoint string) (objectstore.Store, error)
	http      *http.Client
	settings  *settings

	// The polling state below is touched only by whichever goroutine is
	// polling: Start's until it returns, then the poll loop's.

	// user is the Config the program reads, and self is MimirClient.
	user, self *configPoll
	// at is the location Snapshots are read from, and store the object
	// storage at its endpoint. They change only once both Snapshots have
	// been read at the location the settings name.
	at    location
	store objectstore.Store
}

// Start applies the Definition's Defaults, determines the Namespace, reads the
// Namespace's Snapshot once, and then, in the background until ctx is done,
// registers the Config with mimir-server (again every
// DefaultRegistrationInterval) and polls the Snapshot every poll interval.
// Nothing it does in the background can stop the program reading values: on
// any failure the Config keeps its current values.
//
// The client's own settings start from environment variables or their
// Defaults at every Start, so an Override that moved a client somewhere bad
// never survives a restart: it is adopted again only once it has been read
// from the starting location.
func Start(ctx context.Context, def Definition, opts ...Option) *Client {
	o := newOptions(opts)
	c := &Client{
		clock:     o.clock,
		logger:    o.logger.With("config", def.Config),
		onEvent:   o.onEvent,
		openStore: o.openStore,
		http:      &http.Client{Timeout: registrationTimeout},
		settings:  &settings{},
	}
	c.user = newConfigPoll(def, c.logger, c.emit)
	c.self = newConfigPoll(c.settings.definition(), c.logger, c.reportSetting)

	c.user.applyStart(ctx)
	c.self.startFromEnv(ctx, o.getenv)
	c.self.applyStart(ctx)
	c.at = c.settings.wantedLocation()

	ns, err := discoverNamespace(def.Config, o.getenv, o.buildPath)
	if err != nil {
		c.logger.ErrorContext(ctx, "Can't determine the Namespace; running on Defaults only", "error", err)
		return c
	}
	c.user.ns = ns
	c.self.ns = models.Namespace{Config: MimirClientConfig, Deployable: ns.Deployable, Environment: ns.Environment}
	c.logger = c.logger.With("deployable", ns.Deployable, "environment", ns.Environment)
	c.user.logger = c.logger
	c.self.logger = c.logger.With("settings", MimirClientConfig)

	if c.openStore == nil && (objectstore.S3Configured(o.getenv) || c.at.endpoint != "") {
		c.openStore = s3Opener(o.getenv)
	}
	if c.openStore == nil {
		c.logger.ErrorContext(ctx, "No object storage configured; running on Defaults only")
		return c
	}
	c.store, err = c.openStore(ctx, c.at.endpoint)
	if err != nil {
		c.logger.ErrorContext(ctx, "Can't configure object storage; running on Defaults only", "error", err)
		return c
	}

	startupCtx, cancel := context.WithTimeout(ctx, startupPollTimeout)
	c.poll(startupCtx)
	cancel()

	go c.registerEvery(ctx)
	go c.pollEvery(ctx)

	return c
}

// Namespace is the Namespace the client determined, or the zero Namespace if
// it couldn't.
func (c *Client) Namespace() models.Namespace {
	return c.user.ns
}

func (c *Client) emit(e Event) {
	if c.onEvent != nil {
		c.onEvent(e)
	}
}

// reportSetting logs a change to one of the client's own settings. They
// aren't the program's values, so the program isn't sent value events for
// them; rejections are logged where they happen.
func (c *Client) reportSetting(e Event) {
	if e.Kind == EventValueChanged {
		c.self.logger.Info("Client setting changed", "item", e.Item.Name, "value", e.Value)
	}
}

// s3Opener opens S3 object storage from the standard AWS environment
// variables, with the endpoint replaced when one is given.
func s3Opener(getenv func(string) string) func(context.Context, string) (objectstore.Store, error) {
	return func(ctx context.Context, endpoint string) (objectstore.Store, error) {
		env := getenv
		if endpoint != "" {
			env = func(key string) string {
				if key == "AWS_ENDPOINT_URL_S3" {
					return endpoint
				}
				return getenv(key)
			}
		}

		return objectstore.NewS3FromEnv(ctx, env)
	}
}

// Option configures Start. The defaults suit production; the options exist
// mostly so tests can place a client in a controlled world.
type Option func(*options)

type options struct {
	openStore func(ctx context.Context, endpoint string) (objectstore.Store, error)
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

// WithObjectStore sets the object storage Snapshots are read from, whatever
// endpoint the client's settings name.
func WithObjectStore(s objectstore.Store) Option {
	return func(o *options) {
		o.openStore = func(context.Context, string) (objectstore.Store, error) { return s, nil }
	}
}

// WithObjectStores sets how the client opens the object storage at an
// endpoint, as named by its settings ("" for the default endpoint). The
// default opens S3 from the standard AWS environment variables.
func WithObjectStores(open func(ctx context.Context, endpoint string) (objectstore.Store, error)) Option {
	return func(o *options) { o.openStore = open }
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
