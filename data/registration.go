package data

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/worldtree-industries/mimir/models"
)

// RecordRegistration records reg at time at: its Config, every Item it
// declares, and its Namespace, each marked as registered at that time.
// Registration only adds; nothing reg leaves out is removed. An Item keeps
// one description and Default, the ones registered last, so under mixed
// versions they follow whichever registered most recently. It reports whether
// the Namespace is new.
func (s *Store) RecordRegistration(ctx context.Context, reg models.Registration, at time.Time) (bool, error) {
	q := s.q(ctx)
	ns := reg.Namespace

	if _, err := q.Exec(ctx, `
		INSERT INTO configs (name, display_name, registered_at) VALUES ($1, $2, $3)
		ON CONFLICT (name) DO UPDATE SET display_name = EXCLUDED.display_name, registered_at = EXCLUDED.registered_at`,
		ns.Config, reg.DisplayName, at,
	); err != nil {
		return false, fmt.Errorf("record Config %s: %w", ns.Config, err)
	}

	for _, item := range reg.Items {
		if _, err := q.Exec(ctx, `
			INSERT INTO items (config, name, type, description, default_value, first_registered_at, last_registered_at)
			VALUES ($1, $2, $3, $4, $5, $6, $6)
			ON CONFLICT (config, name, type) DO UPDATE SET
				description = EXCLUDED.description,
				default_value = EXCLUDED.default_value,
				last_registered_at = EXCLUDED.last_registered_at`,
			ns.Config, item.Name, string(item.Type), item.Description, item.Default, at,
		); err != nil {
			return false, fmt.Errorf("record Item %s (%s): %w", item.Name, item.Type, err)
		}
	}

	// xmax is 0 only on a row this statement inserted.
	var inserted bool
	if err := q.QueryRow(ctx, `
		INSERT INTO namespaces (config, deployable, environment, first_registered_at, last_registered_at)
		VALUES ($1, $2, $3, $4, $4)
		ON CONFLICT (config, deployable, environment) DO UPDATE SET last_registered_at = EXCLUDED.last_registered_at
		RETURNING xmax = 0`,
		ns.Config, ns.Deployable, ns.Environment, at,
	).Scan(&inserted); err != nil {
		return false, fmt.Errorf("record Namespace: %w", err)
	}

	return inserted, nil
}

// Namespaces returns every known Namespace of config.
func (s *Store) Namespaces(ctx context.Context, config string) ([]models.Namespace, error) {
	return list(ctx, s.q(ctx), "Namespaces of "+config, scanNamespace, `
		SELECT `+namespaceColumns+` FROM namespaces
		WHERE config = $1 ORDER BY deployable, environment`, config)
}

// Items returns every Item registered for config, with its Default.
func (s *Store) Items(ctx context.Context, config string) ([]models.Item, error) {
	return list(ctx, s.q(ctx), "Items of "+config, func(row pgx.Row) (models.Item, error) {
		var item models.Item
		err := row.Scan(&item.Name, &item.Description, &item.Type, &item.Default)

		return item, err
	}, `
		SELECT name, description, type, default_value FROM items
		WHERE config = $1 ORDER BY name, type`, config)
}

// HasNamespace reports whether ns is a known Namespace.
func (s *Store) HasNamespace(ctx context.Context, ns models.Namespace) (bool, error) {
	var exists bool
	err := s.q(ctx).QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM namespaces WHERE config = $1 AND deployable = $2 AND environment = $3)`,
		ns.Config, ns.Deployable, ns.Environment,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("look up Namespace: %w", err)
	}

	return exists, nil
}

// AllNamespaces returns every known Namespace.
func (s *Store) AllNamespaces(ctx context.Context) ([]models.Namespace, error) {
	return list(ctx, s.q(ctx), "Namespaces", scanNamespace, `
		SELECT `+namespaceColumns+` FROM namespaces
		ORDER BY config, deployable, environment`)
}

// NamespaceKnown reports whether ns is registered.
func (s *Store) NamespaceKnown(ctx context.Context, ns models.Namespace) (bool, error) {
	var known bool
	if err := s.q(ctx).QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM namespaces WHERE config = $1 AND deployable = $2 AND environment = $3)`,
		ns.Config, ns.Deployable, ns.Environment,
	).Scan(&known); err != nil {
		return false, fmt.Errorf("look up Namespace: %w", err)
	}

	return known, nil
}
