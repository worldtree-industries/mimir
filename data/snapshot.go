package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/worldtree-industries/mimir/models"
)

// Snapshots writes Snapshots to object storage at the locations clients
// construct for themselves (ADR 0006).
type Snapshots struct {
	store  models.ObjectStore
	prefix string
}

// NewSnapshots returns a Snapshots writing to store under bucket prefix
// prefix.
func NewSnapshots(store models.ObjectStore, prefix string) *Snapshots {
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

// probeKey is the object readiness asks for. No Snapshot can have it, since
// every Snapshot's key holds a "/".
const probeKey = "readiness-probe"

// Reachable reports whether object storage answers for environment's bucket,
// by reading an object that isn't there: not found means the bucket answers;
// any other failure is an error.
func (s *Snapshots) Reachable(ctx context.Context, environment string) error {
	bucket := models.SnapshotLocationFor(s.prefix, models.Namespace{Environment: environment}).Bucket

	_, err := s.store.Get(ctx, bucket, probeKey, "")
	if err != nil && !errors.Is(err, models.ErrNotFound) && !errors.Is(err, models.ErrNotModified) {
		return fmt.Errorf("reach object storage bucket %s: %w", bucket, err)
	}

	return nil
}
