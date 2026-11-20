// Package server assembles mimir-server: it migrates Postgres, builds the
// data, service and api Layers, and puts their routes on mjolnir's router.
// cmd/mimir-server runs it for real; the system tests run it in-process.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dfryer1193/mjolnir/health"
	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/db"

	"github.com/worldtree-industries/mimir/api"
	"github.com/worldtree-industries/mimir/data"
	"github.com/worldtree-industries/mimir/internal/clock"
	"github.com/worldtree-industries/mimir/internal/objectstore"
	"github.com/worldtree-industries/mimir/models"
	"github.com/worldtree-industries/mimir/service"
)

// Config is everything mimir-server runs on.
type Config struct {
	// Pool is the Postgres pool. New migrates its schema.
	Pool *db.Pool
	// ObjectStore is where Snapshots are published.
	ObjectStore objectstore.Store
	// Clock is the server's time source.
	Clock clock.Clock
	// Logger receives the server's logs, including access lines.
	Logger *slog.Logger
	// ReconcileInterval is how often every Snapshot is republished from
	// Postgres. Zero means DefaultReconcileInterval.
	ReconcileInterval time.Duration
	// OnReconcile, if set, is called after each reconcile. Tests use it to
	// know one finished.
	OnReconcile func()
}

// DefaultReconcileInterval is how often Snapshots are republished by default.
const DefaultReconcileInterval = service.DefaultReconcileInterval

// ReadinessCacheTTL is how long /readyz repeats its last answer.
const ReadinessCacheTTL = service.ReadyCacheTTL

// New migrates the database and returns mimir-server's router, ready to
// serve. It also starts the periodic reconcile, which runs until ctx is done.
func New(ctx context.Context, cfg Config) (*router.Router, error) {
	store := data.NewStore(cfg.Pool)
	if err := store.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	svc := service.New(service.Config{
		Store:     store,
		Snapshots: data.NewSnapshots(cfg.ObjectStore, models.DefaultBucketPrefix),
		Clock:     cfg.Clock,
		Logger:    cfg.Logger,
	})

	r, err := router.New(router.Config{
		Health: health.Config{
			Started: health.AlwaysStarted,
			Ready:   svc.Ready,
		},
		AccessLogger: cfg.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("build router: %w", err)
	}
	api.Routes(r, svc, cfg.Logger)

	interval := cfg.ReconcileInterval
	if interval <= 0 {
		interval = DefaultReconcileInterval
	}
	go svc.RunReconcile(ctx, interval, cfg.OnReconcile)

	return r, nil
}
