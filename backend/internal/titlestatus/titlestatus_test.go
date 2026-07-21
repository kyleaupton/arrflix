package titlestatus

import (
	"testing"
	"time"
)

var now = time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

func past() *time.Time   { t := now.Add(-24 * time.Hour); return &t }
func future() *time.Time { t := now.Add(24 * time.Hour); return &t }

func held(h string) *Want { return &Want{Status: wantPending, Hold: &h} }
func want(s string) *Want { return &Want{Status: s} }
func movie(items ...Item) Input {
	return Input{MediaType: MediaTypeMovie, Items: items, Now: now}
}
func series(items ...Item) Input {
	return Input{MediaType: MediaTypeSeries, Items: items, Now: now}
}

func TestDeriveItem(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		item Item
		want State
	}{
		{"no want, no job, obtainable", Item{}, StateNotRequested},
		{"no want, not yet obtainable", Item{ObtainableAt: future()}, StateUnreleased},
		{"no want, obtainable date passed", Item{ObtainableAt: past()}, StateNotRequested},
		{"hand-grabbed job with no want", Item{ActiveJob: true}, StateDownloading},

		{"pending want", Item{Want: want(wantPending)}, StateSearching},
		{"searching want", Item{Want: want(wantSearching)}, StateSearching},
		{"grabbed want", Item{Want: want(wantGrabbed)}, StateDownloading},
		{"downloading want", Item{Want: want(wantDownloading)}, StateDownloading},
		{"imported want", Item{Want: want(wantImported)}, StateImporting},
		{"available want", Item{Want: want(wantAvailable)}, StateAvailable},
		{"failed want", Item{Want: want(wantFailed)}, StateUnavailable},
		{"canceled want", Item{Want: want(wantCanceled)}, StateCanceled},

		{"held for a pick", Item{Want: held(holdNeedsPick)}, StateNeedsPick},
		{"held on a proposal", Item{Want: held(holdProposed)}, StateProposed},

		{"pending want before the date", Item{Want: want(wantPending), ObtainableAt: future()}, StateUnreleased},
		{"grabbed want before the date still downloads", Item{Want: want(wantGrabbed), ObtainableAt: future()}, StateDownloading},

		{"file present", Item{HasFile: true}, StateAvailable},
		{"file beats a canceled want", Item{HasFile: true, Want: want(wantCanceled)}, StateAvailable},
		{"file beats an in-flight upgrade", Item{HasFile: true, Want: want(wantSearching)}, StateAvailable},

		{"unrecognized want status", Item{Want: want("wat")}, StateSearching},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DeriveItem(tc.item, now); got != tc.want {
				t.Fatalf("DeriveItem(%+v) = %q, want %q", tc.item, got, tc.want)
			}
		})
	}
}

// REQ-SEARCH-002 — the state shown to a user must stay stable while the want
// cycles internally. A want that finds nothing returns to pending and is
// re-claimed as searching on the next tick, so the raw status flickers at the
// worker's cadence. Both must report as one state.
func TestReqSearch002_SearchingIsStableAcrossWantOscillation(t *testing.T) {
	t.Parallel()

	pending := Derive(movie(Item{Want: want(wantPending)}))
	searching := Derive(movie(Item{Want: want(wantSearching)}))

	if pending.State != StateSearching || searching.State != StateSearching {
		t.Fatalf("pending=%q searching=%q, want both %q", pending.State, searching.State, StateSearching)
	}
	if pending.Phase != searching.Phase || pending.Active != searching.Active {
		t.Fatalf("activity differs across the cycle: pending=%+v searching=%+v", pending, searching)
	}
}

// REQ-SEARCH-003 — a user must be able to distinguish "working, just waiting"
// from "stuck, needs you." Only one of them wants attention, so they must not
// collapse into a single state.
func TestReqSearch003_WorkingIsDistinctFromStuck(t *testing.T) {
	t.Parallel()

	working := Derive(movie(Item{Want: want(wantSearching)}))
	stuck := Derive(movie(Item{Want: held(holdNeedsPick)}))
	failed := Derive(movie(Item{Want: want(wantFailed)}))

	if !working.Active {
		t.Error("a searching want must read as active work")
	}
	if stuck.Active {
		t.Error("a want held for a human pick must not read as active work")
	}
	if failed.Active {
		t.Error("a failed want must not read as active work")
	}
	if working.State == stuck.State || working.State == failed.State {
		t.Fatalf("states collapse: working=%q stuck=%q failed=%q", working.State, stuck.State, failed.State)
	}
}

