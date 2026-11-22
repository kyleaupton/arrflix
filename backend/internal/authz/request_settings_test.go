package authz

import (
	"slices"
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
)

func TestRequestSettingsGrants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   model.RequestSettings
		want []string
	}{
		{"both off", model.RequestSettings{}, nil},
		{"4K only", model.RequestSettings{Allow4k: true}, []string{
			"requests.create:movie:4k", "requests.create:series:4k",
		}},
		{"auto-approve only", model.RequestSettings{AutoApprove: true}, []string{
			"requests.auto_approve:movie:hd", "requests.auto_approve:series:hd",
		}},
		{"both on", model.RequestSettings{AutoApprove: true, Allow4k: true}, []string{
			"requests.create:movie:4k", "requests.auto_approve:movie:hd", "requests.auto_approve:movie:4k",
			"requests.create:series:4k", "requests.auto_approve:series:hd", "requests.auto_approve:series:4k",
		}},
	}
	owned := RequestSettingsKeys()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RequestSettingsGrants(tc.in)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("grants = %v, want %v", got, tc.want)
			}
			for _, k := range got {
				if !slices.Contains(owned, k) {
					t.Errorf("derived grant %q is outside RequestSettingsKeys", k)
				}
			}
		})
	}
}

func TestRequestSettingsKeys_AreCatalogKeys(t *testing.T) {
	t.Parallel()
	all := AllKeys()
	keys := RequestSettingsKeys()
	if len(keys) != 6 {
		t.Fatalf("RequestSettingsKeys = %v, want 6 keys", keys)
	}
	for _, k := range keys {
		if !slices.Contains(all, k) {
			t.Errorf("%q is not in the catalog", k)
		}
	}
}
