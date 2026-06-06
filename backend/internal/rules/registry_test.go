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
	if info.Type != "enum" || info.Phase != model.PhasePreDownload || len(info.EnumValues) == 0 {
		t.Errorf("quality.resolution info = %+v, want pre_download enum with values", info)
	}

	if _, ok := reg.Field("bogus.path"); ok {
		t.Error(`Field("bogus.path") = ok, want miss`)
	}
}

func TestHasPhase(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	pre := leaf(OpEq, fld("quality.resolution"), litEnum("2160p"))
	post := leaf(OpEq, fld("mediainfo.video_codec"), litEnum("H.265"))
	mixed := branch(OpAnd, pre, branch(OpNot, post))

	tests := []struct {
		name  string
		cond  *Condition
		phase model.Phase
		want  bool
	}{
		{"pre tree has pre", pre, model.PhasePreDownload, true},
		{"pre tree lacks post", pre, model.PhasePostDownload, false},
		{"post tree has post", post, model.PhasePostDownload, true},
		{"post tree lacks pre", post, model.PhasePreDownload, false},
		{"mixed tree has pre", mixed, model.PhasePreDownload, true},
		{"mixed tree has post", mixed, model.PhasePostDownload, true},
		{"nil tree", nil, model.PhasePreDownload, false},
		{"unknown path counts as nothing", leaf(OpEq, fld("bogus.path"), litStr("x")), model.PhasePreDownload, false},
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
