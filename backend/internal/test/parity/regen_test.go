//go:build parity

// Package parity regenerates the parser golden files (Tier 2 of the parsing
// spec's testing strategy): it runs the labeled corpus through live, pinned
// Sonarr/Radarr containers and captures their parse output as ground truth.
//
// This is slow (it boots two containers) and needs the docker socket, so it
// lives behind the `parity` build tag and a dedicated recipe rather than in the
// per-PR integration suite:
//
//	just parity-regen
//
// The committed goldens it produces are read hermetically by the fast Tier-1
// parity test co-located with the parser (no containers).
package parity

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kyleaupton/arrflix/internal/test/starrtest"
)

const (
	inputsPath = "../../parsing/testdata/inputs.json"
	goldenDir  = "../../parsing/testdata"

	domainSeries = "series"
	domainMovie  = "movie"
)

// corpusEntry is one labeled input from inputs.json.
type corpusEntry struct {
	Input  string `json:"input"`
	Domain string `json:"domain"`
}

// goldenEntry is one captured reference parse, keyed by its input. Output is
// decoded into any so it pretty-prints uniformly in the golden file.
type goldenEntry struct {
	Input  string `json:"input"`
	Output any    `json:"output"`
}

func TestRegenerateGoldens(t *testing.T) {
	// Use Background, not t.Context: container teardown in the deferred
	// Terminate must run with a live context.
	ctx := context.Background()

	inputs := loadInputs(t)
	t.Logf("loaded %d corpus inputs", len(inputs))

	sonarr, radarr := startBoth(ctx, t)
	defer func() { _ = sonarr.Terminate(ctx) }()
	defer func() { _ = radarr.Terminate(ctx) }()

	if v, err := sonarr.Version(ctx); err == nil {
		t.Logf("Sonarr version: %s", v)
	}
	if v, err := radarr.Version(ctx); err == nil {
		t.Logf("Radarr version: %s", v)
	}

	var (
		sonarrGold, radarrGold []goldenEntry
		errCount               int
	)

	for _, e := range inputs {
		var inst *starrtest.Instance
		var dst *[]goldenEntry
		switch e.Domain {
		case domainSeries:
			inst, dst = sonarr, &sonarrGold
		case domainMovie:
			inst, dst = radarr, &radarrGold
		default:
			t.Errorf("unknown domain %q for input %q", e.Domain, e.Input)
			errCount++
			continue
		}

		raw, err := inst.Parse(ctx, e.Input)
		if err != nil {
			t.Logf("parse error for %q: %v", e.Input, err)
			errCount++
			continue
		}

		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Logf("decode error for %q: %v", e.Input, err)
			errCount++
			continue
		}
		*dst = append(*dst, goldenEntry{Input: e.Input, Output: out})
	}

	// Write what we captured even if some inputs errored, then surface the
	// failures so a flaky/partial run is visible.
	writeGolden(t, "sonarr.golden.json", sonarrGold)
	writeGolden(t, "radarr.golden.json", radarrGold)

	if errCount > 0 {
		t.Errorf("%d/%d inputs failed to parse; goldens written for the rest", errCount, len(inputs))
	}
}

func loadInputs(t *testing.T) []corpusEntry {
	t.Helper()
	data, err := os.ReadFile(inputsPath)
	if err != nil {
		t.Fatalf("read inputs: %v", err)
	}
	var entries []corpusEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("decode inputs: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("inputs.json is empty")
	}
	return entries
}

// startBoth boots Sonarr and Radarr concurrently (each first boot is ~tens of
// seconds), failing the test if either does not come up.
func startBoth(ctx context.Context, t *testing.T) (*starrtest.Instance, *starrtest.Instance) {
	t.Helper()

	type result struct {
		inst *starrtest.Instance
		err  error
	}
	sCh, rCh := make(chan result, 1), make(chan result, 1)
	go func() { i, err := starrtest.StartSonarr(ctx); sCh <- result{i, err} }()
	go func() { i, err := starrtest.StartRadarr(ctx); rCh <- result{i, err} }()
	sr, rr := <-sCh, <-rCh

	// If one came up and the other failed, don't leak the survivor.
	if sr.err != nil || rr.err != nil {
		if sr.inst != nil {
			_ = sr.inst.Terminate(ctx)
		}
		if rr.inst != nil {
			_ = rr.inst.Terminate(ctx)
		}
		if sr.err != nil {
			t.Fatalf("start sonarr: %v", sr.err)
		}
		t.Fatalf("start radarr: %v", rr.err)
	}
	return sr.inst, rr.inst
}

func writeGolden(t *testing.T, name string, entries []goldenEntry) {
	t.Helper()
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	data = append(data, '\n')
	path := filepath.Join(goldenDir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	t.Logf("wrote %d entries to %s", len(entries), path)
}
