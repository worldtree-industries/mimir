package data

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/worldtree-industries/mimir/models"
)

// ConfigInfos returns every known Config, by name.
func (s *Store) ConfigInfos(ctx context.Context) ([]models.ConfigInfo, error) {
	return list(ctx, s.q(ctx), "Configs", func(row pgx.Row) (models.ConfigInfo, error) {
		var c models.ConfigInfo
		err := row.Scan(&c.Name, &c.DisplayName, &c.LastRegisteredAt)

		return c, err
	}, `SELECT name, display_name, registered_at FROM configs ORDER BY name`)
}

// RegisteredItems returns every known Item of config, or of every Config when
// config is empty, by Config, name and type.
func (s *Store) RegisteredItems(ctx context.Context, config string) ([]models.RegisteredItem, error) {
	return list(ctx, s.q(ctx), "Items", func(row pgx.Row) (models.RegisteredItem, error) {
		var i models.RegisteredItem
		err := row.Scan(&i.Config, &i.Name, &i.Type, &i.Description, &i.Default,
			&i.FirstRegisteredAt, &i.LastRegisteredAt)

		return i, err
	}, `
		SELECT config, name, type, description, default_value, first_registered_at, last_registered_at
		FROM items WHERE $1 = '' OR config = $1 ORDER BY config, name, type`, config)
}

// RegisteredNamespaces returns every known Namespace of config, or of every
// Config when config is empty, by Config, Deployable and Environment.
func (s *Store) RegisteredNamespaces(ctx context.Context, config string) ([]models.RegisteredNamespace, error) {
	return list(ctx, s.q(ctx), "Namespaces", func(row pgx.Row) (models.RegisteredNamespace, error) {
		var n models.RegisteredNamespace
		err := row.Scan(&n.Config, &n.Deployable, &n.Environment, &n.FirstRegisteredAt, &n.LastRegisteredAt)

		return n, err
	}, `
		SELECT `+namespaceColumns+`, first_registered_at, last_registered_at
		FROM namespaces WHERE $1 = '' OR config = $1 ORDER BY config, deployable, environment`, config)
}
