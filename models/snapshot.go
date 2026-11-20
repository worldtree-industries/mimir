package models

import (
	"cmp"
	"slices"
)

// DefaultBucketPrefix is the bucket prefix used unless one is configured.
const DefaultBucketPrefix = "mimir"

// Snapshot is the published values for one Namespace, with Precedence already
// applied. It holds only Items with an applicable Override; every other Item
// uses its Default.
type Snapshot struct {
	Values []SnapshotValue `json:"values"`
}

// SnapshotValue is one Item's effective Override in a Snapshot.
type SnapshotValue struct {
	Item  ItemRef `json:"item"`
	Value string  `json:"value"`
}

// NewSnapshot returns a Snapshot holding values, ordered by Item name then
// type, so the same values always encode to the same bytes (and ETag).
func NewSnapshot(values []SnapshotValue) Snapshot {
	sorted := slices.Clone(values)
	if sorted == nil {
		sorted = []SnapshotValue{}
	}
	slices.SortFunc(sorted, func(a, b SnapshotValue) int {
		return cmp.Or(cmp.Compare(a.Item.Name, b.Item.Name), cmp.Compare(a.Item.Type, b.Item.Type))
	})

	return Snapshot{Values: sorted}
}

// Value returns the Override value the Snapshot holds for key, if any.
func (s Snapshot) Value(key ItemRef) (string, bool) {
	for _, v := range s.Values {
		if v.Item == key {
			return v.Value, true
		}
	}

	return "", false
}

// SnapshotLocation is where a Namespace's Snapshot lives in object storage.
type SnapshotLocation struct {
	Bucket string
	Key    string
}

// SnapshotLocationFor is the location of ns's Snapshot (ADR 0006): bucket
// "<prefix>-<environment>", object "<config>/<deployable>.json". Clients
// construct it themselves; they never ask the server.
func SnapshotLocationFor(prefix string, ns Namespace) SnapshotLocation {
	return SnapshotLocation{
		Bucket: prefix + "-" + ns.Environment,
		Key:    ns.Config + "/" + ns.Deployable + ".json",
	}
}
