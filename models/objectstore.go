package models

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound means an object does not exist.
var ErrNotFound = errors.New("object not found")

// ErrNotModified means a conditional Get found the object unchanged.
var ErrNotModified = errors.New("object not modified")

// Object is an object's body and the ETag that identifies this version of it.
type Object struct {
	Body []byte
	ETag string
}

// ObjectReader reads whole objects from object storage, where Snapshots
// live (ADR 0002, ADR 0006). It is all a client needs. Implementations must
// be safe for concurrent use.
type ObjectReader interface {
	// Get reads the object at bucket/key. When etag is not empty and matches
	// the object's current ETag, it returns ErrNotModified instead. A missing
	// object is ErrNotFound; any other error, including a missing bucket,
	// means the store couldn't answer.
	Get(ctx context.Context, bucket, key, etag string) (Object, error)
}

// ObjectStore reads and writes whole objects: what mimir-server publishes
// Snapshots through. Implementations must be safe for concurrent use.
type ObjectStore interface {
	ObjectReader

	// Put writes body as the object at bucket/key, replacing any existing one.
	Put(ctx context.Context, bucket, key string, body []byte) error
	// Delete removes the object at bucket/key. Deleting a missing object is
	// not an error.
	Delete(ctx context.Context, bucket, key string) error
}

// Clock tells the time and waits. mimir-server and the client are given one,
// so tests can drive polling, re-registration and expiry without sleeping.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// After returns a channel that receives the time once d has passed.
	After(d time.Duration) <-chan time.Time
}
