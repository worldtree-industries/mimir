package systemtest

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// dropTimeout bounds dropping a throwaway database at cleanup, after the
// test's context is gone.
const dropTimeout = 30 * time.Second

// nameRandomBytes is how much randomness a throwaway database's name holds.
const nameRandomBytes = 8

// throwawayDatabase creates an empty database on the server DATABASE_URL
// points at, drops it when the test ends, and returns a DSN for it. It skips
// the test when DATABASE_URL is unset.
func throwawayDatabase(t *testing.T) string {
	t.Helper()

	adminDSN := os.Getenv("DATABASE_URL")
	if adminDSN == "" {
		t.Skip("DATABASE_URL is not set; skipping the Postgres-backed system test")
	}

	conn, err := pgx.Connect(t.Context(), adminDSN)
	if err != nil {
		t.Fatalf("connect to DATABASE_URL: %v", err)
	}
	defer func() { _ = conn.Close(t.Context()) }()

	name := "mimir_test_" + randomHex(t)
	if _, err = conn.Exec(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}

	t.Cleanup(func() {
		ctx, cancel := contextWithTimeout(dropTimeout)
		defer cancel()

		dropConn, connErr := pgx.Connect(ctx, adminDSN)
		if connErr != nil {
			t.Errorf("connect to drop database %s: %v", name, connErr)
			return
		}
		defer func() { _ = dropConn.Close(ctx) }()

		if _, dropErr := dropConn.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); dropErr != nil {
			t.Errorf("drop database %s: %v", name, dropErr)
		}
	})

	return withDatabase(t, adminDSN, name)
}

// withDatabase returns dsn pointing at database name instead, for both URL
// and keyword/value DSNs.
func withDatabase(t *testing.T, dsn, name string) string {
	t.Helper()

	if !strings.Contains(dsn, "://") {
		return dsn + " dbname=" + name
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	u.Path = "/" + name

	return u.String()
}

func randomHex(t *testing.T) string {
	t.Helper()

	b := make([]byte, nameRandomBytes)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}

	return hex.EncodeToString(b)
}
