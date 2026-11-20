// Command mimir-server is an HTTP server, a Deployable. It accepts
// Registration and the admin API, stores Overrides in Postgres
// (DATABASE_URL), and publishes Snapshots to object storage. It serves
// mjolnir's probes (/livez, /readyz and /startupz) and drains gracefully on
// SIGTERM.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/dfryer1193/mjolnir/router"
	"github.com/dfryer1193/mjolnir/utils/db"
	"github.com/dfryer1193/mjolnir/utils/log"

	"github.com/worldtree-industries/mimir/internal/clock"
	"github.com/worldtree-industries/mimir/internal/objectstore"
	"github.com/worldtree-industries/mimir/internal/server"
)

// defaultPort is the port mimir-server listens on when PORT isn't set.
const defaultPort = "8080"

// drainBudget is the whole shutdown budget: half with readiness at 503, half
// for Shutdown. Keep it under the orchestrator's termination grace period.
const drainBudget = 10 * time.Second

func main() {
	lg := log.GetLogger("mimir-server")
	if err := run(context.Background(), lg); err != nil {
		lg.Error("Server failed", "error", err)
		os.Exit(1)
	}
	lg.Info("Server stopped")
}

func run(ctx context.Context, lg *slog.Logger) error {
	pool, err := db.Connect(ctx, os.Getenv("DATABASE_URL"), db.DefaultConfig())
	if err != nil {
		return fmt.Errorf("connect to Postgres: %w", err)
	}
	defer pool.Close()

	// TODO(#16): publish to S3-API object storage. Until then Snapshots only
	// live in this process, so no client can read them.
	lg.WarnContext(ctx, "No object storage configured; Snapshots are kept in memory only")

	r, err := server.New(ctx, server.Config{
		Pool:        pool,
		ObjectStore: objectstore.NewMemory(),
		Clock:       clock.Real(),
		Logger:      lg,
	})
	if err != nil {
		return err
	}

	addr := net.JoinHostPort("", port())
	lg.InfoContext(ctx, "Server starting", "addr", addr)

	return r.Run(router.NewServer(addr, r), drainBudget)
}

// port is the port to listen on: PORT if set, otherwise defaultPort.
func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return defaultPort
}
