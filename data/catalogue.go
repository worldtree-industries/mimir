package data

import (
	"context"
	"fmt"

	"github.com/worldtree-industries/mimir/models"
)

// ConfigInfos returns every known Config, by name.
func (s *Store) ConfigInfos(ctx context.Context) ([]models.ConfigInfo, error) {
	rows, err := s.q(ctx).Query(ctx, `SELECT name, display_name, registered_at FROM configs ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list Configs: %w", err)
	}
	defer rows.Close()

	configs := []models.ConfigInfo{}
	for rows.Next() {
		var c models.ConfigInfo
		if err = rows.Scan(&c.Name, &c.DisplayName, &c.LastRegisteredAt); err != nil {
			return nil, fmt.Errorf("read Config: %w", err)
		}
		configs = append(configs, c)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list Configs: %w", err)
	}

	return configs, nil
}

// RegisteredItems returns every known Item of config, or of every Config when
// config is empty, by Config, name and type.
func (s *Store) RegisteredItems(ctx context.Context, config string) ([]models.RegisteredItem, error) {
	rows, err := s.q(ctx).Query(ctx, `
		SELECT config, name, type, description, default_value, first_registered_at, last_registered_at
		FROM items WHERE $1 = '' OR config = $1 ORDER BY config, name, type`, config)
	if err != nil {
		return nil, fmt.Errorf("list Items: %w", err)
	}
	defer rows.Close()

	items := []models.RegisteredItem{}
	for rows.Next() {
		var i models.RegisteredItem
		if err = rows.Scan(&i.Config, &i.Name, &i.Type, &i.Description, &i.Default,
			&i.FirstRegisteredAt, &i.LastRegisteredAt); err != nil {
			return nil, fmt.Errorf("read Item: %w", err)
		}
		items = append(items, i)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list Items: %w", err)
	}

	return items, nil
}

// RegisteredNamespaces returns every known Namespace of config, or of every
// Config when config is empty, by Config, Deployable and Environment.
func (s *Store) RegisteredNamespaces(ctx context.Context, config string) ([]models.RegisteredNamespace, error) {
	rows, err := s.q(ctx).Query(ctx, `
		SELECT config, deployable, environment, first_registered_at, last_registered_at
		FROM namespaces WHERE $1 = '' OR config = $1 ORDER BY config, deployable, environment`, config)
	if err != nil {
		return nil, fmt.Errorf("list Namespaces: %w", err)
	}
	defer rows.Close()

	namespaces := []models.RegisteredNamespace{}
	for rows.Next() {
		var n models.RegisteredNamespace
		if err = rows.Scan(&n.Config, &n.Deployable, &n.Environment,
			&n.FirstRegisteredAt, &n.LastRegisteredAt); err != nil {
			return nil, fmt.Errorf("read Namespace: %w", err)
		}
		namespaces = append(namespaces, n)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list Namespaces: %w", err)
	}

	return namespaces, nil
}
