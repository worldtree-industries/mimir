package service

import (
	"context"
	"fmt"

	"github.com/worldtree-industries/mimir/models"
)

// publishConfigLogged publishes the Snapshot of every Namespace of config.
// Failures are logged, not returned: the change that prompted publishing is
// already stored, and a later publish rebuilds the Snapshot from Postgres.
func (s *Service) publishConfigLogged(ctx context.Context, config string) {
	namespaces, err := s.store.Namespaces(ctx, config)
	if err != nil {
		s.logger.ErrorContext(ctx, "Can't publish Snapshots", "config", config, "error", err)
		return
	}

	for _, ns := range namespaces {
		s.publishLogged(ctx, ns)
	}
}

// publishLogged publishes ns's Snapshot, logging any failure.
func (s *Service) publishLogged(ctx context.Context, ns models.Namespace) {
	if err := s.publish(ctx, ns); err != nil {
		s.logger.ErrorContext(ctx, "Can't publish Snapshot",
			"config", ns.Config, "deployable", ns.Deployable, "environment", ns.Environment, "error", err)
	}
}

// publish rebuilds ns's Snapshot from Postgres and writes it.
func (s *Service) publish(ctx context.Context, ns models.Namespace) error {
	overrides, err := s.store.Overrides(ctx, ns.Config)
	if err != nil {
		return fmt.Errorf("read Overrides: %w", err)
	}

	return s.snapshots.Publish(ctx, ns, snapshotFor(ns, overrides))
}

// snapshotFor applies Precedence to overrides for ns: for each Item, the
// Override with the most specific Scope covering ns. Only Config-wide Scopes
// exist so far, so each covered Item has at most one.
func snapshotFor(ns models.Namespace, overrides []models.Override) models.Snapshot {
	var values []models.SnapshotValue
	for _, o := range overrides {
		if o.Scope.Config == ns.Config && o.Scope.ConfigWide() {
			values = append(values, models.SnapshotValue{Item: o.Item, Value: o.Value})
		}
	}

	return models.NewSnapshot(values)
}
