package authz

import (
	"testing"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
)

// ptr is a tiny helper for the nullable resource-scope fields.
func strPtr(s string) *string       { return &s }
func idPtr(id uuid.UUID) *uuid.UUID { return &id }

func TestResolve(t *testing.T) {
	t.Parallel()

	libA := uuid.New()
	libB := uuid.New()

	// grant builds a model.Grant for a permission key with an optional scope.
	grant := func(key string, effect model.GrantEffect, rt *string, rid *uuid.UUID) model.Grant {
		return model.Grant{
			ID:            uuid.New(),
			PermissionKey: key,
			Effect:        effect,
			ResourceType:  rt,
			ResourceID:    rid,
		}
	}

	tests := []struct {
		name     string
		grants   []model.Grant
		key      string
		resource *Resource
		want     bool
	}{
		{
			name:   "absent key denies",
			grants: []model.Grant{grant(LibraryRead, model.GrantEffectAllow, nil, nil)},
			key:    LibraryWrite,
			want:   false,
		},
		{
			name:   "global allow grants global check",
			grants: []model.Grant{grant(LibraryRead, model.GrantEffectAllow, nil, nil)},
			key:    LibraryRead,
			want:   true,
		},
		{
			name: "deny wins over allow (same scope)",
			grants: []model.Grant{
				grant(LibraryRead, model.GrantEffectAllow, nil, nil),
				grant(LibraryRead, model.GrantEffectDeny, nil, nil),
			},
			key:  LibraryRead,
			want: false,
		},
		{
			name:     "global allow matches a scoped query",
			grants:   []model.Grant{grant(LibraryRead, model.GrantEffectAllow, nil, nil)},
			key:      LibraryRead,
			resource: &Resource{Type: "library", ID: libA},
			want:     true,
		},
		{
			name:     "scoped allow matches its exact resource",
			grants:   []model.Grant{grant(LibraryRead, model.GrantEffectAllow, strPtr("library"), idPtr(libA))},
			key:      LibraryRead,
			resource: &Resource{Type: "library", ID: libA},
			want:     true,
		},
		{
			name:     "scoped allow does NOT match a different resource",
			grants:   []model.Grant{grant(LibraryRead, model.GrantEffectAllow, strPtr("library"), idPtr(libA))},
			key:      LibraryRead,
			resource: &Resource{Type: "library", ID: libB},
			want:     false,
		},
		{
			name:   "scoped allow does NOT satisfy a global check",
			grants: []model.Grant{grant(LibraryRead, model.GrantEffectAllow, strPtr("library"), idPtr(libA))},
			key:    LibraryRead,
			want:   false,
		},
		{
			name: "scoped deny blocks its resource despite global allow",
			grants: []model.Grant{
				grant(LibraryRead, model.GrantEffectAllow, nil, nil),
				grant(LibraryRead, model.GrantEffectDeny, strPtr("library"), idPtr(libA)),
			},
			key:      LibraryRead,
			resource: &Resource{Type: "library", ID: libA},
			want:     false,
		},
		{
			name: "scoped deny does not block a different resource",
			grants: []model.Grant{
				grant(LibraryRead, model.GrantEffectAllow, nil, nil),
				grant(LibraryRead, model.GrantEffectDeny, strPtr("library"), idPtr(libA)),
			},
			key:      LibraryRead,
			resource: &Resource{Type: "library", ID: libB},
			want:     true,
		},
		{
			name: "multiple roles union to grant",
			grants: []model.Grant{
				grant(LibraryRead, model.GrantEffectAllow, nil, nil),
				grant(JobsManage, model.GrantEffectAllow, nil, nil),
			},
			key:  JobsManage,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			set := NewEffectiveSet(tt.grants)
			if got := Resolve(set, tt.key, tt.resource); got != tt.want {
				t.Fatalf("Resolve(%q, %v) = %v, want %v", tt.key, tt.resource, got, tt.want)
			}
		})
	}
}

func TestResolve_NilSetDenies(t *testing.T) {
	t.Parallel()
	if Resolve(nil, LibraryRead, nil) {
		t.Fatal("Resolve(nil, ...) = true, want false")
	}
}
