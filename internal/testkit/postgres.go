package testkit

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationsDir is computed from this source file's own path via
// runtime.Caller, not as a path relative to the working directory —
// `go test` sets the working directory to the *calling* package's
// directory, which is at a different depth for every caller (identity's
// postgres package vs. platform/outbox), so no single relative path
// from here could work for both.
var migrationsDir = func() string {
	_, thisFile, _, _ := runtime.Caller(0)

	// thisFile is .../internal/testkit/postgres.go; its grandparent's
	// parent is the repository root.
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	return filepath.Join(root, "db", "migrations")
}()

// protectedDatabaseNames blocks PostgreSQL's own built-in databases
// outright, regardless of prefix matching.
var protectedDatabaseNames = map[string]bool{
	"":          true,
	"postgres":  true,
	"template0": true,
	"template1": true,
}

// NewPostgresTestDB creates a fresh, isolated database on the test
// Postgres instance named by DB_DSN_TEST_ADMIN, applies every
// migration under db/migrations to it, and registers cleanup to drop
// the database once the test finishes. It skips the test if
// DB_DSN_TEST_ADMIN isn't set.
//
// prefix must be a fixed, caller-specific string (e.g.
// "app_test_identity_", "app_test_outbox_"). It both names
// the generated database and is the only thing isSafeTestDatabaseName
// trusts before allowing a CREATE/DROP DATABASE statement to run — so
// a mistaken DB_DSN_TEST_ADMIN pointed at a real environment can never
// result in data loss.
func NewPostgresTestDB(t *testing.T, prefix string) *pgxpool.Pool {
	t.Helper()

	adminDSN := os.Getenv("DB_DSN_TEST_ADMIN")
	if adminDSN == "" {
		t.Skip("DB_DSN_TEST_ADMIN not set; skipping Postgres integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	defer adminPool.Close()

	dbName := fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())

	if !isSafeTestDatabaseName(prefix, dbName) {
		t.Fatalf("refusing to operate on unsafe generated database name %q", dbName)
	}

	identifier := pgx.Identifier{dbName}.Sanitize()

	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", identifier)); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	t.Cleanup(func() {
		if !isSafeTestDatabaseName(prefix, dbName) {
			t.Fatalf("refusing to drop unsafe generated database name %q", dbName)
		}

		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cleanupPool, err := pgxpool.New(cleanupCtx, adminDSN)
		if err != nil {
			return
		}
		defer cleanupPool.Close()

		_, _ = cleanupPool.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", identifier))
	})

	testDSN, err := withDatabaseName(adminDSN, dbName)
	if err != nil {
		t.Fatalf("build test database DSN: %v", err)
	}

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	applyMigrations(t, ctx, pool)

	return pool
}

// isSafeTestDatabaseName reports whether name is safe to CREATE/DROP as
// a disposable integration-test database generated with prefix.
func isSafeTestDatabaseName(prefix, name string) bool {
	if prefix == "" || !strings.HasPrefix(name, prefix) {
		return false
	}

	return !protectedDatabaseNames[strings.ToLower(name)]
}

func withDatabaseName(dsn, dbName string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse DSN: %w", err)
	}

	parsed.Path = "/" + dbName

	return parsed.String(), nil
}

func applyMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("read migrations directory: %v", err)
	}

	var upFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, entry.Name())
		}
	}
	sort.Strings(upFiles)

	if len(upFiles) == 0 {
		t.Fatalf("no .up.sql migrations found in %s", migrationsDir)
	}

	for _, name := range upFiles {
		sql, err := os.ReadFile(filepath.Join(migrationsDir, name)) //nolint:gosec // migration names come from os.ReadDir of the repository migrations directory
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}

		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
}
