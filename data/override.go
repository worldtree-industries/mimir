package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/worldtree-industries/mimir/models"
)

// ErrUnknownItem means no Item with that name and type is registered for the
// Config.
var ErrUnknownItem = errors.New("no such registered Item")

// ErrOverrideExists means the Item already has an Override for that Scope.
var ErrOverrideExists = errors.New("an Override already exists for that Item and Scope")

// CreateOverride stores o, created at time at, and returns it with its ID.
// The Item must be registered.
func (s *Store) CreateOverride(ctx context.Context, o models.Override, at time.Time) (models.Override, error) {
	err := s.q(ctx).QueryRow(ctx, `
		INSERT INTO overrides (item_id, deployable, environment, value, created_at, updated_at)
		SELECT id, NULLIF($4, ''), NULLIF($5, ''), $6, $7, $7 FROM items
		WHERE config = $1 AND name = $2 AND type = $3
		RETURNING id`,
		o.Scope.Config, o.Item.Name, string(o.Item.Type), o.Scope.Deployable, o.Scope.Environment, o.Value, at,
	).Scan(&o.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return models.Override{}, ErrUnknownItem
	case isViolation(err, uniqueViolation):
		return models.Override{}, ErrOverrideExists
	case err != nil:
		return models.Override{}, fmt.Errorf("create Override: %w", err)
	}

	return o, nil
}

// Overrides returns every Override of config's Items, in every Scope.
func (s *Store) Overrides(ctx context.Context, config string) ([]models.Override, error) {
	rows, err := s.q(ctx).Query(ctx, `
		SELECT o.id, i.config, coalesce(o.deployable, ''), coalesce(o.environment, ''), i.name, i.type, o.value
		FROM overrides o JOIN items i ON i.id = o.item_id
		WHERE i.config = $1 ORDER BY o.id`, config)
	if err != nil {
		return nil, fmt.Errorf("list Overrides of %s: %w", config, err)
	}
	defer rows.Close()

	var overrides []models.Override
	for rows.Next() {
		var o models.Override
		if err = rows.Scan(&o.ID, &o.Scope.Config, &o.Scope.Deployable, &o.Scope.Environment,
			&o.Item.Name, &o.Item.Type, &o.Value); err != nil {
			return nil, fmt.Errorf("read Override of %s: %w", config, err)
		}
		overrides = append(overrides, o)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list Overrides of %s: %w", config, err)
	}

	return overrides, nil
}
