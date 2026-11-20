package data

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// EnsureEnvironments creates every named Environment that doesn't exist yet.
// Replicas starting together can all call it.
func (s *Store) EnsureEnvironments(ctx context.Context, names []string) error {
	for _, name := range names {
		if _, err := s.q(ctx).Exec(ctx,
			`INSERT INTO environments (name) VALUES ($1) ON CONFLICT DO NOTHING`, name); err != nil {
			return fmt.Errorf("create Environment %s: %w", name, err)
		}
	}

	return nil
}

// Environments lists the Environment names, in name order.
func (s *Store) Environments(ctx context.Context) ([]string, error) {
	return list(ctx, s.q(ctx), "Environments", func(row pgx.Row) (string, error) {
		var name string
		err := row.Scan(&name)

		return name, err
	}, `SELECT name FROM environments ORDER BY name`)
}

// HasEnvironment reports whether the Environment exists.
func (s *Store) HasEnvironment(ctx context.Context, name string) (bool, error) {
	var ok bool
	if err := s.q(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM environments WHERE name = $1)`, name).Scan(&ok); err != nil {
		return false, fmt.Errorf("look up Environment %s: %w", name, err)
	}

	return ok, nil
}
