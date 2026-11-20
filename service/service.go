package service

import (
	"errors"
	"log/slog"

	"github.com/worldtree-industries/mimir/data"
	"github.com/worldtree-industries/mimir/internal/clock"
)

// ErrInvalid means a request is malformed or incomplete. It is wrapped with
// the reason.
var ErrInvalid = errors.New("invalid")

// ErrUnknownItem means an Override names an Item that isn't registered.
var ErrUnknownItem = errors.New("the Item isn't registered for that Config")

// ErrOverrideExists means the Item already has an Override for that Scope.
var ErrOverrideExists = errors.New("the Item already has an Override for that Scope")

// ErrOverrideNotFound means there is no Override with the given ID.
var ErrOverrideNotFound = errors.New("no such Override")

// ErrUnknownNamespace means the Namespace has never registered.
var ErrUnknownNamespace = errors.New("no such Namespace")

// Config is what a Service is built from.
type Config struct {
	Store     *data.Store
	Snapshots *data.Snapshots
	Clock     clock.Clock
	Logger    *slog.Logger
}

// Service is mimir-server's business logic: Registration, Overrides and
// publishing Snapshots. Postgres is the source of truth; every Snapshot is
// rebuilt whole from it, so publishing is idempotent and replicas need no
// coordination.
type Service struct {
	store     *data.Store
	snapshots *data.Snapshots
	clock     clock.Clock
	logger    *slog.Logger
}

// New returns a Service.
func New(cfg Config) *Service {
	return &Service{store: cfg.Store, snapshots: cfg.Snapshots, clock: cfg.Clock, logger: cfg.Logger}
}
