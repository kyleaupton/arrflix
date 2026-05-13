// Package dbtest provides a Postgres testcontainer with template-DB-clone
// isolation for integration tests. Each dbtest.New(t) returns a fresh
// database cloned from a migrated template.
//
// Typical usage from TestMain:
//
//	func TestMain(m *testing.M) {
//	    ctx := context.Background()
//	    if err := dbtest.Start(ctx); err != nil {
//	        panic(err)
//	    }
//	    code := m.Run()
//	    _ = dbtest.Stop(ctx)
//	    os.Exit(code)
//	}
package dbtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kyleaupton/arrflix/internal/db"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	templateDBName = "arrflix_template"

	// adminDBName is the bootstrap database used to run CREATE/DROP DATABASE
	// against — must differ from the template (you can't drop or use a
	// template DB while connected to it).
	adminDBName = "postgres"
)

type harness struct {
	container   *tcpostgres.PostgresContainer
	adminDSN    string
	templateDSN string
	counter     atomic.Uint64
}

var (
	h   *harness
	hMu sync.Mutex
)

// Start spins up a postgres:16-alpine container, creates a template database
// called arrflix_template, and runs all embedded migrations against it. It is
// safe to call from TestMain; subsequent calls without a corresponding Stop
// are a no-op error.
func Start(ctx context.Context) error {
	hMu.Lock()
	defer hMu.Unlock()
	if h != nil {
		return fmt.Errorf("dbtest: already started")
	}

	ctr, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase(adminDBName),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return fmt.Errorf("dbtest: start container: %w", err)
	}

	adminDSN, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = ctr.Terminate(ctx)
		return fmt.Errorf("dbtest: connection string: %w", err)
	}

	createStmt := fmt.Sprintf("CREATE DATABASE %s", quoteIdent(templateDBName))
	if err := execAdmin(ctx, adminDSN, createStmt); err != nil {
		_ = ctr.Terminate(ctx)
		return fmt.Errorf("dbtest: create template db: %w", err)
	}

	templateDSN, err := withDatabaseName(adminDSN, templateDBName)
	if err != nil {
		_ = ctr.Terminate(ctx)
		return fmt.Errorf("dbtest: derive template dsn: %w", err)
	}

	if err := db.ApplyMigrations(templateDSN); err != nil {
		_ = ctr.Terminate(ctx)
		return fmt.Errorf("dbtest: apply migrations to template: %w", err)
	}

	h = &harness{
		container:   ctr,
		adminDSN:    adminDSN,
		templateDSN: templateDSN,
	}
	return nil
}

// Stop terminates the postgres container started by Start. It is safe to call
// even if Start failed partway through (it will simply be a no-op).
func Stop(ctx context.Context) error {
	hMu.Lock()
	defer hMu.Unlock()
	if h == nil {
		return nil
	}
	err := h.container.Terminate(ctx)
	h = nil
	return err
}

// New creates a fresh per-test database by cloning the migrated template
// and returns a connected pgxpool.Pool. Pool close + database drop are
// registered via t.Cleanup.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	hMu.Lock()
	current := h
	hMu.Unlock()
	if current == nil {
		t.Fatal("dbtest.Start was not called from TestMain")
	}

	ctx := t.Context()

	dbName := fmt.Sprintf("test_%d_%d", os.Getpid(), current.counter.Add(1))
	createStmt := fmt.Sprintf(
		"CREATE DATABASE %s TEMPLATE %s",
		quoteIdent(dbName), quoteIdent(templateDBName),
	)
	if err := execAdmin(ctx, current.adminDSN, createStmt); err != nil {
		t.Fatalf("dbtest: create per-test db: %v", err)
	}

	dsn, err := withDatabaseName(current.adminDSN, dbName)
	if err != nil {
		t.Fatalf("dbtest: derive per-test dsn: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		_ = execAdmin(ctx, current.adminDSN,
			fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdent(dbName)))
		t.Fatalf("dbtest: connect per-test db: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		// Use a fresh background context: t.Context() is already cancelled by
		// the time cleanup runs.
		dropCtx := context.Background()
		_ = execAdmin(dropCtx, current.adminDSN,
			fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdent(dbName)))
	})

	return pool
}

// execAdmin opens a one-shot pool against dsn, executes stmt, and closes the
// pool. CREATE/DROP DATABASE cannot run inside a transaction, so we use a
// short-lived connection rather than reusing a shared pool.
func execAdmin(ctx context.Context, dsn, stmt string) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("admin pool: %w", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("exec %q: %w", stmt, err)
	}
	return nil
}

// withDatabaseName returns a copy of dsn with its database (URL path)
// component replaced by name.
func withDatabaseName(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

// quoteIdent wraps a SQL identifier in double quotes and escapes any embedded
// double quotes. The test-database names we generate are constrained to safe
// characters, but quoting keeps us defensible.
func quoteIdent(name string) string {
	escaped := ""
	for _, r := range name {
		if r == '"' {
			escaped += `""`
			continue
		}
		escaped += string(r)
	}
	return `"` + escaped + `"`
}
