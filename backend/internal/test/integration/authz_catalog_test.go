//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/kyleaupton/arrflix/internal/authz"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// TestAuthz_CatalogDrift locks the seeded permission table to the code registry
// (authz.AllKeys) in both directions: a key seeded with no code builder, or a
// code key never seeded, fails here. This is the D1 forcing function — adding a
// tier to qualityprofile.AllTiers grows AllKeys and breaks this test until a
// migration seeds the matching permission rows.
func TestAuthz_CatalogDrift(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx, "SELECT key FROM permission")
	if err != nil {
		t.Fatalf("query permission: %v", err)
	}
	defer rows.Close()

	dbKeys := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("scan key: %v", err)
		}
		dbKeys[key] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate keys: %v", err)
	}

	codeKeys := make(map[string]bool, len(authz.AllKeys()))
	for _, k := range authz.AllKeys() {
		codeKeys[k] = true
	}

	if len(dbKeys) != len(codeKeys) {
		t.Errorf("catalog size mismatch: DB has %d keys, code has %d", len(dbKeys), len(codeKeys))
	}
	for k := range codeKeys {
		if !dbKeys[k] {
			t.Errorf("code key %q is not seeded in the permission table", k)
		}
	}
	for k := range dbKeys {
		if !codeKeys[k] {
			t.Errorf("seeded key %q has no code builder in authz.AllKeys()", k)
		}
	}
}
