package objectstoretest

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

const noSuchKey = `<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`

// FakeS3 is just enough of the S3 API (path-style only) to exercise S3
// clients where no real endpoint exists. It is not a substitute for running
// the contract against a real endpoint.
type FakeS3 struct {
	// URL is the endpoint to give S3 clients.
	URL string

	mu      sync.Mutex
	objects map[string][]byte
}

// NewFakeS3 starts a FakeS3 that stops when the test ends.
func NewFakeS3(t *testing.T) *FakeS3 {
	t.Helper()

	f := &FakeS3{objects: map[string][]byte{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.URL = srv.URL

	return f
}

// Put stores body at bucket/key directly, as another writer would.
func (f *FakeS3) Put(bucket, key string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.objects["/"+bucket+"/"+key] = body
}

func (f *FakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path // /<bucket>/<key>, path-style
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		f.objects[path] = body
		w.Header().Set("ETag", etagOf(body))
	case http.MethodGet:
		body, ok := f.objects[path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, noSuchKey)
			return
		}
		w.Header().Set("ETag", etagOf(body))
		if inm := r.Header.Get("If-None-Match"); inm != "" && inm == etagOf(body) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(body)
	case http.MethodDelete:
		delete(f.objects, path)
		w.WriteHeader(http.StatusNoContent)
	}
}

func etagOf(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}
