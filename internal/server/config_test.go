package server_test

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/worldtree-industries/mimir/internal/server"
)

// s3Env is the least object storage configuration: an endpoint.
func s3Env(extra map[string]string) func(string) string {
	env := map[string]string{"AWS_ENDPOINT_URL_S3": "http://rgw.example"}
	maps.Copy(env, extra)

	return func(key string) string { return env[key] }
}

func TestServerRefusesToStartWithoutObjectStorage(t *testing.T) {
	_, err := server.ConfigFromEnv(t.Context(), func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "object storage") {
		t.Errorf("ConfigFromEnv with no object storage configured: err = %v, want one about object storage", err)
	}
}

func TestServerSettingsDefault(t *testing.T) {
	cfg, err := server.ConfigFromEnv(t.Context(), s3Env(nil))
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if cfg.ObjectStore == nil {
		t.Error("no object store configured from the AWS variables")
	}
	if cfg.BucketPrefix != "mimir" {
		t.Errorf("BucketPrefix = %q, want %q", cfg.BucketPrefix, "mimir")
	}
	if cfg.ReconcileInterval != server.DefaultReconcileInterval {
		t.Errorf("ReconcileInterval = %v, want %v", cfg.ReconcileInterval, server.DefaultReconcileInterval)
	}
	if cfg.ExpiryInterval != server.DefaultExpiryInterval {
		t.Errorf("ExpiryInterval = %v, want %v", cfg.ExpiryInterval, server.DefaultExpiryInterval)
	}
}

func TestServerSettingsComeFromTheEnvironment(t *testing.T) {
	cfg, err := server.ConfigFromEnv(t.Context(), s3Env(map[string]string{
		"MIMIR_BUCKET_PREFIX":      "acme-config",
		"MIMIR_RECONCILE_INTERVAL": "90s",
		"MIMIR_EXPIRY_INTERVAL":    "6h",
	}))
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if cfg.BucketPrefix != "acme-config" {
		t.Errorf("BucketPrefix = %q, want %q", cfg.BucketPrefix, "acme-config")
	}
	if cfg.ReconcileInterval != 90*time.Second {
		t.Errorf("ReconcileInterval = %v, want 90s", cfg.ReconcileInterval)
	}
	if cfg.ExpiryInterval != 6*time.Hour {
		t.Errorf("ExpiryInterval = %v, want 6h", cfg.ExpiryInterval)
	}
}

func TestServerRefusesUnusableSettings(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"a reconcile interval that isn't a duration":  {"MIMIR_RECONCILE_INTERVAL": "often"},
		"a zero reconcile interval":                   {"MIMIR_RECONCILE_INTERVAL": "0s"},
		"a negative expiry interval":                  {"MIMIR_EXPIRY_INTERVAL": "-1h"},
		"a bucket prefix S3 can't name a bucket with": {"MIMIR_BUCKET_PREFIX": "Acme_Config"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := server.ConfigFromEnv(t.Context(), s3Env(env)); err == nil {
				t.Errorf("ConfigFromEnv(%v) succeeded, want an error", env)
			}
		})
	}
}
