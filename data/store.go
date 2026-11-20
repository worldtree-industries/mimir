package data

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/dfryer1193/mjolnir/utils/db"
	"github.com/dfryer1193/mjolnir/utils/tx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrations are the schema changes, applied in file name order. Add a new
// file for every change; never edit one that has shipped.
//
//go:embed migrations/*.sql
var migrations embed.FS

// migrationLock is the advisory lock key that serializes migrations across
// mimir-server replicas ("mimir" in ASCII).
const migrationLock int64 = 0x6d696d6972

// Store is mimir-server's Postgres persistence. Its methods run in the
// transaction InTx put in ctx, if there is one.
type Store struct {
	pool *pgxpool.Pool
	tx   *tx.Manager
}

// NewStore returns a Store on pool.
func NewStore(pool *db.Pool) *Store {
	return &Store{pool: pool.Raw(), tx: tx.New(pool.Raw())}
}

// InTx runs fn in a transaction: every Store method called with the ctx fn
// is given takes part in it.
func (s *Store) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return s.tx.InTx(ctx, fn)
}

// Ping reports whether Postgres answers.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Migrate brings the schema up to date. Replicas starting together take
// turns; each migration is applied once.
func (s *Store) Migrate(ctx context.Context) error {
	return s.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if _, err := q.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLock); err != nil {
			return fmt.Errorf("lock migrations: %w", err)
		}
		if _, err := q.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
			return fmt.Errorf("create schema_migrations: %w", err)
		}

		names, err := fs.Glob(migrations, "migrations/*.sql")
		if err != nil {
			return fmt.Errorf("list migrations: %w", err)
		}
		slices.Sort(names)
		for _, name := range names {
			if err = s.applyMigration(ctx, name); err != nil {
				return err
			}
		}

		return nil
	})
}

func (s *Store) applyMigration(ctx context.Context, name string) error {
	q := s.q(ctx)

	var applied bool
	if err := q.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", name,
	).Scan(&applied); err != nil {
		return fmt.Errorf("check migration %s: %w", name, err)
	}
	if applied {
		return nil
	}

	sql, err := migrations.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}
	if _, err = q.Exec(ctx, string(sql)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err = q.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}

	return nil
}

// querier is what Store methods run statements on: the transaction in ctx,
// or the pool.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Store) q(ctx context.Context) querier {
	if t := tx.GetTx(ctx); t != nil {
		return t
	}

	return s.pool
}

// isViolation reports whether err is the Postgres error with SQLSTATE code.
func isViolation(err error, code string) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgErr.Code == code
}

const uniqueViolation = "23505"
