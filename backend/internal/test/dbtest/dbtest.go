// Package dbtest provides a Postgres server with template-DB-clone isolation
// for integration tests. Each dbtest.New(t) returns a fresh database cloned
// from a migrated template.
//
// By default the server is a postgres:16-alpine testcontainer, which needs a
// docker daemon. When ARRFLIX_TEST_PG_DSN is set, the tests use that existing
// server instead and need no docker. The DSN must name a role allowed to
// create databases (typically the superuser) and a database other than the
// ones the tests create, such as "postgres".
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

// externalDSNEnv names the env var that points the tests at an existing
// Postgres server instead of a testcontainer.
const externalDSNEnv = "ARRFLIX_TEST_PG_DSN"

const (
	// adminDBName is the bootstrap database used to run CREATE/DROP DATABASE
	// against — must differ from the template (you can't drop or use a
	// template DB while connected to it).
	adminDBName = "postgres"
)

type harness struct {
	// container is nil when the tests run against an external server.
	container *tcpostgres.PostgresContainer
	adminDSN  string
	// templateName is unique per test process: `go test` runs packages as
	// parallel processes, and on a shared external server a fixed name would
	// collide.
	templateName string
	templateDSN  string
	counter      atomic.Uint64
}

var (
	h   *harness
	hMu sync.Mutex
)

// Start connects to the Postgres server (starting a testcontainer unless
// ARRFLIX_TEST_PG_DSN is set), creates this process's template database, and
// runs all embedded migrations against it. It is safe to call from TestMain;
// subsequent calls without a corresponding Stop are a no-op error.
func Start(ctx context.Context) error {
	hMu.Lock()
	defer hMu.Unlock()
	if h != nil {
		return fmt.Errorf("dbtest: already started")
	}

	var ctr *tcpostgres.PostgresContainer
	adminDSN := os.Getenv(externalDSNEnv)
	if adminDSN == "" {
		var err error
		ctr, err = tcpostgres.Run(ctx,
			"postgres:16-alpine",
			tcpostgres.WithDatabase(adminDBName),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),
			tcpostgres.BasicWaitStrategies(),
		)
		if err != nil {
			return fmt.Errorf("dbtest: start container: %w", err)
		}
		adminDSN, err = ctr.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			_ = ctr.Terminate(ctx)
			return fmt.Errorf("dbtest: connection string: %w", err)
		}
	}

	hh := &harness{
		container:    ctr,
		adminDSN:     adminDSN,
		templateName: fmt.Sprintf("arrflix_template_%d", os.Getpid()),
	}
	if err := hh.createTemplate(ctx); err != nil {
		_ = hh.teardown(ctx)
		return err
	}
	h = hh
	return nil
}

// createTemplate creates and migrates the template database. A template left
// behind by an earlier crashed process with the same pid is dropped first.
func (hh *harness) createTemplate(ctx context.Context) error {
	if err := execAdmin(ctx, hh.adminDSN, dropStmt(hh.templateName)); err != nil {
		return fmt.Errorf("dbtest: drop stale template db: %w", err)
	}
	createStmt := fmt.Sprintf("CREATE DATABASE %s", quoteIdent(hh.templateName))
	if err := execAdmin(ctx, hh.adminDSN, createStmt); err != nil {
		return fmt.Errorf("dbtest: create template db: %w", err)
	}

	templateDSN, err := withDatabaseName(hh.adminDSN, hh.templateName)
	if err != nil {
		return fmt.Errorf("dbtest: derive template dsn: %w", err)
	}
	if err := db.ApplyMigrations(templateDSN); err != nil {
		return fmt.Errorf("dbtest: apply migrations to template: %w", err)
	}
	hh.templateDSN = templateDSN
	return nil
}

// teardown terminates the testcontainer, or on an external server drops the
// template database, which would otherwise outlive the process.
func (hh *harness) teardown(ctx context.Context) error {
	if hh.container != nil {
		return hh.container.Terminate(ctx)
	}
	return execAdmin(ctx, hh.adminDSN, dropStmt(hh.templateName))
}

// Stop releases what Start created. It is safe to call even if Start failed
// partway through (it will simply be a no-op).
func Stop(ctx context.Context) error {
	hMu.Lock()
	defer hMu.Unlock()
	if h == nil {
		return nil
	}
	err := h.teardown(ctx)
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
		quoteIdent(dbName), quoteIdent(current.templateName),
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
		_ = execAdmin(ctx, current.adminDSN, dropStmt(dbName))
		t.Fatalf("dbtest: connect per-test db: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		// Use a fresh background context: t.Context() is already cancelled by
		// the time cleanup runs.
		dropCtx := context.Background()
		_ = execAdmin(dropCtx, current.adminDSN, dropStmt(dbName))
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

func dropStmt(name string) string {
	return fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdent(name))
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
