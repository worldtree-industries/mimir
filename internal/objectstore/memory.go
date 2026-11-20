package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sync"
)

// Memory is a [Store] held in memory. Tests share one between mimir-server
// and the clients they run. Buckets need no creating. The zero value is not
// usable; call NewMemory.
type Memory struct {
	mu      sync.RWMutex
	objects map[location]Object
}

type location struct {
	bucket, key string
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{objects: make(map[location]Object)}
}

// Put stores a copy of body at bucket/key. Its ETag is the body's SHA-256, so
// rewriting identical contents keeps the ETag, as S3 does.
func (m *Memory) Put(_ context.Context, bucket, key string, body []byte) error {
	sum := sha256.Sum256(body)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.objects[location{bucket, key}] = Object{Body: slices.Clone(body), ETag: `"` + hex.EncodeToString(sum[:]) + `"`}

	return nil
}

// Get returns a copy of the object at bucket/key, following the [Store]
// contract for etag.
func (m *Memory) Get(_ context.Context, bucket, key, etag string) (Object, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	obj, ok := m.objects[location{bucket, key}]
	switch {
	case !ok:
		return Object{}, ErrNotFound
	case etag != "" && etag == obj.ETag:
		return Object{}, ErrNotModified
	}

	return Object{Body: slices.Clone(obj.Body), ETag: obj.ETag}, nil
}

// Delete removes the object at bucket/key, if there is one.
func (m *Memory) Delete(_ context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.objects, location{bucket, key})

	return nil
}
