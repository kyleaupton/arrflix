// Package titlestatus derives the user-facing acquisition state of a title —
// the single answer to "what is happening with this, right now" that every
// surface renders: poster chip, hero control, status card, season grid.
//
// Events named after rows (want_updated, download_job_updated) carry no meaning
// on their own, so each consumer joined them against its own cached state and
// reached its own conclusion. Six surfaces derived one concept six ways, and
// they disagreed. This package is the one derivation they all share.
//
// It is deliberately pure: it imports only time + stdlib, has no repo/db/model
// dependencies, and defines its own minimal input shapes. The service in
// internal/service/ fetches wants, files, jobs, and the request, maps them onto
// Input, and calls Derive. Every function here is total — an unrecognized input
// yields a defined state, never an error.
//
// See specs/modules/title-status/README.md for the model this implements.
package titlestatus

import "time"

// MediaType distinguishes the two title shapes. A movie has exactly one
// acquirable item; a series has one per in-scope episode.
type MediaType string

const (
	MediaTypeMovie  MediaType = "movie"
	MediaTypeSeries MediaType = "series"
)

// State is the headline — what the chip renders. It is deliberately not the
// whole truth: a title can be available *and* working (an upgrade in flight),
// so activity lives on Result.Active rather than adding states for each
// combination.
type State string

const (
	StateNotRequested       State = "not_requested"
	StateUnreleased         State = "unreleased"
	StateAwaitingApproval   State = "awaiting_approval"
	StateDenied             State = "denied"
	StateSearching          State = "searching"
	StateNeedsPick          State = "needs_pick"
	StateProposed           State = "proposed"
	StateDownloading        State = "downloading"
	StateImporting          State = "importing"
	StateAvailable          State = "available"
	StatePartiallyAvailable State = "partially_available"
	StateUnavailable        State = "unavailable"
	StateCanceled           State = "canceled"
)

// Phase is what the pipeline is actively doing, independent of what is already
// on disk. PhaseNone covers both "nothing to do" and "waiting on a date" —
// waiting is not work.
type Phase string

const (
	PhaseNone        Phase = ""
	PhaseSearching   Phase = "searching"
	PhaseDownloading Phase = "downloading"
	PhaseImporting   Phase = "importing"
)

// Want status values, mirroring model.WantStatus. Redeclared rather than
// imported to keep this package free of model: the service passes the raw
// string through, and an unrecognized value degrades gracefully.
const (
	wantPending     = "pending"
	wantSearching   = "searching"
	wantGrabbed     = "grabbed"
	wantDownloading = "downloading"
	wantImported    = "imported"
	wantAvailable   = "available"
	wantFailed      = "failed"
	wantCanceled    = "canceled"
)

// Want hold values, mirroring model.WantHold*. A held want is visible but not
// claimable — it is waiting on a person, not on the pipeline.
const (
	holdNeedsPick = "needs_pick"
	holdProposed  = "proposed"
)

// Request status values, mirroring model.RequestStatus. Only pending and denied
// reach the headline; approved and spawned are superseded by the work they
// produced, and canceled leaves nothing to report.
const (
	requestPending = "pending"
	requestDenied  = "denied"
)

// Want is the minimal want shape the derivation needs. Hold is nil when the
// want is free to be claimed.
type Want struct {
	Status string
	Hold   *string
}

// Request is the viewer's own request, not anyone else's. It governs the
// headline only while nothing has started — see Derive.
type Request struct {
	Status string
}

// Item is one atom: a movie, or a single episode.
//
// InScope marks an atom the title is actually trying to acquire. Out-of-scope
// atoms still get a state — a season grid shows every episode, including ones
// nobody asked for — but they are excluded from the counts and the headline.
// Without that split, a series whose in-scope episodes are all acquired reports
// partially_available forever, held back by episodes it was never going to get.
//
// ObtainableAt is when the atom can first be gotten — a film's home release, an
// episode's air date. Nil means obtainable now (or unknown, which is treated
// the same: we look rather than assume). ActiveJob covers the hand-grab path,
// where an operator started a download with no want behind it.
type Item struct {
	InScope      bool
	HasFile      bool
	Want         *Want
	ActiveJob    bool
	ObtainableAt *time.Time
}

