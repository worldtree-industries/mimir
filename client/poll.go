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
		c.emit(EventPolled, nil)
		return
	case errors.Is(err, objectstore.ErrNotFound):
		c.etag = ""
		c.applySnapshot(models.Snapshot{})
		c.emit(EventPolled, nil)
		return
	case err != nil:
		err = fmt.Errorf("read Snapshot %s/%s: %w", c.location.Bucket, c.location.Key, err)
		c.logger.WarnContext(ctx, "Poll failed; keeping current values", "error", err)
		c.emit(EventPolled, err)
		return
	}

	var snap models.Snapshot
	if err = json.Unmarshal(obj.Body, &snap); err != nil {
		err = fmt.Errorf("decode Snapshot %s/%s: %w", c.location.Bucket, c.location.Key, err)
		c.logger.ErrorContext(ctx, "Poll failed; keeping current values", "error", err)
		c.emit(EventPolled, err)
		return
	}

	c.applySnapshot(snap)
	c.etag = obj.ETag
	c.emit(EventPolled, nil)
}

// applySnapshot sets every Item to its value in snap, or its Default when
// snap holds none. A value that doesn't parse is logged and the Item keeps
// its current value.
func (c *Client) applySnapshot(snap models.Snapshot) {
	for _, item := range c.def.Items {
		value, ok := snap.Value(models.ItemKey{Name: item.Name, Type: item.Type})
		if !ok {
			value = item.Default
		}
		if err := item.Set(value); err != nil {
			c.logger.Error("Value doesn't parse; keeping the current one",
				"item", item.Name, "type", item.Type, "value", value, "error", err)
		}
	}
}
