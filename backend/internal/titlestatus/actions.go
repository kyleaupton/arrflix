package titlestatus

// Actions are the affordances the viewer may take on a title, computed here
// rather than in the component tree.
//
// The client currently decides the CTA's face itself: it filters the tier list
// against the viewer's grants and separately re-checks whether the choice will
// auto-approve. Both of those are the server's facts, and a client that decides
// them can only ever be a copy that drifts. Here the server holds the grant set
// and the state, so it answers directly: these are the buttons, this is what
// each will do.
//
// The rules below stay in this pure package for the same reason the state
// derivation does — every "which button should this person see" requirement
// becomes a table test with no database and no HTTP.

// ActionKind names an affordance. The set is deliberately small: an action
// exists here only when the projection can state its consequences honestly.
// See "Not yet modeled" at the bottom of this file.
type ActionKind string

const (
	ActionRequest ActionKind = "request"
	ActionCancel  ActionKind = "cancel"
)

// TierOption is a quality tier the viewer may request at, and whether choosing
// it lands in the approval queue rather than starting work.
//
// Approval is per tier, not per user: a viewer can be trusted to pull HD on
// their own and still need a decision for 4K. Carrying the flag per tier is what
// lets the UI tell them which is which before they commit — the substance of
// REQ-APPROVE-012.
type TierOption struct {
	Tier             string
	RequiresApproval bool
}

// Grants is the viewer's permission set reduced to the decisions this package
// makes. Booleans rather than permission keys: resolving keys against the RBAC
// catalog is the service's job, and keeping the catalog out of here is what
// keeps these rules testable without an RBAC fixture.
type Grants struct {
	// RequestTiers are the tiers this viewer may create a request at, best
	// first. Empty means they may not request at all.
	RequestTiers []TierOption
	// CanCancel is whether the viewer may withdraw their own live request.
	CanCancel bool
}

// Viewer is who is asking. IsRequester means the viewer has a *live* request for
// this title — pending, approved, or spawned. A canceled or denied request
// leaves it false, so the affordance returns to asking rather than withdrawing.
type Viewer struct {
	IsRequester bool
	Grants      Grants
}

// Action is an affordance plus its consequences.
//
// A disabled action is still sent when the affordance needs to explain itself.
// An action that simply does not apply is omitted; one that is unavailable for a
// reason the viewer should know is present, disabled, and carries the reason.
type Action struct {
	Kind ActionKind
	// Enabled is whether the viewer may take the action now.
	Enabled bool
	// RequiresApproval is true only when *every* offered tier needs a decision,
	// so a single-tier UI can read it directly. When tiers differ, this is false
	// and Tiers carries the truth.
	RequiresApproval bool
	// Tiers is the request action's tier choice. Empty for other kinds.
	Tiers []TierOption
	// DisabledReason explains an Enabled=false action in the viewer's terms.
	DisabledReason string
}

// deriveActions computes the affordances for one viewer against a derived state.
func deriveActions(in Input, state State) []Action {
	var out []Action
	if a, ok := requestAction(in, state); ok {
		out = append(out, a)
	}
	if a, ok := cancelAction(in); ok {
		out = append(out, a)
	}
	return out
}

// requestAction offers the ask. It is absent, rather than disabled, in each case
// below: none of them is a condition the viewer can act on by seeing a greyed
// button.
func requestAction(in Input, state State) (Action, bool) {
	// Already asked. Withdrawing is the affordance now, not asking twice.
	if in.Viewer.IsRequester {
		return Action{}, false
	}

	// No tier this viewer may request at. A permission they don't hold is not a
	// state that will change, so showing it disabled would only invite them to
	// keep trying.
	tiers := in.Viewer.Grants.RequestTiers
	if len(tiers) == 0 {
		return Action{}, false
	}

	// A movie is one atom; once it is on disk there is nothing left to ask for.
	// A series is never finished this way — later seasons are always askable —
	// so it keeps the affordance even when every episode in scope is available.
	if in.MediaType == MediaTypeMovie && state == StateAvailable {
		return Action{}, false
	}

	return Action{
		Kind:             ActionRequest,
		Enabled:          true,
		RequiresApproval: allRequireApproval(tiers),
		Tiers:            tiers,
	}, true
}

// cancelAction offers withdrawal of the viewer's own live request.
func cancelAction(in Input) (Action, bool) {
	if !in.Viewer.IsRequester {
		return Action{}, false
	}
	if !in.Viewer.Grants.CanCancel {
		// The viewer has a live request they may not withdraw. Unlike a missing
		// request permission, this one is worth saying out loud: they can see
		// their own request, so its absence of a cancel button needs explaining.
		return Action{
			Kind:           ActionCancel,
			Enabled:        false,
			DisabledReason: "you do not have permission to withdraw this request",
		}, true
	}
	return Action{Kind: ActionCancel, Enabled: true}, true
}

// allRequireApproval reports whether every tier lands in the approval queue.
// False for an empty set, which never reaches here.
func allRequireApproval(tiers []TierOption) bool {
	for _, t := range tiers {
		if !t.RequiresApproval {
			return false
		}
	}
	return len(tiers) > 0
}

// Not yet modeled, and deliberately so — each needs a fact the projection cannot
// state honestly today:
//
//   - approve / deny — need the pending request of *any* requester on this
//     title, plus its id to act on. Today Input carries only the viewer's own
//     request. These are also queue affordances more than focus-page ones.
//   - retry — needs the distinction between "searched and found nothing" and
//     "the indexer was unreachable", which lives in want.last_error rather than
//     in the state.
//   - pick / upgrade — need the candidate set and the tier comparison.
//   - effect.episodesAdded — the scope diff ("Season 3 is already followed;
//     requesting adds Seasons 1-2") requires resolving the requester-union
//     scope, which is deliberately never materialized.
//   - effect.bytesEstimate — no size estimate is persisted pre-grab.
//
// Shipping a half-supported action is worse than omitting it: the client would
// render an affordance whose consequences the server is guessing at.
