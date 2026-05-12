//go:build integration

package integration

import (
	"context"
	"os"
	"testing"

	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// TestMain boots the shared Postgres testcontainer once for the whole
// `integration`-tagged test binary, then runs all tests, then tears it down.
func TestMain(m *testing.M) {
	ctx := context.Background()
	if err := dbtest.Start(ctx); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = dbtest.Stop(ctx)
	os.Exit(code)
}
