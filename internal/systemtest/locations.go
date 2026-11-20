package systemtest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/worldtree-industries/mimir/internal/objectstore"
	"github.com/worldtree-industries/mimir/models"
)

// NewEndpoint adds object storage that clients reach at endpoint when their
// settings name it, as another object storage service would be. h.Store is
// at the default endpoint, "". Any endpoint not added can't be reached.
func (h *Harness) NewEndpoint(endpoint string) *objectstore.Memory {
	store := objectstore.NewMemory()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.endpoints == nil {
		h.endpoints = make(map[string]*objectstore.Memory)
	}
	h.endpoints[endpoint] = store

	return store
}

// openEndpoint is how clients in the harness open object storage.
func (h *Harness) openEndpoint(_ context.Context, endpoint string) (objectstore.Store, error) {
	if endpoint == "" {
		return h.Store, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if store, ok := h.endpoints[endpoint]; ok {
		return store, nil
	}

	return nil, fmt.Errorf("dial %s: %w", endpoint, errUnreachable)
}

// CopySnapshot copies ns's published Snapshot to the bucket with prefix in
// to, as when a location's contents are mirrored to another.
func (h *Harness) CopySnapshot(t *testing.T, ns models.Namespace, to objectstore.Store, prefix string) {
	t.Helper()

	snap, ok := h.Snapshot(t, ns)
	if !ok {
		t.Fatalf("no Snapshot published for %+v to copy", ns)
	}
	h.PutSnapshotAt(t, to, prefix, ns, snap)
}

// PutSnapshotAt writes snap as ns's Snapshot to the bucket with prefix in
// to.
func (h *Harness) PutSnapshotAt(
	t *testing.T,
	to objectstore.Store,
	prefix string,
	ns models.Namespace,
	snap models.Snapshot,
) {
	t.Helper()

	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	loc := models.SnapshotLocationFor(prefix, ns)
	if err = to.Put(t.Context(), loc.Bucket, loc.Key, body); err != nil {
		t.Fatalf("write Snapshot %s/%s: %v", loc.Bucket, loc.Key, err)
	}
}

// TakeLocationSwitches returns the outcome of each attempt the client has
// made to switch Snapshot location since the last call, in order: nil for a
// switch, or why it failed.
func (d *RunningDeployable) TakeLocationSwitches() []error {
	d.mu.Lock()
	defer d.mu.Unlock()

	switches := d.locationSwitches
	d.locationSwitches = nil

	return switches
}
