package service

import (
	"context"
	"fmt"

	"github.com/worldtree-industries/mimir/models"
)

// publishScopeLogged publishes the Snapshot of every Namespace scope covers
// and no other. Failures are logged, not returned: the change that prompted
// publishing is already stored, and a later publish rebuilds the Snapshot
// from Postgres.
func (s *Service) publishScopeLogged(ctx context.Context, scope models.Scope) {
	namespaces, err := s.store.Namespaces(ctx, scope.Config)
	if err != nil {
		s.logger.ErrorContext(ctx, "Can't publish Snapshots", "config", scope.Config, "error", err)
		return
	}
	overrides, err := s.store.Overrides(ctx, scope.Config)
	if err != nil {
		s.logger.ErrorContext(ctx, "Can't publish Snapshots", "config", scope.Config, "error", err)
		return
	}

	for _, ns := range namespaces {
		if !scope.Covers(ns) {
			continue
		}
		err = s.snapshots.Publish(ctx, ns, snapshotFor(ns, overrides))
		if err == nil {
			err = s.deleteIfExpired(ctx, ns)
		}
		if err != nil {
			s.logPublishFailure(ctx, ns, err)
		}
	}
}

// publishLogged publishes ns's Snapshot, logging any failure.
func (s *Service) publishLogged(ctx context.Context, ns models.Namespace) {
	if err := s.publish(ctx, ns); err != nil {
		s.logPublishFailure(ctx, ns, err)
	}
}

func (s *Service) logPublishFailure(ctx context.Context, ns models.Namespace, err error) {
	s.logger.ErrorContext(ctx, "Can't publish Snapshot",
		"config", ns.Config, "deployable", ns.Deployable, "environment", ns.Environment, "error", err)
}

// publish rebuilds ns's Snapshot from Postgres and writes it.
func (s *Service) publish(ctx context.Context, ns models.Namespace) error {
	overrides, err := s.store.Overrides(ctx, ns.Config)
	if err != nil {
		return fmt.Errorf("read Overrides: %w", err)
	}

	if err = s.snapshots.Publish(ctx, ns, snapshotFor(ns, overrides)); err != nil {
		return err
	}

	return s.deleteIfExpired(ctx, ns)
}

// deleteIfExpired deletes ns's Snapshot if ns is no longer known: expiry
// removes a Namespace's row before its Snapshot, so a publish that began
// before and finished after would otherwise leave a Snapshot nothing will
// ever delete. Checking after the upload covers either order.
func (s *Service) deleteIfExpired(ctx context.Context, ns models.Namespace) error {
	known, err := s.store.NamespaceKnown(ctx, ns)
	if err != nil {
		return fmt.Errorf("check Namespace is still registered: %w", err)
	}
	if known {
		return nil
	}

	return s.snapshots.Delete(ctx, ns)
}

// snapshotFor applies Precedence (ADR 0003) to overrides for ns: for each
// Item, the Override with the most specific Scope covering ns.
func snapshotFor(ns models.Namespace, overrides []models.Override) models.Snapshot {
	winners := models.Precedence(ns, overrides)
	values := make([]models.SnapshotValue, 0, len(winners))
	for key, o := range winners {
		values = append(values, models.SnapshotValue{Item: key, Value: o.Value})
	}

	return models.NewSnapshot(values)
}