// Input is everything the derivation reads. Items holds one entry per acquirable
// atom — exactly one for a movie, one per in-scope episode for a series.
type Input struct {
	MediaType MediaType
	Items     []Item
	Request   *Request
	Now       time.Time
}

// Library is what is on disk. Counted from Items rather than derived, but
// returned alongside the state so consumers need only one call.
type Library struct {
	HasFiles  bool
	FileCount int
}

// Counts summarize the items. Working counts atoms the pipeline is actively
// progressing, which is independent of Available — an available atom with an
// upgrade in flight is both.
type Counts struct {
	Total     int
	Available int
	Working   int
}

// Result is the derived projection core. ItemStates is index-aligned with
// Input.Items so a season grid and the title chip come from one call and cannot
// drift apart.
type Result struct {
	State      State
	Phase      Phase
	Active     bool
	Library    Library
	Counts     Counts
	ItemStates []State
}

// stateDominance orders headline states by how much they warrant attention,
// most first. When no atom is available, the title reports the most dominant
// state present among its items. States needing a person outrank work in
// progress, which outranks waiting, which outranks terminal outcomes.
var stateDominance = []State{
	StateNeedsPick,
	StateProposed,
	StateDownloading,
	StateImporting,
	StateSearching,
	StateUnreleased,
	StateUnavailable,
	StateCanceled,
	StateNotRequested,
}

// phaseDominance orders active phases for the title-level headline. A title
// with one episode downloading and another importing reports downloading.
var phaseDominance = []Phase{
	PhaseDownloading,
	PhaseImporting,
	PhaseSearching,
}

// Derive computes the title's state from its items and the viewer's request.
// Pure and total.
//
// Three rules govern the headline:
//
//   - A file on disk wins. An atom with a file is available whatever its want
//     says, because the file is ground truth and the want may be a stale
//     cancellation or an upgrade still running.
//   - The viewer's request governs only while nothing has started. A pending or
//     denied request is the headline when there is no file and no work; once
//     either exists, what the system is doing outranks what was asked.
//   - Activity is orthogonal. Result.Active and Result.Phase are read from the
//     wants, never from the item states, so an upgrade behind an existing file
//     is visible rather than masked by it.
func Derive(in Input) Result {
	res := Result{
		State:      StateNotRequested,
		ItemStates: make([]State, len(in.Items)),
	}

	dominantPhase := PhaseNone
	scoped := make([]State, 0, len(in.Items))

	for i, item := range in.Items {
		st := DeriveItem(item, in.Now)
		res.ItemStates[i] = st

		// A file counts toward the library whether or not the title still wants
		// it — it is on disk either way.
		if item.HasFile {
			res.Library.FileCount++
		}

		if !item.InScope {
			continue
		}

		scoped = append(scoped, st)
		res.Counts.Total++
		if st == StateAvailable {
			res.Counts.Available++
		}
		if ph := itemPhase(item, in.Now); ph != PhaseNone {
			res.Counts.Working++
			dominantPhase = morePhase(dominantPhase, ph)
		}
	}

	res.Library.HasFiles = res.Library.FileCount > 0
	res.Phase = dominantPhase
	res.Active = dominantPhase != PhaseNone
	res.State = headline(in, res, scoped)

	return res
}

// headline picks the title-level state. scoped holds the derived states of the
// in-scope atoms only — out-of-scope ones are visible in the grid but must not
// speak for the title.
func headline(in Input, res Result, scoped []State) State {
	// A pending or denied request speaks only into a vacuum.
	if reqState := requestHeadline(in.Request); reqState != "" {
		if !res.Library.HasFiles && res.Counts.Working == 0 {
			return reqState
		}
	}

	if res.Counts.Total == 0 {
		return StateNotRequested
	}

	switch {
	case res.Counts.Available == res.Counts.Total:
		return StateAvailable
	case res.Counts.Available > 0:
		// Only a series can be partly there; a movie has a single atom.
		if in.MediaType == MediaTypeSeries {
			return StatePartiallyAvailable
		}
		return StateAvailable
	}

	return dominantState(scoped)
}

