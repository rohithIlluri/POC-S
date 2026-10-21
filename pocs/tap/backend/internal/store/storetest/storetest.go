// Package storetest hands tests a Store: in-memory by default, or a fresh
// Postgres schema per test when TAP_TEST_PG holds a connection URL.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/rohithIlluri/POC-S/pocs/tap/backend/internal/store"
)

// Kind reports which backend New returns, for test names and logs.
func Kind() string {
	if os.Getenv("TAP_TEST_PG") != "" {
		return "postgres"
	}
	return "memory"
}

func New(t testing.TB) store.Store {
	t.Helper()
	url := os.Getenv("TAP_TEST_PG")
	if url == "" {
		return store.NewMemory()
	}
	ctx := context.Background()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "tap_test_" + hex.EncodeToString(b)

	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("TAP_TEST_PG: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	pg, err := store.OpenPostgres(ctx, url+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pg.Close()
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close(ctx)
	})
	return pg
}
