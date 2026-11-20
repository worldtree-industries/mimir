package data

import (
	"context"
	"fmt"
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
	rows, err := s.q(ctx).Query(ctx, `SELECT name FROM environments ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list Environments: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read Environment: %w", err)
		}
		names = append(names, name)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list Environments: %w", err)
	}

	return names, nil
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
