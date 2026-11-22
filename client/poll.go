package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/worldtree-industries/mimir/models"
)

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
	// for MimirClient the environment variable it starts from, or its Local
	// Value.
	start map[models.ItemRef]string
	// local is the Items whose start value is a Local Value, which no
	// Override replaces.
	local map[models.ItemRef]bool
	// etag is the ETag of the last Snapshot applied.
	etag string
	// good is each Item's last good value: what its getter returns.
	good map[models.ItemRef]string
	// rejected is each Item's last rejected value, so a bad value that stays
	// in the Snapshot is reported once rather than on every poll.
	rejected map[models.ItemRef]string
}

func newConfigPoll(def Definition, logger *slog.Logger, report func(Event)) *configPoll {
	p := &configPoll{
		def:      def,
		logger:   logger,
		report:   report,
		start:    make(map[models.ItemRef]string, len(def.Items)),
		local:    make(map[models.ItemRef]bool),
		good:     make(map[models.ItemRef]string, len(def.Items)),
		rejected: make(map[models.ItemRef]string),
	}
	for _, item := range def.Items {
		p.start[item.ref()] = item.Default
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
// when snap holds none or it has a Local Value, reporting each Item whose
// value changes. An Item already at that value is left alone. A value that
// doesn't parse is logged and reported once, and the Item keeps its last
// good value.
func (p *configPoll) applySnapshot(ctx context.Context, snap models.Snapshot) {
	for _, item := range p.def.Items {
		key := item.ref()
		p.setItem(ctx, item, key, p.valueFor(snap, key), true)
	}
}

// applyStart gives every Item its start value when the client starts.
// Taking it isn't a change, so only rejections are reported.
func (p *configPoll) applyStart(ctx context.Context) {
	for _, item := range p.def.Items {
		key := item.ref()
		p.setItem(ctx, item, key, p.start[key], false)
	}
}

// setItem sets one Item to value unless it already has it, or value was the
// last one rejected for it.
func (p *configPoll) setItem(ctx context.Context, item ItemDefinition, key models.ItemRef, value string, notify bool) {
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

func (item ItemDefinition) ref() models.ItemRef {
	return models.ItemRef{Name: item.Name, Type: item.Type}
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
		p.start[item.ref()] = value
	}
}
