package service

import (
	"context"
	"fmt"
	"time"

	"github.com/worldtree-industries/mimir/models"
)

// ExpiryAge is how long an Item or a Namespace can go without being
// registered before it expires (ADR 0004, ADR 0006).
const ExpiryAge = 60 * 24 * time.Hour

// DefaultExpiryInterval is how often a server looks for things to expire
// unless told otherwise.
const DefaultExpiryInterval = time.Hour

// expiryCaller is who history names for what expiry removes.
const expiryCaller = "mimir-server:expiry"

// expired is what one run of Expire removed.
type expired struct {
	items int
	// overrideScopes are the Scopes of the Overrides removed: the Snapshots
	// of the Namespaces they cover must be republished.
	overrideScopes []models.Scope
	namespaces     []models.Namespace
}

// Expire removes what has gone unregistered for ExpiryAge: Items, with their
// Overrides, and Namespaces. Each Override removed is recorded in the history,
// in the same transaction as its removal. Afterwards the Snapshots of the
// Configs that lost Overrides are republished without them, and the Snapshots
// of the Namespaces that expired are deleted. Replicas take turns: one that
// finds another expiring does nothing. A failure is returned; the next run
// starts over from Postgres.
func (s *Service) Expire(ctx context.Context) error {
	ctx = WithCaller(ctx, expiryCaller)
	cutoff := s.clock.Now().Add(-ExpiryAge)

	var (
		gone expired
		ran  bool
	)
	err := s.store.InTx(ctx, func(ctx context.Context) error {
		locked, err := s.store.TryLockExpiry(ctx)
		if err != nil || !locked {
			return err
		}
		ran = true
		gone, err = s.expireInTx(ctx, cutoff)

		return err
	})
	if err != nil {
		return fmt.Errorf("expire: %w", err)
	}
	if !ran {
		return nil
	}

	if gone.items > 0 || len(gone.namespaces) > 0 {
		s.logger.InfoContext(ctx, "Expired what went unregistered",
			"items", gone.items, "namespaces", len(gone.namespaces))
	}
	for _, ns := range gone.namespaces {
		if err = s.snapshots.Delete(ctx, ns); err != nil {
			s.logger.ErrorContext(ctx, "Can't delete the Snapshot of an expired Namespace",
				"namespace", ns, "error", err)
		}
	}
	for _, scope := range gone.overrideScopes {
		s.republishScope(ctx, scope)
	}

	return nil
}

// expireInTx removes what went unregistered since cutoff, recording the
// removal of each Override. It runs in a transaction.
func (s *Service) expireInTx(ctx context.Context, cutoff time.Time) (expired, error) {
	var gone expired

	items, err := s.store.ExpiredItems(ctx, cutoff)
	if err != nil {
		return expired{}, err
	}
	for _, item := range items {
		for _, o := range item.Overrides {
			if err = s.recordHistory(ctx, models.HistoryExpired, o.Scope, o.Item, &o.Value, nil); err != nil {
				return expired{}, err
			}
			gone.overrideScopes = append(gone.overrideScopes, o.Scope)
		}
		if err = s.store.DeleteItem(ctx, item.ID); err != nil {
			return expired{}, err
		}
	}
	gone.items = len(items)

	gone.namespaces, err = s.store.ExpireNamespaces(ctx, cutoff)
	if err != nil {
		return expired{}, err
	}

	return gone, nil
}

// RunExpiry calls Expire every interval, by the Service's clock, until ctx is
// done. Failures are logged. finished, if not nil, is called after each run.
func (s *Service) RunExpiry(ctx context.Context, interval time.Duration, finished func()) {
	s.every(ctx, interval, func(ctx context.Context) {
		if err := s.Expire(ctx); err != nil {
			s.logger.ErrorContext(ctx, "Can't expire what went unregistered", "error", err)
		}
	}, finished)
}
