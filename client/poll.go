package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/worldtree-industries/mimir/internal/objectstore"
	"github.com/worldtree-industries/mimir/models"
)

// pollEvery polls the Snapshot every interval until ctx is done.
func (c *Client) pollEvery(ctx context.Context, interval time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.clock.After(interval):
			c.poll(ctx)
		}
	}
}

// poll reads the Snapshot from its constructed location and applies it. Only
// object storage is read, never mimir-server (ADR 0002). Not found means no
// Overrides, so every Item takes its Default; an unchanged Snapshot changes
// nothing; any other failure keeps the current values.
func (c *Client) poll(ctx context.Context) {
	obj, err := c.store.Get(ctx, c.location.Bucket, c.location.Key, c.etag)
	switch {
	case errors.Is(err, objectstore.ErrNotModified):
		c.emit(Event{Kind: EventPolled})
		return
	case errors.Is(err, objectstore.ErrNotFound):
		c.etag = ""
		c.applySnapshot(ctx, models.Snapshot{})
		c.emit(Event{Kind: EventPolled})
		return
	case err != nil:
		err = fmt.Errorf("read Snapshot %s/%s: %w", c.location.Bucket, c.location.Key, err)
		c.logger.WarnContext(ctx, "Poll failed; keeping current values", "error", err)
		c.emit(Event{Kind: EventPolled, Err: err})
		return
	}

	var snap models.Snapshot
	if err = json.Unmarshal(obj.Body, &snap); err != nil {
		err = fmt.Errorf("decode Snapshot %s/%s: %w", c.location.Bucket, c.location.Key, err)
		c.logger.ErrorContext(ctx, "Poll failed; keeping current values", "error", err)
		c.emit(Event{Kind: EventPolled, Err: err})
		return
	}

	c.applySnapshot(ctx, snap)
	c.etag = obj.ETag
	c.emit(Event{Kind: EventPolled})
}

// applySnapshot sets every Item to its value in snap, or its Default when
// snap holds none, reporting each Item whose value changes. An Item already
// at that value is left alone. A value that doesn't parse is logged and
// reported once, and the Item keeps its last good value.
func (c *Client) applySnapshot(ctx context.Context, snap models.Snapshot) {
	for _, item := range c.def.Items {
		key := models.ItemKey{Name: item.Name, Type: item.Type}
		value, ok := snap.Value(key)
		if !ok {
			value = item.Default
		}
		c.setItem(ctx, item, key, value, true)
	}
}

// applyDefaults gives every Item its Default when the client starts. Taking
// a Default isn't a change, so only rejections are reported.
func (c *Client) applyDefaults(ctx context.Context) {
	for _, item := range c.def.Items {
		c.setItem(ctx, item, models.ItemKey{Name: item.Name, Type: item.Type}, item.Default, false)
	}
}

// setItem sets one Item to value unless it already has it, or value was the
// last one rejected for it.
func (c *Client) setItem(ctx context.Context, item Item, key models.ItemKey, value string, notify bool) {
	if good, ok := c.good[key]; ok && good == value {
		delete(c.rejected, key)
		return
	}
	if rejected, ok := c.rejected[key]; ok && rejected == value {
		return
	}

	if err := item.Set(value); err != nil {
		c.rejected[key] = value
		err = fmt.Errorf("item %s (%s): value %q: %w", item.Name, item.Type, value, err)
		c.logger.ErrorContext(ctx, "Value doesn't parse; keeping the last good one",
			"item", item.Name, "type", item.Type, "value", value, "error", err)
		c.emit(Event{Kind: EventValueRejected, Err: err, Item: key, Value: value})

		return
	}

	c.good[key] = value
	delete(c.rejected, key)
	if notify {
		c.emit(Event{Kind: EventValueChanged, Item: key, Value: value})
	}
}
