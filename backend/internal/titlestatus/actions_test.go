package titlestatus

import "testing"

var (
	hdOnly    = []TierOption{{Tier: "HD", RequiresApproval: false}}
	hdGated   = []TierOption{{Tier: "HD", RequiresApproval: true}}
	hdAnd4K   = []TierOption{{Tier: "HD", RequiresApproval: false}, {Tier: "4K", RequiresApproval: true}}
	bothGated = []TierOption{{Tier: "HD", RequiresApproval: true}, {Tier: "4K", RequiresApproval: true}}
)

// requester is a viewer who may request at the given tiers and withdraw.
func requester(tiers []TierOption) Viewer {
	return Viewer{Grants: Grants{RequestTiers: tiers, CanCancel: true}}
}

// find returns the action of the given kind, or false.
func find(actions []Action, kind ActionKind) (Action, bool) {
	for _, a := range actions {
		if a.Kind == kind {
			return a, true
		}
	}
	return Action{}, false
}

func TestRequestIsOfferedToSomeoneWhoHasNotAsked(t *testing.T) {
	t.Parallel()

	res := Derive(Input{
		MediaType: MediaTypeMovie,
		Items:     []Item{{InScope: false}},
		Viewer:    requester(hdOnly),
		Now:       now,
	})

	a, ok := find(res.Actions, ActionRequest)
	if !ok {
		t.Fatal("an unrequested title must offer the request action")
	}
	if !a.Enabled {
		t.Fatal("request must be enabled for a viewer who holds a create tier")
	}
	if len(a.Tiers) != 1 || a.Tiers[0].Tier != "HD" {
		t.Fatalf("request must carry the viewer's tiers, got %v", a.Tiers)
	}
}

// The client currently filters the tier list itself. The projection carries only
// the tiers this viewer may actually choose, so there is nothing left to filter.
func TestRequestCarriesOnlyTheViewersOwnTiers(t *testing.T) {
	t.Parallel()

	res := Derive(Input{
		MediaType: MediaTypeMovie,
		Items:     []Item{{InScope: false}},
		Viewer:    requester(hdOnly),
		Now:       now,
	})

	a, _ := find(res.Actions, ActionRequest)
	for _, tier := range a.Tiers {
		if tier.Tier == "4K" {
			t.Fatal("a viewer without the 4k create grant must not be offered it")
		}
	}
}

// REQ-APPROVE-012: a user must know their choice requires approval before they
// commit to it. Approval is per tier, so the flag has to be too.
func TestReqApprove012_ApprovalIsKnownPerTierBeforeCommitting(t *testing.T) {
	t.Parallel()

	res := Derive(Input{
		MediaType: MediaTypeMovie,
		Items:     []Item{{InScope: false}},
		Viewer:    requester(hdAnd4K),
		Now:       now,
	})

	a, _ := find(res.Actions, ActionRequest)
	if len(a.Tiers) != 2 {
		t.Fatalf("both tiers must be offered, got %v", a.Tiers)
	}
	if a.Tiers[0].RequiresApproval {
		t.Error("HD is auto-approved for this viewer and must say so")
	}
	if !a.Tiers[1].RequiresApproval {
		t.Error("4K needs a decision for this viewer and must say so")
	}
	if a.RequiresApproval {
		t.Error("the headline flag must be false when tiers differ; the per-tier flags carry the truth")
	}
}

func TestRequiresApprovalIsSetWhenEveryTierNeedsADecision(t *testing.T) {
	t.Parallel()

	for name, tiers := range map[string][]TierOption{"single tier": hdGated, "every tier": bothGated} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := Derive(Input{
				MediaType: MediaTypeMovie,
				Items:     []Item{{InScope: false}},
				Viewer:    requester(tiers),
				Now:       now,
			})
			a, _ := find(res.Actions, ActionRequest)
			if !a.RequiresApproval {
				t.Fatal("every offered tier needs approval, so the headline flag must be set")
			}
		})
	}
}

// A permission the viewer does not hold is not a state that will change, so the
// affordance is absent rather than greyed out.
func TestRequestIsAbsentWithoutACreateGrant(t *testing.T) {
	t.Parallel()

	res := Derive(Input{
		MediaType: MediaTypeMovie,
		Items:     []Item{{InScope: false}},
		Viewer:    Viewer{Grants: Grants{CanCancel: true}},
		Now:       now,
	})

	if _, ok := find(res.Actions, ActionRequest); ok {
		t.Fatal("a viewer with no create tier must not be offered the request action at all")
	}
}

