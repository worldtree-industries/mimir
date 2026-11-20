package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/worldtree-industries/mimir/models"
)

// pollEvery polls the Snapshots every poll interval until ctx is done. The
// interval is read afresh for each wait, so an Override on it takes effect
// from the next wait on.
func (c *Client) pollEvery(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.clock.After(c.settings.currentPollInterval()):
			c.poll(ctx)
		}
	}
}

// poll reads MimirClient's Snapshot and then the Config's, and applies them.
// Only object storage is read, never mimir-server (ADR 0002). When the
// settings name a new location, the client first tries to read both
// Snapshots there, and moves only if it can; otherwise it logs why and stays
// where it is. Every poll ends with exactly one EventPolled.
func (c *Client) poll(ctx context.Context) {
	settingsErr := c.self.poll(ctx, c.store, c.at.prefix)

	if wanted := c.settings.wantedLocation(); wanted != c.at {
		if c.switchTo(ctx, wanted) {
			c.emit(Event{Kind: EventPolled})
			return
		}
	}

	err := c.user.poll(ctx, c.store, c.at.prefix)
	c.emit(Event{Kind: EventPolled, Err: errors.Join(settingsErr, err)})
}

// switchTo moves the client to read from loc, if it can read both Snapshots
// there; a missing Snapshot counts, since it means no Overrides. It reports
// whether it moved. On failure the client keeps its current location, so a
// bad location never strands it where it can't read the fix.
func (c *Client) switchTo(ctx context.Context, loc location) bool {
	store, err := c.storeFor(ctx, loc)
	var self, user fetched
	if err == nil {
		self, err = c.self.fetch(ctx, store, loc.prefix, "")
	}
	if err == nil {
		user, err = c.user.fetch(ctx, store, loc.prefix, "")
	}
	if err != nil {
		err = fmt.Errorf("switch to %s: %w", loc, err)
		c.logger.ErrorContext(ctx, "Can't read Snapshots from the new location; keeping the current one",
			"current", c.at.String(), "error", err)
		c.emit(Event{Kind: EventLocationSwitch, Err: err})

		return false
	}

	c.logger.InfoContext(ctx, "Switched Snapshot location", "from", c.at.String(), "to", loc.String())
	c.store, c.at = store, loc
	c.self.apply(ctx, self)
	c.user.apply(ctx, user)
	c.emit(Event{Kind: EventLocationSwitch})

	return true
}

// storeFor is the object storage at loc's endpoint: the current store if
// the endpoint is unchanged, or else a new one.
func (c *Client) storeFor(ctx context.Context, loc location) (models.ObjectReader, error) {
	if loc.endpoint == c.at.endpoint {
		return c.store, nil
	}
	store, err := c.openStore(ctx, loc.endpoint)
	if err != nil {
		return nil, fmt.Errorf("open object storage: %w", err)
	}

	return store, nil
}

// configPoll is the polling state of one Config the client reads: the
// Config the program uses, or MimirClient. It is touched only by whichever
// goroutine is polling: Start's until it returns, then the poll loop's.
type configPoll struct {
	def    Definition
	ns     models.Namespace
	logger *slog.Logger
	// report is told about value changes and rejections.
	report func(Event)

	// start is each Item's value when no Override applies: its Default, or
	// for MimirClient the environment variable it starts from.
	start map[models.ItemKey]string
	// etag is the ETag of the last Snapshot applied.
	etag string
	// good is each Item's last good value: what its getter returns.
	good map[models.ItemKey]string
	// rejected is each Item's last rejected value, so a bad value that stays
	// in the Snapshot is reported once rather than on every poll.
	rejected map[models.ItemKey]string
}

func newConfigPoll(def Definition, logger *slog.Logger, report func(Event)) *configPoll {
	p := &configPoll{
		def:      def,
		logger:   logger,
		report:   report,
		start:    make(map[models.ItemKey]string, len(def.Items)),
		good:     make(map[models.ItemKey]string, len(def.Items)),
		rejected: make(map[models.ItemKey]string),
	}
	for _, item := range def.Items {
		p.start[item.key()] = item.Default
	}

	return p
}

// errUndecodable is a Snapshot that isn't valid JSON.
var errUndecodable = errors.New("can't decode Snapshot")

// fetched is the result of reading a Snapshot.
type fetched struct {
	// unchanged is set when the Snapshot still has the ETag asked about.
	unchanged bool
	snap      models.Snapshot
	etag      string
}

