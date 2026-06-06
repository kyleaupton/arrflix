package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/routing"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Routing struct{ svc *service.Services }

func NewRouting(s *service.Services) *Routing { return &Routing{svc: s} }

// ----- Shared body shapes -----

// routingRuleWriteBody is the create/update shape. Conditions rides as the
// canonical rules-substrate JSON tree — the same encoding stored in the JSONB
// column and walked by the evaluator. It is carried opaquely here (the tree
// type is recursive) and decoded + type-checked against the field registry in
// the service, which rejects ill-typed trees as 422s.
type routingRuleWriteBody struct {
	Name           string          `json:"name" required:"true" minLength:"1" doc:"Display name"`
	Enabled        bool            `json:"enabled" doc:"Whether the rule participates in dispatch"`
	Conditions     json.RawMessage `json:"conditions" required:"true" doc:"Condition tree (canonical rules JSON: {op, left, right} leaves composed by and/or/not)"`
	DownloaderID   *uuid.UUID      `json:"downloaderId,omitempty" format:"uuid" doc:"Downloader applied on match"`
	LibraryID      *uuid.UUID      `json:"libraryId,omitempty" format:"uuid" doc:"Library applied on match"`
	NameTemplateID *uuid.UUID      `json:"nameTemplateId,omitempty" format:"uuid" doc:"Name template applied on match"`
	Continue       bool            `json:"continue" doc:"Keep evaluating later rules after this one matches (they fill only unset slots)"`
}

func (b routingRuleWriteBody) toInput() service.RoutingRuleInput {
	return service.RoutingRuleInput{
		Name:           b.Name,
		Enabled:        b.Enabled,
		Conditions:     b.Conditions,
		DownloaderID:   b.DownloaderID,
		LibraryID:      b.LibraryID,
		NameTemplateID: b.NameTemplateID,
		Continue:       b.Continue,
	}
}

// ----- List -----

type RoutingListRulesInput struct{}

type RoutingListRulesOutput struct {
	Body []model.RoutingRule
}

func (h *Routing) ListRules(ctx context.Context, _ *RoutingListRulesInput) (*RoutingListRulesOutput, error) {
	out, err := h.svc.Routing.List(ctx)
	if err != nil {
		return nil, err
	}
	return &RoutingListRulesOutput{Body: out}, nil
}

// ----- Get -----

type RoutingGetRuleInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Routing rule ID"`
}

type RoutingGetRuleOutput struct {
	Body model.RoutingRule
}

func (h *Routing) GetRule(ctx context.Context, input *RoutingGetRuleInput) (*RoutingGetRuleOutput, error) {
	out, err := h.svc.Routing.Get(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &RoutingGetRuleOutput{Body: out}, nil
}

// ----- Create -----

type RoutingCreateRuleInput struct {
	Body routingRuleWriteBody
}

type RoutingCreateRuleOutput struct {
	Body model.RoutingRule
}

func (h *Routing) CreateRule(ctx context.Context, input *RoutingCreateRuleInput) (*RoutingCreateRuleOutput, error) {
	out, err := h.svc.Routing.Create(ctx, input.Body.toInput())
	if err != nil {
		return nil, err
	}
	return &RoutingCreateRuleOutput{Body: out}, nil
}

// ----- Update -----

type RoutingUpdateRuleInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Routing rule ID"`
	Body routingRuleWriteBody
}

type RoutingUpdateRuleOutput struct {
	Body model.RoutingRule
}

func (h *Routing) UpdateRule(ctx context.Context, input *RoutingUpdateRuleInput) (*RoutingUpdateRuleOutput, error) {
	out, err := h.svc.Routing.Update(ctx, input.ID, input.Body.toInput())
	if err != nil {
		return nil, err
	}
	return &RoutingUpdateRuleOutput{Body: out}, nil
}

// ----- Delete -----

type RoutingDeleteRuleInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Routing rule ID"`
}

type RoutingDeleteRuleOutput struct{}

func (h *Routing) DeleteRule(ctx context.Context, input *RoutingDeleteRuleInput) (*RoutingDeleteRuleOutput, error) {
	if err := h.svc.Routing.Delete(ctx, input.ID); err != nil {
		return nil, err
	}
	return &RoutingDeleteRuleOutput{}, nil
}

// ----- Reorder -----

type routingReorderBody struct {
	IDs []uuid.UUID `json:"ids" required:"true" doc:"Every rule id in the desired evaluation order"`
}

type RoutingReorderRulesInput struct {
	Body routingReorderBody
}

type RoutingReorderRulesOutput struct {
	Body []model.RoutingRule
}

func (h *Routing) ReorderRules(ctx context.Context, input *RoutingReorderRulesInput) (*RoutingReorderRulesOutput, error) {
	out, err := h.svc.Routing.Reorder(ctx, input.Body.IDs)
	if err != nil {
		return nil, err
	}
	return &RoutingReorderRulesOutput{Body: out}, nil
}

// ----- GetFields -----

type RoutingGetFieldsInput struct{}