func TestRequestIsReplacedByCancelOnceAsked(t *testing.T) {
	t.Parallel()

	viewer := requester(hdOnly)
	viewer.IsRequester = true

	res := Derive(Input{
		MediaType: MediaTypeMovie,
		Items:     []Item{{InScope: true, Want: want(wantSearching)}},
		Request:   &Request{Status: requestPending},
		Viewer:    viewer,
		Now:       now,
	})

	if _, ok := find(res.Actions, ActionRequest); ok {
		t.Error("a viewer with a live request must not be offered a second one")
	}
	a, ok := find(res.Actions, ActionCancel)
	if !ok || !a.Enabled {
		t.Errorf("a live request must offer an enabled cancel, got %+v ok=%v", a, ok)
	}
}

// A movie is one atom; once it is on disk there is nothing left to ask for.
func TestRequestIsAbsentForAMovieAlreadyInTheLibrary(t *testing.T) {
	t.Parallel()

	res := Derive(Input{
		MediaType: MediaTypeMovie,
		Items:     []Item{{InScope: true, HasFile: true}},
		Viewer:    requester(hdOnly),
		Now:       now,
	})

	if res.State != StateAvailable {
		t.Fatalf("precondition: state = %q, want available", res.State)
	}
	if _, ok := find(res.Actions, ActionRequest); ok {
		t.Fatal("an available movie affords no further request")
	}
}

// A series is never finished the way a movie is — later seasons are always
// askable, so a fully-acquired one keeps the affordance.
func TestRequestSurvivesAFullyAcquiredSeries(t *testing.T) {
	t.Parallel()

	res := Derive(Input{
		MediaType: MediaTypeSeries,
		Items:     inScope([]Item{{HasFile: true}, {HasFile: true}}),
		Viewer:    requester(hdOnly),
		Now:       now,
	})

	if res.State != StateAvailable {
		t.Fatalf("precondition: state = %q, want available", res.State)
	}
	if _, ok := find(res.Actions, ActionRequest); !ok {
		t.Fatal("a series always affords asking for more")
	}
}

// A viewer who can see their own request but may not withdraw it deserves an
// explanation, not a missing button.
func TestCancelIsShownDisabledWhenTheViewerMayNotWithdraw(t *testing.T) {
	t.Parallel()

	res := Derive(Input{
		MediaType: MediaTypeMovie,
		Items:     []Item{{InScope: true, Want: want(wantSearching)}},
		Viewer:    Viewer{IsRequester: true, Grants: Grants{RequestTiers: hdOnly}},
		Now:       now,
	})

	a, ok := find(res.Actions, ActionCancel)
	if !ok {
		t.Fatal("cancel must be present so it can explain itself")
	}
	if a.Enabled {
		t.Fatal("cancel must be disabled without the grant")
	}
	if a.DisabledReason == "" {
		t.Fatal("a disabled action must carry its reason")
	}
}

func TestCancelIsAbsentForSomeoneElsesTitle(t *testing.T) {
	t.Parallel()

	res := Derive(Input{
		MediaType: MediaTypeMovie,
		Items:     []Item{{InScope: true, Want: want(wantSearching)}},
		Viewer:    requester(hdOnly),
		Now:       now,
	})

	if _, ok := find(res.Actions, ActionCancel); ok {
		t.Fatal("a viewer with no request of their own has nothing to withdraw")
	}
}

// A denied request is not live, so the viewer is back to asking. Appealing is a
// legitimate flow; the operator can decide again.
func TestADeniedRequestReturnsTheAskAffordance(t *testing.T) {
	t.Parallel()

	res := Derive(Input{
		MediaType: MediaTypeMovie,
		Items:     []Item{{InScope: false}},
		Request:   &Request{Status: requestDenied},
		Viewer:    requester(hdOnly), // IsRequester false: a denied request is not live
		Now:       now,
	})

	if res.State != StateDenied {
		t.Fatalf("precondition: state = %q, want denied", res.State)
	}
	if _, ok := find(res.Actions, ActionRequest); !ok {
		t.Fatal("a denied request must leave the viewer able to ask again")
	}
	if _, ok := find(res.Actions, ActionCancel); ok {
		t.Fatal("there is nothing live to withdraw after a denial")
	}
}

// An unauthenticated or grant-less read is a legitimate call, not an error.
func TestTheZeroViewerAffordsNothing(t *testing.T) {
	t.Parallel()

	res := Derive(Input{
		MediaType: MediaTypeMovie,
		Items:     []Item{{InScope: true, Want: want(wantSearching)}},
		Now:       now,
	})

	if len(res.Actions) != 0 {
		t.Fatalf("the zero viewer must afford nothing, got %v", res.Actions)
	}
}
