package objectstore_test

import (
	"os"
	"strings"
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

func TestS3AgainstPathStyleFake(t *testing.T) {
	srv := objectstoretest.NewFakeS3(t)

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
