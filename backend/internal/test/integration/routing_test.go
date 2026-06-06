//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/routing"
	"github.com/kyleaupton/arrflix/internal/rules"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// resolutionCondition is a canonical one-leaf tree: quality.resolution == res.
func resolutionCondition(res string) map[string]any {
	return map[string]any{
		"op":    "==",
		"left":  map[string]any{"kind": "field", "path": "quality.resolution"},
		"right": map[string]any{"kind": "literal", "literal": map[string]any{"type": "enum", "str": res}},
	}
}

// makeRoutingRuleBody builds a valid create/update body. Conditions ride as
// the canonical tree; action ids are optional.
func makeRoutingRuleBody(name string, conditions map[string]any) map[string]any {
	return map[string]any{
		"name":       name,
		"enabled":    true,
		"continue":   false,
		"conditions": conditions,
	}
}

// seedDefaults creates a default downloader, library, and name template
// through the public API so dispatch fallbacks resolve.
func seedDefaults(t *testing.T, app *testapp.App) (downloader model.Downloader, library model.Library, template model.NameTemplate) {
	t.Helper()

	app.POST(t, "/api/v1/downloaders", map[string]any{
		"name":     "Default qBit",
		"type":     "qbittorrent",
		"protocol": "torrent",
		"url":      "http://localhost:9999",
		"enabled":  true,
		"default":  true,
	}, &downloader, http.StatusCreated)

	app.POST(t, "/api/v1/libraries", map[string]any{
		"name":     "Movies",
		"type":     "movie",
		"rootPath": t.TempDir(),
		"enabled":  true,
		"default":  true,
	}, &library, http.StatusCreated)

	app.POST(t, "/api/v1/name-templates", map[string]any{
		"name":             "Default Movie",
		"type":             "movie",
		"template":         "{{.Media.Title}} ({{.Media.Year}})",
		"movieDirTemplate": "{{.Media.Title}} ({{.Media.Year}})",
		"default":          true,
	}, &template, http.StatusCreated)

	return downloader, library, template
}

// TestRouting_CreateRoundTrip asserts the conditions JSONB column stores and
// returns the canonical tree losslessly — the wire body, the stored bytes,
// and the in-memory tree are one encoding.
func TestRouting_CreateRoundTrip(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	tree := map[string]any{
		"op": "and",
		"children": []any{
			resolutionCondition("2160p"),
			map[string]any{
				"op":    ">",
				"left":  map[string]any{"kind": "field", "path": "candidate.seeders"},
				"right": map[string]any{"kind": "literal", "literal": map[string]any{"type": "int", "int": 10}},
			},
		},
	}

	var created model.RoutingRule
	app.POST(t, "/api/v1/routing/rules", makeRoutingRuleBody("4K rule", tree), &created, http.StatusCreated)

	if created.ID == uuid.Nil {
		t.Fatalf("expected non-nil id, got: %+v", created)
	}
	if created.Position != 0 {
		t.Errorf("Position = %d, want 0 (first rule appends at the start)", created.Position)
	}

	var fetched model.RoutingRule
	app.GET(t, "/api/v1/routing/rules/"+created.ID.String(), &fetched, http.StatusOK)

	// Decode the stored conditions into the substrate type — the round trip
	// must produce the exact tree that was sent.
	var cond rules.Condition
	if err := json.Unmarshal(fetched.Conditions, &cond); err != nil {
		t.Fatalf("stored conditions don't decode: %v", err)
	}
	if cond.Op != rules.OpAnd || len(cond.Children) != 2 {
		t.Fatalf("decoded tree = %+v, want and with 2 children", cond)
	}
	leaf := cond.Children[0]
	if leaf.Op != rules.OpEq || leaf.Left.Path != "quality.resolution" || leaf.Right.Literal.Str != "2160p" {
		t.Errorf("children[0] = %+v, want quality.resolution == 2160p", leaf)
	}
	if got := cond.Children[1].Right.Literal.Int; got != 10 {
		t.Errorf("children[1] int literal = %d, want 10 (lossless int round-trip)", got)
	}
}

// TestRouting_CreateValidation_IllTypedTree asserts authoring-time
// type-checking rejects an invalid tree as a 422 with per-field problems.
func TestRouting_CreateValidation_IllTypedTree(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	// Ordering on an enum field — unbuildable by design.
	tree := map[string]any{
		"op":    ">",
		"left":  map[string]any{"kind": "field", "path": "quality.resolution"},
		"right": map[string]any{"kind": "literal", "literal": map[string]any{"type": "enum", "str": "1080p"}},
	}

	var pd apperrors.ProblemDetails
	app.POST(t, "/api/v1/routing/rules", makeRoutingRuleBody("bad", tree), &pd, http.StatusUnprocessableEntity)

	fe := findFieldError(t, pd, "body.conditions")
	if fe.Message == "" {
		t.Errorf("expected a validation message for body.conditions, got %+v", pd)
	}
}

