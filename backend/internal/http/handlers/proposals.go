package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

// Proposals is the operator-facing surface over acquisition proposals — the
// picks a propose-segment want parked for confirmation. Reads are scoped per
// tracking (surfaced in-context on the series page, not a global inbox); the two
// writes resolve a proposal by approving (grab) or declining (exclude + re-arm).
type Proposals struct{ svc *service.Services }

func NewProposals(s *service.Services) *Proposals { return &Proposals{svc: s} }

// ----- List by tracking -----

type ProposalsListInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Tracking ID"`
}

type ProposalsListOutput struct {
	Body []model.ProposalView
}

func (h *Proposals) ListForTracking(ctx context.Context, input *ProposalsListInput) (*ProposalsListOutput, error) {
	out, err := h.svc.Proposals.ListForTracking(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ProposalsListOutput{Body: out}, nil
}

// ----- Approve -----

type ProposalsApproveInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Proposal ID"`
}

type ProposalsResolveOutput struct {
	Body []model.Want
}

func (h *Proposals) Approve(ctx context.Context, input *ProposalsApproveInput) (*ProposalsResolveOutput, error) {
	out, err := h.svc.Proposals.Approve(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ProposalsResolveOutput{Body: out}, nil
}

// ----- Decline -----

type ProposalsDeclineInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Proposal ID"`
}

func (h *Proposals) Decline(ctx context.Context, input *ProposalsDeclineInput) (*ProposalsResolveOutput, error) {
	out, err := h.svc.Proposals.Decline(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &ProposalsResolveOutput{Body: out}, nil
}

// ----- Register -----

func (h *Proposals) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "tracking-proposals",
		Method:      http.MethodGet,
		Path:        "/api/v1/tracking/{id}/proposals",
		Summary:     "List a tracking's proposals",
		Description: "Returns the tracking's open proposals (picks parked for confirmation), each with the episode and want ids it covers.",
		Tags:        []string{"tracking"},
		Errors:      errsRead,
	}, h.ListForTracking)

	huma.Register(api, huma.Operation{
		OperationID: "proposals-approve",
		Method:      http.MethodPost,
		Path:        "/api/v1/proposals/{id}/approve",
		Summary:     "Approve a proposal",
		Description: "Grabs the proposed release and clears the proposal. 404 if already resolved; 409 if the covered wants are no longer grabbable (a stale pack or a declined-then-approved race).",
		Tags:        []string{"tracking"},
		Errors:      errsUpsert,
	}, h.Approve)

	huma.Register(api, huma.Operation{
		OperationID: "proposals-decline",
		Method:      http.MethodPost,
		Path:        "/api/v1/proposals/{id}/decline",
		Summary:     "Decline a proposal",
		Description: "Excludes the proposed release for every covered want and re-arms them so the next search proposes a different release. 404 if already resolved.",
		Tags:        []string{"tracking"},
		Errors:      errsUpsert,
	}, h.Decline)
}
