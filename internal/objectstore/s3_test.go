package objectstore_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/worldtree-industries/mimir/internal/objectstore"
	"github.com/worldtree-industries/mimir/internal/objectstore/objectstoretest"
)

// TestS3MeetsContract runs the contract against a real S3-compatible
// endpoint, such as Ceph RGW or MinIO. It runs only when
// MIMIR_TEST_S3_ENDPOINT and MIMIR_TEST_S3_BUCKET are set; credentials and the
// region come from the standard AWS environment variables. The bucket must
// already exist.
func TestS3MeetsContract(t *testing.T) {
	endpoint := os.Getenv("MIMIR_TEST_S3_ENDPOINT")
	bucket := os.Getenv("MIMIR_TEST_S3_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Skip("set MIMIR_TEST_S3_ENDPOINT and MIMIR_TEST_S3_BUCKET to run against a real S3-compatible endpoint")
	}

	getenv := func(key string) string {
		if key == "AWS_ENDPOINT_URL_S3" {
			return endpoint
		}
		return os.Getenv(key)
	}
	store, err := objectstore.NewS3FromEnv(t.Context(), getenv)
	if err != nil {
		t.Fatalf("NewS3FromEnv: %v", err)
	}
	objectstoretest.Run(t, store, bucket)
}

const noSuchKey = `<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`

// fakeS3 is just enough of the S3 API (path-style) to exercise S3 where no
// real endpoint exists. It is not a substitute for TestS3MeetsContract.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

func TestS3AgainstPathStyleFake(t *testing.T) {
	srv := httptest.NewServer(&fakeS3{objects: map[string][]byte{}})
	t.Cleanup(srv.Close)

	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	getenv := func(key string) string {
		if key == "AWS_ENDPOINT_URL_S3" {
			return srv.URL
		}
		return os.Getenv(key)
	}
	store, err := objectstore.NewS3FromEnv(t.Context(), getenv)
	if err != nil {
		t.Fatalf("NewS3FromEnv: %v", err)
	}

	// The fake only does path-style, so reaching it proves path-style works.
	objectstoretest.Run(t, store, "mimir-dev")
}

func TestS3Configured(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"nothing", nil, false},
		{"s3 endpoint", map[string]string{"AWS_ENDPOINT_URL_S3": "http://rgw"}, true},
		{"generic endpoint", map[string]string{"AWS_ENDPOINT_URL": "http://rgw"}, true},
		{"access key only", map[string]string{"AWS_ACCESS_KEY_ID": "k"}, true},
		{"unrelated", map[string]string{"PATH": "/bin"}, false},
	}
	for _, tt := range tests {
		t.Run(strings.ReplaceAll(tt.name, " ", "_"), func(t *testing.T) {
			got := objectstore.S3Configured(func(k string) string { return tt.env[k] })
			if got != tt.want {
				t.Errorf("S3Configured = %v, want %v", got, tt.want)
			}
		})
	}
}
