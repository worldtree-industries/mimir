// Package objectstore is Mimir's view of object storage: where mimir-server
// publishes Snapshots and clients poll them (ADR 0002, ADR 0006).
//
// It is shared infrastructure rather than a Layer: the data Layer writes
// through it and the client Layer reads through it, and neither imports the
// other. Implementations: [Memory] (tests). The S3 one comes with #16.
package objectstore

import (
	"context"
	"errors"
)

// ErrNotFound means the object does not exist.
var ErrNotFound = errors.New("objectstore: object not found")

// ErrNotModified means a conditional Get found the object unchanged.
var ErrNotModified = errors.New("objectstore: object not modified")

// Object is an object's body and the ETag that identifies this version of it.
type Object struct {
	Body []byte
	ETag string
}

// Store reads and writes whole objects. Implementations must be safe for
// concurrent use.
type Store interface {
	// Put writes body as the object at bucket/key, replacing any existing one.
	Put(ctx context.Context, bucket, key string, body []byte) error
	// Get reads the object at bucket/key. When etag is not empty and matches
	// the object's current ETag, it returns ErrNotModified instead. A missing
	// object is ErrNotFound; any other error means the store couldn't answer.
	Get(ctx context.Context, bucket, key, etag string) (Object, error)
	// Delete removes the object at bucket/key. Deleting a missing object is
	// not an error.
	Delete(ctx context.Context, bucket, key string) error
}
