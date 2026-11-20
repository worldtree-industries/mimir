package data

import (
	"context"
	"fmt"

	"github.com/worldtree-industries/mimir/models"
)

// AppendHistory adds e to the history, whose ID it ignores, and returns the
// entry with its ID. History is append-only: there is no way to change or
// remove an entry.
func (s *Store) AppendHistory(ctx context.Context, e models.HistoryEntry) (models.HistoryEntry, error) {
	err := s.q(ctx).QueryRow(ctx, `
		INSERT INTO history (at, action, config, deployable, environment, item_name, item_type,
			old_value, new_value, caller)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		e.Time, string(e.Action), e.Scope.Config, e.Scope.Deployable, e.Scope.Environment,
		e.Item.Name, string(e.Item.Type), e.OldValue, e.NewValue, e.Caller,
	).Scan(&e.ID)
	if err != nil {
		return models.HistoryEntry{}, fmt.Errorf("append history: %w", err)
	}

	return e, nil
}

// History returns the entries matching filter, oldest first. An empty field
// of filter matches every value.
func (s *Store) History(ctx context.Context, filter models.Scope) ([]models.HistoryEntry, error) {
	rows, err := s.q(ctx).Query(ctx, `
		SELECT id, at, action, config, deployable, environment, item_name, item_type,
			old_value, new_value, caller
		FROM history
		WHERE ($1 = '' OR config = $1) AND ($2 = '' OR deployable = $2) AND ($3 = '' OR environment = $3)
		ORDER BY id`,
		filter.Config, filter.Deployable, filter.Environment)
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	defer rows.Close()

	entries := []models.HistoryEntry{}
	for rows.Next() {
		var e models.HistoryEntry
		if err = rows.Scan(&e.ID, &e.Time, &e.Action, &e.Scope.Config, &e.Scope.Deployable,
			&e.Scope.Environment, &e.Item.Name, &e.Item.Type, &e.OldValue, &e.NewValue, &e.Caller); err != nil {
			return nil, fmt.Errorf("read history entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}

	return entries, nil
}
