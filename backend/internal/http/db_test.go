package http

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/AGlElectronics/tasks/backend/migrations"
)

// A lock wait fails the test instead of sitting until the package timeout.
// An idle transaction is dropped before the next test's reset needs the tables.
const (
	testLockTimeout   = "30s"
	testIdleTxTimeout = "15s"
)

// TestMain migrates the disposable database once. Rebuilding it in every
// test made the package slow enough to hit Go's 10 minute timeout, and
// DROP SCHEMA public waits for any session that still holds a lock.
func TestMain(m *testing.M) {
	if os.Getenv("TEST_DATABASE_URL") != "" {
		if err := prepareTestDatabase(); err != nil {
			fmt.Fprintf(os.Stderr, "test database: %v\n", err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

func prepareTestDatabase() error {
	ctx := context.Background()
	db, err := openTestDB()
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS public CASCADE`); err != nil {
		_ = db.Close()
		return fmt.Errorf("drop public schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA public`); err != nil {
		_ = db.Close()
		return fmt.Errorf("create public schema: %w", err)
	}
	// The session cached the schema that was just dropped.
	if err := db.Close(); err != nil {
		return fmt.Errorf("close database after recreating public: %w", err)
	}

	db, err = openTestDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("goose_db_version")
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// resetTestData empties every application table and puts the seeded
// organisation back. It does not change the schema.
func resetTestData(ctx context.Context, db *sql.DB) error {
	var tables sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT string_agg(format('%I.%I', schemaname, tablename), ', ')
		  FROM pg_tables
		 WHERE schemaname = 'public' AND tablename <> 'goose_db_version'`).Scan(&tables); err != nil {
		return err
	}
	if !tables.Valid {
		return fmt.Errorf("public schema has no application tables")
	}
	if _, err := db.ExecContext(ctx, "TRUNCATE TABLE "+tables.String+" RESTART IDENTITY CASCADE"); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO organizations (id, slug, display_name)
		VALUES ('00000000-0000-0000-0000-000000000010', 'demo-org', 'Demo Organization')`); err != nil {
		return fmt.Errorf("seed organisation: %w", err)
	}
	return nil
}

func openTestDB() (*sql.DB, error) {
	dsn, err := testDSN()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	// One session, so a migration cannot wait on a lock held by another
	// connection from this same pool.
	db.SetMaxOpenConns(1)
	return db, nil
}

// backfillDatabase is a second database on the same server. The backfill
// test migrates it only part of the way. It is not the public schema of the
// shared test database, so it cannot lock or destroy that schema.
const backfillDatabase = "tasks_grant_backfill"

func recreateBackfillDatabase(ctx context.Context, admin *sql.DB) error {
	if err := dropBackfillDatabase(ctx, admin); err != nil {
		return err
	}
	stmt, err := identifierStatement(ctx, admin, `CREATE DATABASE %I`, backfillDatabase)
	if err != nil {
		return err
	}
	if _, err := admin.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("create backfill database: %w", err)
	}
	return nil
}

func dropBackfillDatabase(ctx context.Context, admin *sql.DB) error {
	if _, err := admin.ExecContext(ctx, `
		SELECT pg_terminate_backend(pid)
		  FROM pg_stat_activity
		 WHERE datname = $1 AND pid <> pg_backend_pid()`, backfillDatabase); err != nil {
		return fmt.Errorf("disconnect backfill database: %w", err)
	}
	stmt, err := identifierStatement(ctx, admin, `DROP DATABASE IF EXISTS %I`, backfillDatabase)
	if err != nil {
		return err
	}
	if _, err := admin.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("drop backfill database: %w", err)
	}
	return nil
}

// identifierStatement asks Postgres to quote a constant identifier. CREATE
// DATABASE and DROP DATABASE cannot take a bound parameter.
func identifierStatement(ctx context.Context, db *sql.DB, format, ident string) (string, error) {
	var stmt string
	if err := db.QueryRowContext(ctx, `SELECT format($1, $2::text)`, format, ident).Scan(&stmt); err != nil {
		return "", err
	}
	return stmt, nil
}

// openBackfillDB connects to the empty backfill database.
func openBackfillDB() (*sql.DB, error) {
	dsn, err := testDSN()
	if err != nil {
		return nil, err
	}
	dsn, err = withDatabase(dsn, backfillDatabase)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func withDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse TEST_DATABASE_URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("TEST_DATABASE_URL must be a postgres URL")
	}
	u.Path = "/" + name
	return u.String(), nil
}

func openTestPool(ctx context.Context) (*pgxpool.Pool, error) {
	dsn, err := testDSN()
	if err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, `SELECT set_config('lock_timeout', $1, false)`, testLockTimeout); err != nil {
			return err
		}
		_, err := conn.Exec(ctx, `SELECT set_config('idle_in_transaction_session_timeout', $1, false)`, testIdleTxTimeout)
		return err
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}

func testDSN() (string, error) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		return "", fmt.Errorf("TEST_DATABASE_URL is not set")
	}
	var err error
	dsn, err = withRuntimeParam(dsn, "lock_timeout", testLockTimeout)
	if err != nil {
		return "", err
	}
	return withRuntimeParam(dsn, "idle_in_transaction_session_timeout", testIdleTxTimeout)
}

func withRuntimeParam(dsn, key, value string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse TEST_DATABASE_URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("TEST_DATABASE_URL must be a postgres URL")
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
