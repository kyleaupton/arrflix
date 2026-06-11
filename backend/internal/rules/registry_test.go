package rules

import (
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
)

func TestRegistryField(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	info, ok := reg.Field("quality.resolution")
	if !ok {
		t.Fatal(`Field("quality.resolution") not found`)
	}
	if info.Type != "enum" || info.Phase != model.PhaseSearch || len(info.EnumValues) == 0 {
		t.Errorf("quality.resolution info = %+v, want search enum with values", info)
	}

	if _, ok := reg.Field("bogus.path"); ok {
		t.Error(`Field("bogus.path") = ok, want miss`)
	}
}

func TestHasPhase(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	search := leaf(OpEq, fld("quality.resolution"), litEnum("2160p"))
	imp := leaf(OpEq, fld("mediainfo.video_codec"), litEnum("H.265"))
	mixed := branch(OpAnd, search, branch(OpNot, imp))

	tests := []struct {
		name  string
		cond  *Condition
		phase model.Phase
		want  bool
	}{
		{"search tree has search", search, model.PhaseSearch, true},
		{"search tree lacks import", search, model.PhaseImport, false},
		{"import tree has import", imp, model.PhaseImport, true},
		{"import tree lacks search", imp, model.PhaseSearch, false},
		{"mixed tree has search", mixed, model.PhaseSearch, true},
		{"mixed tree has import", mixed, model.PhaseImport, true},
		{"nothing is tagged grab yet", mixed, model.PhaseGrab, false},
		{"nil tree", nil, model.PhaseSearch, false},
		{"unknown path counts as nothing", leaf(OpEq, fld("bogus.path"), litStr("x")), model.PhaseSearch, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := reg.HasPhase(tt.cond, tt.phase); got != tt.want {
				t.Errorf("HasPhase(%s, %s) = %v, want %v", tt.name, tt.phase, got, tt.want)
			}
		})
	}
}
