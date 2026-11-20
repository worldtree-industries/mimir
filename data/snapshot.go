package data

import (
	"context"
	"encoding/json"
	"errors"
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

// Delete removes ns's Snapshot. Deleting one that isn't there is fine.
func (s *Snapshots) Delete(ctx context.Context, ns models.Namespace) error {
	loc := models.SnapshotLocationFor(s.prefix, ns)
	if err := s.store.Delete(ctx, loc.Bucket, loc.Key); err != nil {
		return fmt.Errorf("delete Snapshot %s/%s: %w", loc.Bucket, loc.Key, err)
	}

	return nil
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

// Reachable reports whether object storage answers for ns's bucket. A missing
// Snapshot still means it does; only a failure to get an answer is an error.
func (s *Snapshots) Reachable(ctx context.Context, ns models.Namespace) error {
	loc := models.SnapshotLocationFor(s.prefix, ns)

	_, err := s.store.Get(ctx, loc.Bucket, loc.Key, "")
	if err != nil && !errors.Is(err, objectstore.ErrNotFound) && !errors.Is(err, objectstore.ErrNotModified) {
		return fmt.Errorf("reach object storage bucket %s: %w", loc.Bucket, err)
	}

	return nil
}
