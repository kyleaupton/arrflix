package rules

import (
	"testing"
)

func TestEvalComparisons(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	rel := postRelease()

	tests := []struct {
		name string
		cond *Condition
		want Tri
	}{
		// Enum equality / membership.
		{"enum eq true", leaf(OpEq, fld("quality.resolution"), litEnum("2160p")), True},
		{"enum eq false", leaf(OpEq, fld("quality.resolution"), litEnum("1080p")), False},
		{"enum ne true", leaf(OpNe, fld("quality.resolution"), litEnum("1080p")), True},
		{"enum ne false", leaf(OpNe, fld("quality.resolution"), litEnum("2160p")), False},
		{"enum in true", leaf(OpIn, fld("quality.resolution"), litList("1080p", "2160p")), True},
		{"enum in false", leaf(OpIn, fld("quality.resolution"), litList("720p", "1080p")), False},
		{"enum not in true", leaf(OpNotIn, fld("quality.resolution"), litList("720p", "1080p")), True},
		{"enum not in false", leaf(OpNotIn, fld("quality.resolution"), litList("1080p", "2160p")), False},

		// Numbers: int64, int, float64 fields; ordering and equality.
		{"number gt true", leaf(OpGt, fld("candidate.seeders"), litInt(10)), True},
		{"number gt false", leaf(OpGt, fld("candidate.seeders"), litInt(50)), False},
		{"number gte boundary", leaf(OpGte, fld("candidate.seeders"), litInt(50)), True},
		{"number lt false", leaf(OpLt, fld("candidate.seeders"), litInt(50)), False},
		{"number lte boundary", leaf(OpLte, fld("candidate.seeders"), litInt(50)), True},
		{"int64 eq", leaf(OpEq, fld("candidate.size"), litInt(4_000_000_000)), True},
		{"float eq", leaf(OpEq, fld("candidate.age_hours"), litFloat(5.5)), True},
		{"int vs float ordering", leaf(OpGt, fld("candidate.age_hours"), litInt(5)), True},
		{"wrapped int eq", leaf(OpEq, fld("quality.version"), litInt(2)), True},

		// Bools.
		{"bool eq true", leaf(OpEq, fld("quality.is_repack"), litBool(true)), True},
		{"bool eq false", leaf(OpEq, fld("quality.is_repack"), litBool(false)), False},

		// Strings: substring and membership.
		{"string contains true", leaf(OpContains, fld("candidate.title"), litStr("BluRay")), True},
		{"string contains false", leaf(OpContains, fld("candidate.title"), litStr("WEB-DL")), False},
		{"string in true", leaf(OpIn, fld("encode.release_group"), litList("FLUX", "FraMeSToR")), True},
		{"string in false", leaf(OpIn, fld("encode.release_group"), litList("FLUX")), False},
		{"string not in true", leaf(OpNotIn, fld("encode.release_group"), litList("FLUX")), True},

		// []string fields: membership and set equality.
		{"list contains true", leaf(OpContains, fld("mediainfo.audio_languages"), litStr("jpn")), True},
		{"list contains false", leaf(OpContains, fld("mediainfo.audio_languages"), litStr("fra")), False},
		{"list eq order-independent", leaf(OpEq, fld("candidate.categories"), litList("Movies/UHD", "Movies")), True},
		{"list eq different sets", leaf(OpEq, fld("candidate.categories"), litList("Movies")), False},
		{"list ne", leaf(OpNe, fld("candidate.categories"), litList("Movies")), True},

		// Cross-field.
		{"cross-field eq", leaf(OpEq, fld("media.title"), fld("media.clean_title")), True},
		{"cross-field contains", leaf(OpContains, fld("candidate.title"), fld("media.title")), True},

		// Defensive: combinations validation rejects, reaching eval anyway.
		{"cross-type eq is false", leaf(OpEq, fld("media.title"), litInt(5)), False},
		{"cross-type ne is true", leaf(OpNe, fld("media.title"), litInt(5)), True},
		{"ordering on string", leaf(OpGt, fld("media.title"), litStr("A")), Unknown},
		{"contains non-string right", leaf(OpContains, fld("candidate.title"), litInt(5)), Unknown},
		{"in with non-string left", leaf(OpIn, fld("candidate.seeders"), litList("50")), Unknown},
		{"in without list right", leaf(OpIn, fld("encode.release_group"), litStr("FLUX")), Unknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := reg.Eval(tt.cond, rel)
			if got.Result != tt.want {
				t.Errorf("Eval(%s) = %v, want %v", tt.name, got.Result, tt.want)
			}
		})
	}
}

func TestEvalLogicalComposition(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	rel := preRelease()

	// Definite leaves against the fixture.
	yes := leaf(OpEq, fld("media.type"), litEnum("movie")) // True
	no := leaf(OpEq, fld("media.type"), litEnum("series")) // False

	tests := []struct {
		name string
		cond *Condition
		want Tri
	}{
		{"and all true", branch(OpAnd, yes, yes), True},
		{"and one false", branch(OpAnd, yes, no), False},
		{"or one true", branch(OpOr, no, yes), True},
		{"or all false", branch(OpOr, no, no), False},
		{"not true", branch(OpNot, yes), False},
		{"not false", branch(OpNot, no), True},
		{"nested", branch(OpAnd, yes, branch(OpOr, no, branch(OpNot, no))), True},
		{"deep nesting false", branch(OpOr, no, branch(OpAnd, yes, branch(OpNot, yes))), False},

		// Arity edges (defensive at eval; validation rejects these).
		{"and single child", branch(OpAnd, yes), True},
		{"empty and", branch(OpAnd), Unknown},
		{"not wrong arity", branch(OpNot, yes, no), Unknown},
		{"unknown operator", &Condition{Op: "matches"}, Unknown},
		{"nil condition", nil, Unknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := reg.Eval(tt.cond, rel)
			if got.Result != tt.want {
				t.Errorf("Eval(%s) = %v, want %v", tt.name, got.Result, tt.want)
			}
		})
	}
}