// REQ-UNREL-001 — a title that cannot yet be obtained must be presented as
// waiting for a date, never as searching or failing.
func TestReqUnrel001_UnobtainableIsNotSearching(t *testing.T) {
	t.Parallel()

	res := Derive(movie(Item{Want: want(wantPending), ObtainableAt: future()}))

	if res.State != StateUnreleased {
		t.Fatalf("State = %q, want %q", res.State, StateUnreleased)
	}
	if res.Active || res.Phase != PhaseNone {
		t.Errorf("waiting for a date must not read as work: active=%v phase=%q", res.Active, res.Phase)
	}
	if res.Counts.Working != 0 {
		t.Errorf("Counts.Working = %d, want 0", res.Counts.Working)
	}
}

// REQ-UNREL-006 — when the obtainable date passes, the want must become
// ordinary searching work under the normal cadence.
func TestReqUnrel006_ObtainableDatePassingResumesSearching(t *testing.T) {
	t.Parallel()

	res := Derive(movie(Item{Want: want(wantPending), ObtainableAt: past()}))

	if res.State != StateSearching {
		t.Fatalf("State = %q, want %q", res.State, StateSearching)
	}
	if !res.Active || res.Phase != PhaseSearching {
		t.Errorf("a title past its date must read as working: active=%v phase=%q", res.Active, res.Phase)
	}
}

// REQ-UNREL-009 — a title with no known date must be accepted and presented as
// an indefinite watch, never as a scheduled wait against an invented date.
func TestReqUnrel009_UnknownDateIsWatchedNotDeferred(t *testing.T) {
	t.Parallel()

	res := Derive(movie(Item{Want: want(wantPending)}))

	if res.State == StateUnreleased {
		t.Fatalf("a nil obtainable date must not produce %q", StateUnreleased)
	}
	if res.State != StateSearching {
		t.Fatalf("State = %q, want %q", res.State, StateSearching)
	}
}

// REQ-UPGRADE-001 / REQ-UPGRADE-012 — reaching available must not permanently
// end the system's interest in a title, and a tracking still watching for
// upgrades must not read as finished. The projection must therefore be able to
// say "available" and "working" at the same time.
func TestReqUpgrade001_AvailableAndWorkingAreSimultaneous(t *testing.T) {
	t.Parallel()

	res := Derive(movie(Item{HasFile: true, Want: want(wantSearching)}))

	if res.State != StateAvailable {
		t.Fatalf("State = %q, want %q — the file on disk is ground truth", res.State, StateAvailable)
	}
	if !res.Active || res.Phase != PhaseSearching {
		t.Fatalf("the in-flight upgrade is masked: active=%v phase=%q", res.Active, res.Phase)
	}
	if res.Counts.Available != 1 || res.Counts.Working != 1 {
		t.Fatalf("Counts = %+v, want the atom counted as both available and working", res.Counts)
	}
}

// REQ-SERIES-002 — episodes that have not aired must exist as visible future
// work, not as absences. They must be present in the per-item states with a
// state of their own rather than omitted.
func TestReqSeries002_UnairedEpisodesArePresent(t *testing.T) {
	t.Parallel()

	res := Derive(series(
		Item{HasFile: true},
		Item{Want: want(wantPending), ObtainableAt: future()},
		Item{Want: want(wantPending), ObtainableAt: future()},
	))

	if len(res.ItemStates) != 3 {
		t.Fatalf("ItemStates has %d entries, want 3 — unaired episodes must not be dropped", len(res.ItemStates))
	}
	for i, s := range res.ItemStates[1:] {
		if s != StateUnreleased {
			t.Errorf("ItemStates[%d] = %q, want %q", i+1, s, StateUnreleased)
		}
	}
	if res.Counts.Total != 3 {
		t.Errorf("Counts.Total = %d, want 3", res.Counts.Total)
	}
}

// REQ-SERIES-008 — series-level progress must be presentable without composing
// it from per-episode state. The rollup is produced here, once.
func TestReqSeries008_ProgressIsRolledUp(t *testing.T) {
	t.Parallel()

	res := Derive(series(
		Item{HasFile: true},
		Item{HasFile: true},
		Item{Want: want(wantDownloading)},
		Item{Want: want(wantPending)},
		Item{Want: want(wantPending), ObtainableAt: future()},
	))

	if res.State != StatePartiallyAvailable {
		t.Fatalf("State = %q, want %q", res.State, StatePartiallyAvailable)
	}
	if res.Counts.Total != 5 || res.Counts.Available != 2 || res.Counts.Working != 2 {
		t.Fatalf("Counts = %+v, want {Total:5 Available:2 Working:2}", res.Counts)
	}
	if res.Library.FileCount != 2 || !res.Library.HasFiles {
		t.Fatalf("Library = %+v, want 2 files", res.Library)
	}
	if res.Phase != PhaseDownloading {
		t.Errorf("Phase = %q, want %q — downloading outranks searching", res.Phase, PhaseDownloading)
	}
}

