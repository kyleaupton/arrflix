package repo

import (
	"testing"
)

// TestRolesFromAggregate guards the decode of the json_agg roles column across
// the shapes pgx can hand back. The regression that motivated it: pgx's JSON
// codec decodes a `json` column scanned into interface{} into native Go values
// ([]interface{} of map[string]interface{}), not raw []byte — the old
// raw.([]byte) assertion silently dropped every role.
func TestRolesFromAggregate(t *testing.T) {
	t.Parallel()

	const id = "a60a52e4-7647-493e-8145-d7b76849b592"

	tests := []struct {
		name      string
		raw       any
		wantNames []string
	}{
		{
			name: "pgx native decode (the real driver output)",
			raw: []any{
				map[string]any{"id": id, "name": "admin", "description": nil},
			},
			wantNames: []string{"admin"},
		},
		{
			name:      "raw json bytes",
			raw:       []byte(`[{"id":"` + id + `","name":"co_admin","description":"ops"}]`),
			wantNames: []string{"co_admin"},
		},
		{
			name:      "json string",
			raw:       `[{"id":"` + id + `","name":"user"}]`,
			wantNames: []string{"user"},
		},
		{name: "nil column", raw: nil, wantNames: nil},
		{name: "empty filtered aggregate", raw: []byte(`[]`), wantNames: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := rolesFromAggregate(tt.raw)
			if len(got) != len(tt.wantNames) {
				t.Fatalf("got %d roles, want %d (%v)", len(got), len(tt.wantNames), got)
			}
			for i, name := range tt.wantNames {
				if got[i].Name != name {
					t.Errorf("role[%d].Name = %q, want %q", i, got[i].Name, name)
				}
			}
		})
	}
}
