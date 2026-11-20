// Package objectstoretest is the contract every [models.ObjectStore]
// implementation must meet (each implementation's tests call Run), and
// FakeS3, a path-style S3 endpoint for tests.
package objectstoretest

import (
	"errors"
	"testing"

	"github.com/worldtree-industries/mimir/models"
)

// Run checks store against the object storage contract. bucket must be a
// bucket the store can write to; the contract writes only keys under
// "objectstoretest/".
func Run(t *testing.T, store models.ObjectStore, bucket string) {
	t.Helper()

	t.Run("get of a missing object is not found", func(t *testing.T) {
		_, err := store.Get(t.Context(), bucket, "objectstoretest/missing.json", "")
		if !errors.Is(err, models.ErrNotFound) {
			t.Fatalf("Get missing: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("put then get returns the body and an ETag", func(t *testing.T) {
		key := "objectstoretest/put.json"
		put(t, store, bucket, key, `{"a":1}`)

		obj := get(t, store, bucket, key, "")
		if string(obj.Body) != `{"a":1}` {
			t.Errorf("body = %q, want %q", obj.Body, `{"a":1}`)
		}
		if obj.ETag == "" {
			t.Error("ETag is empty")
		}
	})

	t.Run("conditional get with the current ETag is not modified", func(t *testing.T) {
		key := "objectstoretest/conditional.json"
		put(t, store, bucket, key, `{"a":1}`)
		first := get(t, store, bucket, key, "")

		_, err := store.Get(t.Context(), bucket, key, first.ETag)
		if !errors.Is(err, models.ErrNotModified) {
			t.Fatalf("Get with current ETag: err = %v, want ErrNotModified", err)
		}

		put(t, store, bucket, key, `{"a":2}`)
		second := get(t, store, bucket, key, first.ETag)
		if string(second.Body) != `{"a":2}` {
			t.Errorf("body after change = %q, want %q", second.Body, `{"a":2}`)
		}
		if second.ETag == first.ETag {
			t.Error("ETag did not change with the body")
		}
	})

	t.Run("deleted object is not found, and deleting again is fine", func(t *testing.T) {
		key := "objectstoretest/delete.json"
		put(t, store, bucket, key, `{}`)

		if err := store.Delete(t.Context(), bucket, key); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := store.Get(t.Context(), bucket, key, ""); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("Get after Delete: err = %v, want ErrNotFound", err)
		}
		if err := store.Delete(t.Context(), bucket, key); err != nil {
			t.Errorf("second Delete: %v", err)
		}
	})
}

func put(t *testing.T, store models.ObjectStore, bucket, key, body string) {
	t.Helper()

	if err := store.Put(t.Context(), bucket, key, []byte(body)); err != nil {
		t.Fatalf("Put %s/%s: %v", bucket, key, err)
	}
}

func get(t *testing.T, store models.ObjectStore, bucket, key, etag string) models.Object {
	t.Helper()

	obj, err := store.Get(t.Context(), bucket, key, etag)
	if err != nil {
		t.Fatalf("Get %s/%s: %v", bucket, key, err)
	}

	return obj
}
