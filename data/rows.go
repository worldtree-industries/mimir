package data

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/worldtree-industries/mimir/models"
)

// list runs the query sql on q and reads every row it returns with scan.
// what names the rows in errors. An empty result is an empty, non-nil slice.
func list[T any](
	ctx context.Context, q querier, what string, scan func(pgx.Row) (T, error), sql string, args ...any,
) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", what, err)
	}
	all, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (T, error) { return scan(row) })
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", what, err)
	}

	return all, nil
}

// overrideColumns are what scanOverride reads, from an Override's row o and
// its Item's row i.
const overrideColumns = `o.id, i.config, coalesce(o.deployable, ''), coalesce(o.environment, ''), i.name, i.type, o.value`

// scanOverride reads an Override from a row of overrideColumns.
func scanOverride(row pgx.Row) (models.Override, error) {
	var o models.Override
	err := row.Scan(&o.ID, &o.Scope.Config, &o.Scope.Deployable, &o.Scope.Environment,
		&o.Item.Name, &o.Item.Type, &o.Value)

	return o, err
}

// namespaceColumns are what scanNamespace reads.
const namespaceColumns = `config, deployable, environment`

// scanNamespace reads a Namespace from a row of namespaceColumns.
func scanNamespace(row pgx.Row) (models.Namespace, error) {
	var ns models.Namespace
	err := row.Scan(&ns.Config, &ns.Deployable, &ns.Environment)

	return ns, err
}
