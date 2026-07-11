package authz

import (
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
)

func TestAllKeys_CountAndUniqueness(t *testing.T) {
	t.Parallel()

	keys := AllKeys()
	if len(keys) != 33 {
		t.Fatalf("AllKeys() returned %d keys, want 33", len(keys))
	}

	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if seen[k] {
			t.Fatalf("AllKeys() contains duplicate key %q", k)
		}
		seen[k] = true
	}
}

func TestBuilders_Format(t *testing.T) {
	t.Parallel()

	cases := []struct {
		got  string
		want string
	}{
		// Lowercasing the tier is the load-bearing behavior here.
		{RequestCreate(model.MediaTypeMovie, "HD"), "requests.create:movie:hd"},
		{RequestCreate(model.MediaTypeSeries, "4K"), "requests.create:series:4k"},
		{RequestAutoApprove(model.MediaTypeMovie, "4K"), "requests.auto_approve:movie:4k"},
		{RequestApprove(model.MediaTypeSeries), "requests.approve:series"},
		{RequestDeny(model.MediaTypeMovie), "requests.deny:movie"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("builder = %q, want %q", c.got, c.want)
		}
	}
}