// fetch reads the Config's Snapshot from store with bucket prefix prefix. A
// missing Snapshot is an empty one: no Overrides.
func (p *configPoll) fetch(ctx context.Context, store models.ObjectReader, prefix, etag string) (fetched, error) {
	loc := models.SnapshotLocationFor(prefix, p.ns)
	obj, err := store.Get(ctx, loc.Bucket, loc.Key, etag)
	switch {
	case errors.Is(err, models.ErrNotModified):
		return fetched{unchanged: true}, nil
	case errors.Is(err, models.ErrNotFound):
		return fetched{}, nil
	case err != nil:
		return fetched{}, fmt.Errorf("read Snapshot %s/%s: %w", loc.Bucket, loc.Key, err)
	}

	var snap models.Snapshot
	if err = json.Unmarshal(obj.Body, &snap); err != nil {
		return fetched{}, fmt.Errorf("%w %s/%s: %w", errUndecodable, loc.Bucket, loc.Key, err)
	}

	return fetched{snap: snap, etag: obj.ETag}, nil
}

// poll reads the Config's Snapshot and applies it. Not found means no
// Overrides, so every Item takes its start value; an unchanged Snapshot
// changes nothing; any other failure is logged, returned, and keeps the
// current values.
func (p *configPoll) poll(ctx context.Context, store models.ObjectReader, prefix string) error {
	f, err := p.fetch(ctx, store, prefix, p.etag)
	if err != nil {
		level := slog.LevelWarn
		if errors.Is(err, errUndecodable) {
			level = slog.LevelError
		}
		p.logger.Log(ctx, level, "Poll failed; keeping current values", "error", err)

		return err
	}
	p.apply(ctx, f)

	return nil
}

// apply applies a fetched Snapshot.
func (p *configPoll) apply(ctx context.Context, f fetched) {
	if f.unchanged {
		return
	}
	p.applySnapshot(ctx, f.snap)
	p.etag = f.etag
}

// applySnapshot sets every Item to its value in snap, or its start value
// when snap holds none, reporting each Item whose value changes. An Item
// already at that value is left alone. A value that doesn't parse is logged
// and reported once, and the Item keeps its last good value.
func (p *configPoll) applySnapshot(ctx context.Context, snap models.Snapshot) {
	for _, item := range p.def.Items {
		key := item.key()
		value, ok := snap.Value(key)
		if !ok {
			value = p.start[key]
		}
		p.setItem(ctx, item, key, value, true)
	}
}

// applyStart gives every Item its start value when the client starts.
// Taking it isn't a change, so only rejections are reported.
func (p *configPoll) applyStart(ctx context.Context) {
	for _, item := range p.def.Items {
		key := item.key()
		p.setItem(ctx, item, key, p.start[key], false)
	}
}

// setItem sets one Item to value unless it already has it, or value was the
// last one rejected for it.
func (p *configPoll) setItem(ctx context.Context, item Item, key models.ItemKey, value string, notify bool) {
	if good, ok := p.good[key]; ok && good == value {
		delete(p.rejected, key)
		return
	}
	if rejected, ok := p.rejected[key]; ok && rejected == value {
		return
	}

	if err := item.Set(value); err != nil {
		p.rejected[key] = value
		err = fmt.Errorf("item %s (%s): value %q: %w", item.Name, item.Type, value, err)
		p.logger.ErrorContext(ctx, "Value doesn't parse; keeping the last good one",
			"item", item.Name, "type", item.Type, "value", value, "error", err)
		p.report(Event{Kind: EventValueRejected, Err: err, Item: key, Value: value})

		return
	}

	p.good[key] = value
	delete(p.rejected, key)
	if notify {
		p.report(Event{Kind: EventValueChanged, Item: key, Value: value})
	}
}

func (item Item) key() models.ItemKey {
	return models.ItemKey{Name: item.Name, Type: item.Type}
}

// startFromEnv makes each Item's start value its environment variable, when
// that is set and parses. A bad one is logged and the Default used instead.
func (p *configPoll) startFromEnv(ctx context.Context, getenv func(string) string) {
	for _, item := range p.def.Items {
		name := envVar(item.Name)
		value := getenv(name)
		if value == "" {
			continue
		}
		if err := item.Set(value); err != nil {
			p.logger.ErrorContext(ctx, "Ignoring a bad environment variable; using the Default",
				"variable", name, "value", value, "error", err)
			continue
		}
		p.start[item.key()] = value
	}
}
