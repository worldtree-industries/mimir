package data

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/worldtree-industries/mimir/internal/objectstore"
	"github.com/worldtree-industries/mimir/models"
)

// Snapshots writes Snapshots to object storage at the locations clients
// construct for themselves (ADR 0006).
type Snapshots struct {
	store  objectstore.Store
	prefix string
}

// NewSnapshots returns a Snapshots writing to store under bucket prefix
// prefix.
func NewSnapshots(store objectstore.Store, prefix string) *Snapshots {
	return &Snapshots{store: store, prefix: prefix}
}

// Publish writes snap as ns's Snapshot, replacing the one there.
func (s *Snapshots) Publish(ctx context.Context, ns models.Namespace, snap models.Snapshot) error {
	loc := models.SnapshotLocationFor(s.prefix, ns)

	body, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("encode Snapshot %s/%s: %w", loc.Bucket, loc.Key, err)
	}
	if err = s.store.Put(ctx, loc.Bucket, loc.Key, body); err != nil {
		return fmt.Errorf("publish Snapshot %s/%s: %w", loc.Bucket, loc.Key, err)
	}

	return nil
}