// TestRouting_CreateValidation_UnresolvedTarget asserts referenced action
// targets must exist at save time.
func TestRouting_CreateValidation_UnresolvedTarget(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	body := makeRoutingRuleBody("dangling", resolutionCondition("1080p"))
	body["downloaderId"] = uuid.NewString()

	var pd apperrors.ProblemDetails
	app.POST(t, "/api/v1/routing/rules", body, &pd, http.StatusUnprocessableEntity)
	findFieldError(t, pd, "body.downloaderId")
}

// TestRouting_Reorder asserts the positions endpoint reassigns the global
// order from the full id list.
func TestRouting_Reorder(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	var first, second model.RoutingRule
	app.POST(t, "/api/v1/routing/rules", makeRoutingRuleBody("first", resolutionCondition("2160p")), &first, http.StatusCreated)
	app.POST(t, "/api/v1/routing/rules", makeRoutingRuleBody("second", resolutionCondition("1080p")), &second, http.StatusCreated)

	var reordered []model.RoutingRule
	app.PUT(t, "/api/v1/routing/rules/positions", map[string]any{
		"ids": []string{second.ID.String(), first.ID.String()},
	}, &reordered, http.StatusOK)

	if len(reordered) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(reordered))
	}
	if reordered[0].ID != second.ID || reordered[1].ID != first.ID {
		t.Errorf("order = [%s %s], want [second first]", reordered[0].Name, reordered[1].Name)
	}

	// A partial id list is rejected — reordering is list-level.
	var pd apperrors.ProblemDetails
	app.PUT(t, "/api/v1/routing/rules/positions", map[string]any{
		"ids": []string{first.ID.String()},
	}, &pd, http.StatusUnprocessableEntity)
}

// TestRouting_EvaluateDispatch asserts the dry-run endpoint dispatches at
// grab: a matching rule fires its action set; non-matching candidates fall
// back to the defaults; an import-phase condition is indeterminate (not a
// match) at grab.
func TestRouting_EvaluateDispatch(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	defaultDownloader, defaultLibrary, _ := seedDefaults(t, app)

	// A second, non-default library the rule routes 4K movies into.
	var uhdLibrary model.Library
	app.POST(t, "/api/v1/libraries", map[string]any{
		"name":     "Movies 4K",
		"type":     "movie",
		"rootPath": t.TempDir(),
		"enabled":  true,
		"default":  false,
	}, &uhdLibrary, http.StatusCreated)

	ruleBody := makeRoutingRuleBody("4K to UHD library", resolutionCondition("2160p"))
	ruleBody["libraryId"] = uhdLibrary.ID.String()
	var rule model.RoutingRule
	app.POST(t, "/api/v1/routing/rules", ruleBody, &rule, http.StatusCreated)

	// An import-phase rule: indeterminate at grab, must not dispatch.
	importTree := map[string]any{
		"op":    "==",
		"left":  map[string]any{"kind": "field", "path": "mediainfo.video_codec"},
		"right": map[string]any{"kind": "literal", "literal": map[string]any{"type": "enum", "str": "H.265"}},
	}
	var importRule model.RoutingRule
	app.POST(t, "/api/v1/routing/rules", makeRoutingRuleBody("import-phase", importTree), &importRule, http.StatusCreated)

	// Put the import-phase rule first so it's evaluated (not skipped behind a
	// terminal match) — its Unknown must pass dispatch to the 4K rule.
	var reordered []model.RoutingRule
	app.PUT(t, "/api/v1/routing/rules/positions", map[string]any{
		"ids": []string{importRule.ID.String(), rule.ID.String()},
	}, &reordered, http.StatusOK)

	evaluate := func(title string) routing.Evaluation {
		var eval routing.Evaluation
		app.POST(t, "/api/v1/routing/evaluate", map[string]any{
			"mediaType": "movie",
			"candidate": map[string]any{
				"title":        title,
				"filename":     "",
				"link":         "",
				"guid":         "test-guid",
				"indexer":      "Test Indexer",
				"indexerId":    1,
				"indexerFlags": []string{},
				"categories":   []string{"Movies"},
				"protocol":     "torrent",
				"publishDate":  "2024-01-01T00:00:00Z",
				"size":         1,
				"seeders":      1,
				"peers":        0,
				"age":          0,
				"ageHours":     0,
				"grabs":        0,
			},
		}, &eval, http.StatusOK)
		return eval
	}

	// 4K release: the rule fires, library comes from the rule, the other
	// slots fall back to defaults.
	eval := evaluate("Dune.2021.2160p.BluRay.REMUX-FraMeSToR")
	if len(eval.Fired) != 1 || eval.Fired[0] != rule.ID {
		t.Fatalf("Fired = %v, want [%s]", eval.Fired, rule.ID)
	}
	if eval.Final.LibraryID == nil || *eval.Final.LibraryID != uhdLibrary.ID {
		t.Errorf("Final.LibraryID = %v, want UHD library %s", eval.Final.LibraryID, uhdLibrary.ID)
	}
	if eval.FellBack.Library {
		t.Errorf("FellBack.Library = true, want false (rule set it)")
	}
	if eval.Final.DownloaderID == nil || *eval.Final.DownloaderID != defaultDownloader.ID {
		t.Errorf("Final.DownloaderID = %v, want default %s", eval.Final.DownloaderID, defaultDownloader.ID)
	}
	if !eval.FellBack.Downloader || !eval.FellBack.NameTemplate {
		t.Errorf("FellBack = %+v, want downloader+nameTemplate fallbacks", eval.FellBack)
	}
	// The import-phase rule runs first and is indeterminate at grab —
	// recorded with its tri-state, not fired.
	if eval.Rules[0].Result != rules.Unknown || eval.Rules[0].Fired {
		t.Errorf("rules[0] = %+v, want unknown/not-fired (tri-state on the wire)", eval.Rules[0])
	}
	// The 4K rule fired but set only the library slot — flagged incomplete.
	if !eval.Rules[1].Fired || !eval.Rules[1].Incomplete {
		t.Errorf("rules[1] = %+v, want fired+incomplete", eval.Rules[1])
	}

	// 1080p release: no match, everything falls back.
	eval = evaluate("Dune.2021.1080p.WEB-DL.x264-GROUP")
	if len(eval.Fired) != 0 {
		t.Fatalf("Fired = %v, want none", eval.Fired)
	}
	if eval.Final.LibraryID == nil || *eval.Final.LibraryID != defaultLibrary.ID {
		t.Errorf("Final.LibraryID = %v, want default %s", eval.Final.LibraryID, defaultLibrary.ID)
	}
	want := routing.Fallbacks{Downloader: true, Library: true, NameTemplate: true}
	if eval.FellBack != want {
		t.Errorf("FellBack = %+v, want everything fell back", eval.FellBack)
	}
	if eval.Rules[1].Result != rules.False {
		t.Errorf("rules[1].Result = %v, want false (definite non-match)", eval.Rules[1].Result)
	}
}