type RoutingGetFieldsOutput struct {
	Body []model.FieldDefinition
}

func (h *Routing) GetFields(ctx context.Context, _ *RoutingGetFieldsInput) (*RoutingGetFieldsOutput, error) {
	out, err := h.svc.Routing.GetFieldDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	return &RoutingGetFieldsOutput{Body: out}, nil
}

// ----- Evaluate -----

// routingEvaluateBody is the dry-run request shape: the raw candidate plus
// the explicit media type the caller wants the rules evaluated against.
// Parse requires a domain, so the caller has to commit to one. Evaluation
// always runs at the grab moment — routing's moment; per-moment preview is a
// later editor concern.
type routingEvaluateBody struct {
	MediaType string                  `json:"mediaType" required:"true" enum:"movie,series" doc:"Media type to evaluate against (movie or series). Required — parsing is domain-dispatched."`
	Candidate model.DownloadCandidate `json:"candidate" doc:"The download candidate to evaluate"`
}

type RoutingEvaluateInput struct {
	Body routingEvaluateBody
}

type RoutingEvaluateOutput struct {
	Body routing.Evaluation
}

func (h *Routing) Evaluate(ctx context.Context, input *RoutingEvaluateInput) (*RoutingEvaluateOutput, error) {
	// Map the explicit media type onto a parsing domain. The enum tag on the
	// body field rejects anything other than movie/series before we get here;
	// this switch is the belt to that suspenders, surfacing the same 422 with
	// an explicit detail message if a future code path bypasses the tag.
	var domain parsing.Domain
	switch model.MediaType(input.Body.MediaType) {
	case model.MediaTypeMovie:
		domain = parsing.DomainMovie
	case model.MediaTypeSeries:
		domain = parsing.DomainSeries
	default:
		return nil, apperrors.Validation("media type is required to evaluate routing rules — pick movie or series",
			apperrors.Field("body.mediaType", "must be 'movie' or 'series'"),
		).Op("RoutingHandler.Evaluate")
	}

	q := parsing.Parse(input.Body.Candidate.Title, domain)
	subject := model.NewSubject(input.Body.Candidate, q)

	_, eval, err := h.svc.Routing.Dispatch(ctx, subject)
	if err != nil {
		return nil, err
	}
	return &RoutingEvaluateOutput{Body: eval}, nil
}

// ----- Register -----

func (h *Routing) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "routing-list-rules",
		Method:      http.MethodGet,
		Path:        "/api/v1/routing/rules",
		Summary:     "List routing rules",
		Tags:        []string{"routing"},
	}, h.ListRules)

	huma.Register(api, huma.Operation{
		OperationID:   "routing-create-rule",
		Method:        http.MethodPost,
		Path:          "/api/v1/routing/rules",
		Summary:       "Create routing rule",
		Description:   "New rules append to the end of the evaluation order; use the positions endpoint to reorder.",
		Tags:          []string{"routing"},
		DefaultStatus: http.StatusCreated,
		Errors:        errsWrite,
	}, h.CreateRule)

	huma.Register(api, huma.Operation{
		OperationID: "routing-reorder-rules",
		Method:      http.MethodPut,
		Path:        "/api/v1/routing/rules/positions",
		Summary:     "Reorder routing rules",
		Description: "Takes the full ordered id list — reordering is a list-level operation.",
		Tags:        []string{"routing"},
		Errors:      errsWrite,
	}, h.ReorderRules)

	huma.Register(api, huma.Operation{
		OperationID: "routing-get-rule",
		Method:      http.MethodGet,
		Path:        "/api/v1/routing/rules/{id}",
		Summary:     "Get routing rule",
		Tags:        []string{"routing"},
		Errors:      errsRead,
	}, h.GetRule)

	huma.Register(api, huma.Operation{
		OperationID: "routing-update-rule",
		Method:      http.MethodPut,
		Path:        "/api/v1/routing/rules/{id}",
		Summary:     "Update routing rule",
		Tags:        []string{"routing"},
		Errors:      errsUpsert,
	}, h.UpdateRule)

	huma.Register(api, huma.Operation{
		OperationID:   "routing-delete-rule",
		Method:        http.MethodDelete,
		Path:          "/api/v1/routing/rules/{id}",
		Summary:       "Delete routing rule",
		Tags:          []string{"routing"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsRead,
	}, h.DeleteRule)

	huma.Register(api, huma.Operation{
		OperationID: "routing-get-fields",
		Method:      http.MethodGet,
		Path:        "/api/v1/routing/fields",
		Summary:     "Get routing field definitions",
		Tags:        []string{"routing"},
	}, h.GetFields)

	huma.Register(api, huma.Operation{
		OperationID: "routing-evaluate",
		Method:      http.MethodPost,
		Path:        "/api/v1/routing/evaluate",
		Summary:     "Dry-run routing rules against a download candidate",
		Description: "Evaluates at the grab moment and returns the per-rule tri-state dispatch record.",
		Tags:        []string{"routing"},
		Errors:      errsWrite,
	}, h.Evaluate)
}
