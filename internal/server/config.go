package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/worldtree-industries/mimir/internal/objectstore"
)

// The environment variables mimir-server reads its settings from, beside
// DATABASE_URL, PORT and the standard AWS variables for object storage.
const (
	// EnvReconcileInterval is how often every Snapshot is republished, as a
	// Go duration.
	EnvReconcileInterval = "MIMIR_RECONCILE_INTERVAL"
	// EnvExpiryInterval is how often expiry runs, as a Go duration.
	EnvExpiryInterval = "MIMIR_EXPIRY_INTERVAL"
)

// errNoObjectStorage means no object storage is configured. mimir-server
// refuses to start then: Snapshots it can't publish are no use to a client.
var errNoObjectStorage = errors.New(
	"no object storage configured: set AWS_ENDPOINT_URL_S3, AWS_ENDPOINT_URL or AWS_ACCESS_KEY_ID")

// ConfigFromEnv reads the settings of a real mimir-server through getenv:
// object storage (S3, from the standard AWS variables, which must configure
// it) and the reconcile and expiry intervals (EnvReconcileInterval and
// EnvExpiryInterval, default DefaultReconcileInterval and
// DefaultExpiryInterval). Pool, Clock and Logger are left for the caller.
func ConfigFromEnv(ctx context.Context, getenv func(string) string) (Config, error) {
	if !objectstore.S3Configured(getenv) {
		return Config{}, errNoObjectStorage
	}
	store, err := objectstore.NewS3FromEnv(ctx, getenv)
	if err != nil {
		return Config{}, fmt.Errorf("configure object storage: %w", err)
	}

	cfg := Config{
		ObjectStore:       store,
		ReconcileInterval: DefaultReconcileInterval,
		ExpiryInterval:    DefaultExpiryInterval,
	}
	if cfg.ReconcileInterval, err = interval(getenv, EnvReconcileInterval, cfg.ReconcileInterval); err != nil {
		return Config{}, err
	}
	if cfg.ExpiryInterval, err = interval(getenv, EnvExpiryInterval, cfg.ExpiryInterval); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// interval reads the positive Go duration in the variable name, or returns
// def when it is unset.
func interval(getenv func(string) string, name string, def time.Duration) (time.Duration, error) {
	raw := getenv(name)
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: %q is not a positive duration", name, raw)
	}

	return d, nil
}
