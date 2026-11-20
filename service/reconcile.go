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
		s.publishLogged(ctx, ns)
	}
}

// RunReconcile calls Reconcile every interval, by the Service's clock, until
// ctx is done. onReconcile, if not nil, is called after each one.
func (s *Service) RunReconcile(ctx context.Context, interval time.Duration, onReconcile func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.clock.After(interval):
		}

		s.Reconcile(ctx)
		if onReconcile != nil {
			onReconcile()
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

// Ready reports whether Postgres and object storage both answer. It checks at
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

	// Object storage is probed through the bucket of a Namespace we publish
	// to; with none known there is nothing to publish yet.
	ns, ok, err := s.store.AnyNamespace(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "Not ready: Postgres can't answer", "error", err)
		return false
	}
	if !ok {
		return true
	}
	if err = s.snapshots.Reachable(ctx, ns); err != nil {
		s.logger.WarnContext(ctx, "Not ready: object storage unreachable", "error", err)
		return false
	}

	return true
}
