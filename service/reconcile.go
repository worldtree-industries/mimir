package service

import (
	"context"
	"sync"
	"time"
)

// DefaultReconcileInterval is how often a server republishes every Snapshot
// unless told otherwise.
const DefaultReconcileInterval = 5 * time.Minute

// Reconcile republishes the Snapshot of every known Namespace from Postgres.
// It corrects a Snapshot that a failed upload left stale or missing, and one
// that replicas racing to publish left out of order. Failures are logged and
// the next Reconcile tries again.
func (s *Service) Reconcile(ctx context.Context) {
	namespaces, err := s.store.AllNamespaces(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Can't reconcile Snapshots", "error", err)
		return
	}

	for _, ns := range namespaces {
		s.republishNamespace(ctx, ns)
	}
}

// RunReconcile calls Reconcile every interval, by the Service's clock, until
// ctx is done. finished, if not nil, is called after each one.
func (s *Service) RunReconcile(ctx context.Context, interval time.Duration, finished func()) {
	s.every(ctx, interval, s.Reconcile, finished)
}

// every calls run every interval, by the Service's clock, until ctx is done,
// and finished, if not nil, after each run.
func (s *Service) every(ctx context.Context, interval time.Duration, run func(context.Context), finished func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.clock.After(interval):
		}

		run(ctx)
		if finished != nil {
			finished()
		}
	}
}

// readyCache remembers the last answer to Ready for readyCacheTTL.
type readyCache struct {
	mu      sync.Mutex
	until   time.Time
	ready   bool
	checked bool
}

// ReadyCacheTTL is how long Ready remembers its answer, so probes stay cheap
// however often they come.
const ReadyCacheTTL = 5 * time.Second

// readyTimeout bounds one check of the dependencies.
const readyTimeout = 3 * time.Second

// Ready reports whether Postgres and object storage (every Environment's
// bucket) answer. It checks at
// most once per ReadyCacheTTL and otherwise repeats the last answer.
func (s *Service) Ready(ctx context.Context) bool {
	c := &s.ready
	c.mu.Lock()
	defer c.mu.Unlock()

	now := s.clock.Now()
	if c.checked && now.Before(c.until) {
		return c.ready
	}

	c.ready = s.checkDependencies(ctx)
	c.checked = true
	c.until = now.Add(ReadyCacheTTL)

	return c.ready
}

func (s *Service) checkDependencies(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()

	if err := s.store.Ping(ctx); err != nil {
		s.logger.WarnContext(ctx, "Not ready: Postgres unreachable", "error", err)
		return false
	}

	// Object storage is probed through every Environment's bucket, since a
	// Snapshot may be published to any of them, even before anything has
	// registered.
	environments, err := s.store.Environments(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "Not ready: Postgres can't answer", "error", err)
		return false
	}
	for _, environment := range environments {
		if err = s.snapshots.Reachable(ctx, environment); err != nil {
			s.logger.WarnContext(ctx, "Not ready: object storage unreachable", "error", err)
			return false
		}
	}

	return true
}