// DeriveItem computes the state of a single atom. Exported because a season
// grid renders per-episode cells from the same vocabulary as the title chip.
func DeriveItem(item Item, now time.Time) State {
	if item.HasFile {
		return StateAvailable
	}

	if item.Want != nil {
		return wantState(*item.Want, item, now)
	}

	if item.ActiveJob {
		return StateDownloading
	}
	if notYetObtainable(item, now) {
		return StateUnreleased
	}
	return StateNotRequested
}

// wantState maps a want onto a user-facing state. A hold is checked before
// status because a held want sits at pending — the status says nothing about
// why it is parked.
func wantState(w Want, item Item, now time.Time) State {
	if w.Hold != nil {
		switch *w.Hold {
		case holdProposed:
			return StateProposed
		case holdNeedsPick:
			return StateNeedsPick
		}
	}

	switch w.Status {
	case wantAvailable:
		return StateAvailable
	case wantImported:
		return StateImporting
	case wantGrabbed, wantDownloading:
		return StateDownloading
	case wantFailed:
		return StateUnavailable
	case wantCanceled:
		return StateCanceled
	case wantPending, wantSearching:
		// The want oscillates pending↔searching every cycle it finds nothing.
		// Both report as searching so the user sees one stable state rather
		// than a value flickering at the worker's cadence (REQ-SEARCH-002).
		if notYetObtainable(item, now) {
			return StateUnreleased
		}
		return StateSearching
	}

	// A want exists, so something is underway; an unknown status is more
	// honestly reported as searching than as nothing at all.
	return StateSearching
}

// itemPhase reports what the pipeline is actively doing to an atom. It reads
// the want directly rather than the derived state, so work behind an existing
// file (an upgrade) stays visible.
func itemPhase(item Item, now time.Time) Phase {
	if item.Want == nil {
		if item.ActiveJob {
			return PhaseDownloading
		}
		return PhaseNone
	}

	w := *item.Want
	// A held want is waiting on a person, not progressing.
	if w.Hold != nil {
		return PhaseNone
	}

	switch w.Status {
	case wantGrabbed, wantDownloading:
		return PhaseDownloading
	case wantImported:
		return PhaseImporting
	case wantPending, wantSearching:
		// Waiting for a release date is not work.
		if notYetObtainable(item, now) {
			return PhaseNone
		}
		return PhaseSearching
	}
	return PhaseNone
}

// requestHeadline maps a request onto the state it would contribute, or empty
// if it has nothing to say. Approved and spawned requests are superseded by the
// work they produced; canceled ones leave nothing to report.
func requestHeadline(r *Request) State {
	if r == nil {
		return ""
	}
	switch r.Status {
	case requestPending:
		return StateAwaitingApproval
	case requestDenied:
		return StateDenied
	}
	return ""
}

// notYetObtainable reports whether an atom cannot be gotten yet. A nil date
// means obtainable — unknown dates are searched rather than deferred, since
// declining to look is the more expensive mistake.
func notYetObtainable(item Item, now time.Time) bool {
	return item.ObtainableAt != nil && item.ObtainableAt.After(now)
}

// dominantState returns the most attention-worthy state present, falling back
// to not-requested for an empty or wholly unrecognized set.
func dominantState(states []State) State {
	present := make(map[State]bool, len(states))
	for _, s := range states {
		present[s] = true
	}
	for _, s := range stateDominance {
		if present[s] {
			return s
		}
	}
	return StateNotRequested
}

// morePhase returns whichever phase ranks higher in phaseDominance.
func morePhase(a, b Phase) Phase {
	if phaseRank(a) <= phaseRank(b) {
		return a
	}
	return b
}

// phaseRank is the index of a phase in phaseDominance; PhaseNone and any
// unrecognized value rank last.
func phaseRank(p Phase) int {
	for i, q := range phaseDominance {
		if q == p {
			return i
		}
	}
	return len(phaseDominance)
}