func TestEvalThreeValued(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	pre := preRelease()
	post := postRelease()

	// A post_download leaf: Unknown pre-download, definite post-download.
	hevc := leaf(OpEq, fld("mediainfo.video_codec"), litEnum("H.265"))
	yes := leaf(OpEq, fld("media.type"), litEnum("movie")) // True
	no := leaf(OpEq, fld("media.type"), litEnum("series")) // False

	t.Run("post_download leaf pre-download is Unknown", func(t *testing.T) {
		t.Parallel()
		if got := reg.Eval(hevc, pre).Result; got != Unknown {
			t.Errorf("Result = %v, want Unknown", got)
		}
	})
	t.Run("same leaf post-download is definite", func(t *testing.T) {
		t.Parallel()
		if got := reg.Eval(hevc, post).Result; got != True {
			t.Errorf("Result = %v, want True", got)
		}
	})
	t.Run("ordering on unavailable field is Unknown", func(t *testing.T) {
		t.Parallel()
		cond := leaf(OpGte, fld("mediainfo.video_bit_depth"), litInt(10))
		if got := reg.Eval(cond, pre).Result; got != Unknown {
			t.Errorf("Result = %v, want Unknown", got)
		}
	})
	t.Run("unknown path is Unknown", func(t *testing.T) {
		t.Parallel()
		cond := leaf(OpEq, fld("bogus.path"), litStr("x"))
		if got := reg.Eval(cond, pre).Result; got != Unknown {
			t.Errorf("Result = %v, want Unknown", got)
		}
	})
	t.Run("nil optional field is Unknown", func(t *testing.T) {
		t.Parallel()
		// media.season is an unset *int on a movie release.
		cond := leaf(OpEq, fld("media.season"), litInt(1))
		if got := reg.Eval(cond, pre).Result; got != Unknown {
			t.Errorf("Result = %v, want Unknown", got)
		}
	})

	// Kleene propagation.
	propagation := []struct {
		name string
		cond *Condition
		want Tri
	}{
		{"and: False dominates Unknown", branch(OpAnd, no, hevc), False},
		{"and: Unknown taints True", branch(OpAnd, yes, hevc), Unknown},
		{"or: True dominates Unknown", branch(OpOr, yes, hevc), True},
		{"or: Unknown taints False", branch(OpOr, no, hevc), Unknown},
		{"not: Unknown stays Unknown", branch(OpNot, hevc), Unknown},
	}
	for _, tt := range propagation {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := reg.Eval(tt.cond, pre).Result; got != tt.want {
				t.Errorf("Result = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvalTrace(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	cond := branch(OpAnd,
		leaf(OpEq, fld("quality.resolution"), litEnum("2160p")),
		leaf(OpEq, fld("mediainfo.hdr"), litEnum("HDR10")),
	)
	out := reg.Eval(cond, preRelease())

	if out.Result != Unknown {
		t.Fatalf("Result = %v, want Unknown", out.Result)
	}
	tr := out.Trace
	if tr.Result != out.Result {
		t.Errorf("Trace.Result = %v, want %v (must mirror Outcome.Result)", tr.Result, out.Result)
	}
	if tr.Op != OpAnd || len(tr.Children) != 2 {
		t.Fatalf("trace root = %s with %d children, want and with 2", tr.Op, len(tr.Children))
	}

	// First leaf: available pre-download field, definite result.
	first := tr.Children[0]
	if first.Op != OpEq || first.Result != True {
		t.Errorf("children[0] = %s/%v, want ==/True", first.Op, first.Result)
	}
	if first.Left == nil || first.Left.Path != "quality.resolution" || !first.Left.Available || first.Left.Value != "2160p" {
		t.Errorf("children[0].Left = %+v, want available quality.resolution = 2160p", first.Left)
	}
	if first.Right == nil || !first.Right.Available || first.Right.Value != "2160p" || first.Right.Path != "" {
		t.Errorf("children[0].Right = %+v, want available literal 2160p with no path", first.Right)
	}

	// Second leaf: post_download field pre-download — unavailable, Unknown.
	second := tr.Children[1]
	if second.Result != Unknown {
		t.Errorf("children[1].Result = %v, want Unknown", second.Result)
	}
	if second.Left == nil || second.Left.Path != "mediainfo.hdr" || second.Left.Available || second.Left.Value != nil {
		t.Errorf("children[1].Left = %+v, want unavailable mediainfo.hdr with nil value", second.Left)
	}
	if second.Right == nil || !second.Right.Available {
		t.Errorf("children[1].Right = %+v, want available literal", second.Right)
	}

	// Post-download, the same tree is definite and the trace records it.
	out = reg.Eval(cond, postRelease())
	if out.Result != True {
		t.Fatalf("post-download Result = %v, want True", out.Result)
	}
	second = out.Trace.Children[1]
	if !second.Left.Available || second.Left.Value != "HDR10" || second.Result != True {
		t.Errorf("post-download children[1].Left = %+v (Result %v), want available HDR10/True", second.Left, second.Result)
	}
}

func TestTriString(t *testing.T) {
	t.Parallel()
	for tri, want := range map[Tri]string{True: "true", False: "false", Unknown: "unknown"} {
		if got := tri.String(); got != want {
			t.Errorf("Tri(%d).String() = %q, want %q", tri, got, want)
		}
	}
}
