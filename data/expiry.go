package data

import (
	"context"
	"fmt"
	"time"

	"github.com/worldtree-industries/mimir/models"
)

// expiryLock is the advisory lock key that lets one mimir-server replica at a
// time expire things ("mimir" in ASCII, plus one).
const expiryLock int64 = 0x6d696d6973

// TryLockExpiry takes, for the rest of the transaction in ctx, the lock that
// lets one replica at a time run expiry. It reports false when another
// replica holds it.
func (s *Store) TryLockExpiry(ctx context.Context) (bool, error) {
	var locked bool
	if err := s.q(ctx).QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", expiryLock).Scan(&locked); err != nil {
		return false, fmt.Errorf("lock expiry: %w", err)
	}

	return locked, nil
}

// ExpiredItem is an Item to expire, with the Overrides that go with it.
type ExpiredItem struct {
	ID        int64
	Config    string
	Ref       models.ItemRef
	Overrides []models.Override
}

// ExpiredItems returns the Items last registered at or before cutoff, with
// their Overrides, and locks them for the transaction in ctx.
func (s *Store) ExpiredItems(ctx context.Context, cutoff time.Time) ([]ExpiredItem, error) {
	q := s.q(ctx)

	rows, err := q.Query(ctx, `
		SELECT id, config, name, type FROM items
		WHERE last_registered_at <= $1 ORDER BY id FOR UPDATE`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("find expired Items: %w", err)
	}
	defer rows.Close()

	var items []ExpiredItem
	byID := map[int64]int{}
	var ids []int64
	for rows.Next() {
		var item ExpiredItem
		if err = rows.Scan(&item.ID, &item.Config, &item.Ref.Name, &item.Ref.Type); err != nil {
			return nil, fmt.Errorf("read expired Item: %w", err)
		}
		byID[item.ID] = len(items)
		ids = append(ids, item.ID)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("find expired Items: %w", err)
	}
	rows.Close()
	if len(items) == 0 {
		return nil, nil
	}

	orows, err := q.Query(ctx, `
		SELECT id, item_id, coalesce(deployable, ''), coalesce(environment, ''), value
		FROM overrides WHERE item_id = ANY($1) ORDER BY id`, ids)
	if err != nil {
		return nil, fmt.Errorf("find Overrides of expired Items: %w", err)
	}
	defer orows.Close()

	for orows.Next() {
		var (
			o      models.Override
			itemID int64
		)
		if err = orows.Scan(&o.ID, &itemID, &o.Scope.Deployable, &o.Scope.Environment, &o.Value); err != nil {
			return nil, fmt.Errorf("read Override of an expired Item: %w", err)
		}
		item := &items[byID[itemID]]
		o.Scope.Config = item.Config
		o.Item = item.Ref
		item.Overrides = append(item.Overrides, o)
	}
	if err = orows.Err(); err != nil {
		return nil, fmt.Errorf("find Overrides of expired Items: %w", err)
	}

	return items, nil
}

// DeleteItem removes the Item with id and, with it, its Overrides.
func (s *Store) DeleteItem(ctx context.Context, id int64) error {
	if _, err := s.q(ctx).Exec(ctx, "DELETE FROM items WHERE id = $1", id); err != nil {
		return fmt.Errorf("delete Item %d: %w", id, err)
	}

	return nil
}

// ExpireNamespaces removes the Namespaces last registered at or before
// cutoff, and returns them.
func (s *Store) ExpireNamespaces(ctx context.Context, cutoff time.Time) ([]models.Namespace, error) {
	rows, err := s.q(ctx).Query(ctx, `
		DELETE FROM namespaces WHERE last_registered_at <= $1
		RETURNING config, deployable, environment`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("expire Namespaces: %w", err)
	}
	defer rows.Close()

	var namespaces []models.Namespace
	for rows.Next() {
		var ns models.Namespace
		if err = rows.Scan(&ns.Config, &ns.Deployable, &ns.Environment); err != nil {
			return nil, fmt.Errorf("read expired Namespace: %w", err)
		}
		namespaces = append(namespaces, ns)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("expire Namespaces: %w", err)
	}

	return namespaces, nil
}