func TestDeriveSeriesRollup(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		in    Input
		state State
	}{
		{"every episode present", series(Item{HasFile: true}, Item{HasFile: true}), StateAvailable},
		{"some episodes present", series(Item{HasFile: true}, Item{Want: want(wantPending)}), StatePartiallyAvailable},
		{"none present", series(Item{Want: want(wantPending)}, Item{Want: want(wantPending)}), StateSearching},
		{"no episodes at all", series(), StateNotRequested},
		{
			"a pick needed outranks work in progress",
			series(Item{Want: held(holdNeedsPick)}, Item{Want: want(wantDownloading)}),
			StateNeedsPick,
		},
		{
			"work in progress outranks waiting",
			series(Item{Want: want(wantDownloading)}, Item{Want: want(wantPending), ObtainableAt: future()}),
			StateDownloading,
		},
		{
			"waiting outranks a failure",
			series(Item{Want: want(wantFailed)}, Item{Want: want(wantPending), ObtainableAt: future()}),
			StateUnreleased,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Derive(tc.in).State; got != tc.state {
				t.Fatalf("State = %q, want %q", got, tc.state)
			}
		})
	}
}

// A movie has a single atom, so it can never be partially available.
func TestDeriveMovieIsNeverPartial(t *testing.T) {
	t.Parallel()

	if got := Derive(movie(Item{HasFile: true})).State; got != StateAvailable {
		t.Fatalf("State = %q, want %q", got, StateAvailable)
	}
}

// The viewer's own request is the headline only while nothing has started.
// Once a file exists or work is underway, what the system is doing outranks
// what was asked.
func TestDeriveRequestGovernsOnlyInAVacuum(t *testing.T) {
	t.Parallel()

	pending := &Request{Status: requestPending}
	denied := &Request{Status: requestDenied}

	cases := []struct {
		name  string
		in    Input
		state State
	}{
		{
			"pending request, nothing started",
			Input{MediaType: MediaTypeMovie, Request: pending, Now: now},
			StateAwaitingApproval,
		},
		{
			"denied request, nothing started",
			Input{MediaType: MediaTypeMovie, Request: denied, Now: now},
			StateDenied,
		},
		{
			"pending request but the title is already in the library",
			Input{MediaType: MediaTypeMovie, Request: pending, Items: []Item{{HasFile: true}}, Now: now},
			StateAvailable,
		},
		{
			"denied request but someone else already got it",
			Input{MediaType: MediaTypeMovie, Request: denied, Items: []Item{{HasFile: true}}, Now: now},
			StateAvailable,
		},
		{
			"pending request but work is already underway",
			Input{MediaType: MediaTypeMovie, Request: pending, Items: []Item{{Want: want(wantDownloading)}}, Now: now},
			StateDownloading,
		},
		{
			"an approved request says nothing on its own",
			Input{MediaType: MediaTypeMovie, Request: &Request{Status: "approved"}, Now: now},
			StateNotRequested,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Derive(tc.in).State; got != tc.state {
				t.Fatalf("State = %q, want %q", got, tc.state)
			}
		})
	}
}

// Derive is total: no input shape produces an empty state or a panic.
func TestDeriveIsTotal(t *testing.T) {
	t.Parallel()

	cases := []Input{
		{},
		{MediaType: MediaTypeMovie, Now: now},
		{MediaType: "wat", Items: []Item{{Want: want("wat")}}, Now: now},
		{MediaType: MediaTypeSeries, Items: []Item{{Want: &Want{Status: "wat", Hold: strp("wat")}}}, Now: now},
		{MediaType: MediaTypeSeries, Request: &Request{Status: "wat"}, Now: now},
	}

	for i, in := range cases {
		res := Derive(in)
		if res.State == "" {
			t.Errorf("case %d: empty state for %+v", i, in)
		}
		if len(res.ItemStates) != len(in.Items) {
			t.Errorf("case %d: %d item states for %d items", i, len(res.ItemStates), len(in.Items))
		}
		for j, s := range res.ItemStates {
			if s == "" {
				t.Errorf("case %d: empty item state at %d", i, j)
			}
		}
	}
}

func strp(s string) *string { return &s }
