package systemtest

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/worldtree-industries/mimir/internal/objectstore"
)

// errInjected is what a faulty store returns while a fault is switched on.
var errInjected = errors.New("systemtest: injected object storage failure")

// faultyStore wraps a store, which mimir-server publishes through, so a test
// can make uploads or the whole of object storage fail. Clients and the
// test's own reads go to the wrapped store directly.
type faultyStore struct {
	objectstore.Store

	failUploads atomic.Bool
	failAll     atomic.Bool
	calls       atomic.Int64
}

func (f *faultyStore) Put(ctx context.Context, bucket, key string, body []byte) error {
	f.calls.Add(1)
	if f.failUploads.Load() || f.failAll.Load() {
		return errInjected
	}

	return f.Store.Put(ctx, bucket, key, body)
}

func (f *faultyStore) Get(ctx context.Context, bucket, key, etag string) (objectstore.Object, error) {
	f.calls.Add(1)
	if f.failAll.Load() {
		return objectstore.Object{}, errInjected
	}

	return f.Store.Get(ctx, bucket, key, etag)
}

func (f *faultyStore) Delete(ctx context.Context, bucket, key string) error {
	f.calls.Add(1)
	if f.failAll.Load() {
		return errInjected
	}

	return f.Store.Delete(ctx, bucket, key)
}
