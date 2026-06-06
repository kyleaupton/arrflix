package rules

import (
	"encoding/json"
	"reflect"
	"testing"
)

// canonicalJSON is the storage/wire contract for the condition tree — the
// canonical example from the phase spec. Phase 3 persists exactly these bytes.
const canonicalJSON = `{
  "op": "and",
  "children": [
    { "op": "==", "left": {"kind":"field","path":"quality.resolution"},
                  "right": {"kind":"literal","literal":{"type":"enum","str":"2160p"}} },
    { "op": "in", "left": {"kind":"field","path":"encode.release_group"},
                  "right": {"kind":"literal","literal":{"type":"list","list":["FLUX","FraMeSToR"]}} }
  ]
}`

func canonicalTree() *Condition {
	return branch(OpAnd,
		leaf(OpEq, fld("quality.resolution"), litEnum("2160p")),
		leaf(OpIn, fld("encode.release_group"), litList("FLUX", "FraMeSToR")),
	)
}

func TestConditionJSONCanonicalShape(t *testing.T) {
	t.Parallel()

	// The canonical bytes decode to the expected tree…
	var decoded Condition
	if err := json.Unmarshal([]byte(canonicalJSON), &decoded); err != nil {
		t.Fatalf("Unmarshal(canonical) = %v", err)
	}
	if want := canonicalTree(); !reflect.DeepEqual(&decoded, want) {
		t.Errorf("decoded tree = %+v, want %+v", &decoded, want)
	}

	// …and the tree encodes back to the same JSON shape.
	encoded, err := json.Marshal(canonicalTree())
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	var got, want any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("Unmarshal(encoded) = %v", err)
	}
	if err := json.Unmarshal([]byte(canonicalJSON), &want); err != nil {
		t.Fatalf("Unmarshal(canonical) = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("encoded shape = %s, want canonical %s", encoded, canonicalJSON)
	}

	// The canonical tree is also valid.
	if err := NewRegistry().Validate(canonicalTree()); err != nil {
		t.Errorf("Validate(canonical) = %v, want nil", err)
	}
}

func TestConditionJSONRoundTripAllLiteralTypes(t *testing.T) {
	t.Parallel()

	// A nested tree exercising every literal type and a list literal.
	tree := branch(OpAnd,
		leaf(OpEq, fld("media.title"), litStr("Dune")),
		leaf(OpGt, fld("candidate.size"), litInt(4_000_000_000)),
		leaf(OpLt, fld("candidate.age_hours"), litFloat(1.5)),
		leaf(OpEq, fld("quality.is_repack"), litBool(true)),
		branch(OpNot, leaf(OpNe, fld("quality.resolution"), litEnum("2160p"))),
		branch(OpOr,
			leaf(OpNotIn, fld("encode.release_group"), litList("BAD-GROUP")),
			leaf(OpContains, fld("candidate.title"), fld("media.title")),
		),
	)

	encoded, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	var decoded Condition
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal = %v", err)
	}
	if !reflect.DeepEqual(&decoded, tree) {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", &decoded, tree)
	}
}

func TestTriJSONRoundTrip(t *testing.T) {
	t.Parallel()

	for tri, want := range map[Tri]string{True: `"true"`, False: `"false"`, Unknown: `"unknown"`} {
		b, err := json.Marshal(tri)
		if err != nil {
			t.Fatalf("Marshal(%v) = %v", tri, err)
		}
		if string(b) != want {
			t.Errorf("Marshal(%v) = %s, want %s", tri, b, want)
		}
		var back Tri
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("Unmarshal(%s) = %v", b, err)
		}
		if back != tri {
			t.Errorf("round-trip %v → %v", tri, back)
		}
	}

	var invalid Tri
	if err := json.Unmarshal([]byte(`"maybe"`), &invalid); err == nil {
		t.Error("Unmarshal(invalid Tri) = nil, want error")
	}
}