// TestRouting_UpdateAndDelete covers the remaining CRUD wire surface.
func TestRouting_UpdateAndDelete(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	var created model.RoutingRule
	app.POST(t, "/api/v1/routing/rules", makeRoutingRuleBody("rule", resolutionCondition("2160p")), &created, http.StatusCreated)

	update := makeRoutingRuleBody("renamed", resolutionCondition("1080p"))
	update["enabled"] = false
	var updated model.RoutingRule
	app.PUT(t, "/api/v1/routing/rules/"+created.ID.String(), update, &updated, http.StatusOK)

	if updated.Name != "renamed" || updated.Enabled {
		t.Errorf("updated = %+v, want renamed/disabled", updated)
	}
	var cond rules.Condition
	if err := json.Unmarshal(updated.Conditions, &cond); err != nil {
		t.Fatalf("updated conditions don't decode: %v", err)
	}
	if cond.Right.Literal.Str != "1080p" {
		t.Errorf("updated condition literal = %q, want 1080p", cond.Right.Literal.Str)
	}

	app.DELETE(t, "/api/v1/routing/rules/"+created.ID.String(), http.StatusNoContent)

	var pd apperrors.ProblemDetails
	app.GET(t, "/api/v1/routing/rules/"+created.ID.String(), &pd, http.StatusNotFound)

	var list []model.RoutingRule
	app.GET(t, "/api/v1/routing/rules", &list, http.StatusOK)
	if len(list) != 0 {
		t.Errorf("expected empty list after delete, got %d", len(list))
	}
}

// TestRouting_FieldsCatalog asserts the field catalog serves the editor:
// registry-driven operators, timeline phases, and no unassembled namespaces.
func TestRouting_FieldsCatalog(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	var fields []model.FieldDefinition
	app.GET(t, "/api/v1/routing/fields", &fields, http.StatusOK)

	byPath := map[string]model.FieldDefinition{}
	for _, f := range fields {
		byPath[f.Path] = f
	}

	res, ok := byPath["quality.resolution"]
	if !ok {
		t.Fatal("quality.resolution missing from catalog")
	}
	if res.Type != model.FieldTypeEnum || res.Phase != model.PhaseSearch || len(res.EnumValues) == 0 {
		t.Errorf("quality.resolution = %+v, want search-phase enum with values", res)
	}
	for _, op := range res.Operators {
		if op == ">" {
			t.Errorf("enum field offers ordering operator %q", op)
		}
	}

	if mi, ok := byPath["mediainfo.video_codec"]; !ok || mi.Phase != model.PhaseImport {
		t.Errorf("mediainfo.video_codec = %+v, want import phase", mi)
	}

	// want.* is registered but unassembled — omitted so the editor can't
	// offer fields that always evaluate indeterminate.
	for path := range byPath {
		if len(path) >= 5 && path[:5] == "want." {
			t.Errorf("unassembled namespace leaked into the catalog: %s", path)
		}
	}
}
