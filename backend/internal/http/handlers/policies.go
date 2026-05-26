package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type Policies struct{ svc *service.Services }

func NewPolicies(s *service.Services) *Policies { return &Policies{svc: s} }

// ----- Shared body shapes -----

type policyWriteBody struct {
	Name        string  `json:"name" required:"true" minLength:"1" doc:"Display name"`
	Description *string `json:"description,omitempty" doc:"Optional description"`
	Enabled     bool    `json:"enabled" doc:"Whether the policy is active"`
	Priority    int32   `json:"priority" doc:"Evaluation priority (higher runs first)"`
}

type ruleWriteBody struct {
	LeftOperand  string `json:"leftOperand" required:"true" doc:"Left operand of the rule"`
	Operator     string `json:"operator" required:"true" minLength:"1" doc:"Operator"`
	RightOperand string `json:"rightOperand" required:"true" doc:"Right operand"`
}

type actionWriteBody struct {
	Type  string `json:"type" required:"true" minLength:"1" doc:"Action type"`
	Value string `json:"value" doc:"Action value"`
	Order int32  `json:"order" doc:"Order within the policy's action list"`
}

// ----- List -----

type PoliciesListInput struct{}

type PoliciesListOutput struct {
	Body []model.Policy
}

func (h *Policies) List(ctx context.Context, _ *PoliciesListInput) (*PoliciesListOutput, error) {
	out, err := h.svc.Policies.List(ctx)
	if err != nil {
		return nil, err
	}
	return &PoliciesListOutput{Body: out}, nil
}

// ----- Get -----

type PoliciesGetInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Policy ID"`
}

type PoliciesGetOutput struct {
	Body model.Policy
}

func (h *Policies) Get(ctx context.Context, input *PoliciesGetInput) (*PoliciesGetOutput, error) {
	out, err := h.svc.Policies.Get(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &PoliciesGetOutput{Body: out}, nil
}

// ----- Create -----

type PoliciesCreateInput struct {
	Body policyWriteBody
}

type PoliciesCreateOutput struct {
	Body model.Policy
}

func (h *Policies) Create(ctx context.Context, input *PoliciesCreateInput) (*PoliciesCreateOutput, error) {
	out, err := h.svc.Policies.Create(ctx, input.Body.Name, input.Body.Description, input.Body.Enabled, input.Body.Priority)
	if err != nil {
		return nil, err
	}
	return &PoliciesCreateOutput{Body: out}, nil
}

// ----- Update -----

type PoliciesUpdateInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Policy ID"`
	Body policyWriteBody
}

type PoliciesUpdateOutput struct {
	Body model.Policy
}

func (h *Policies) Update(ctx context.Context, input *PoliciesUpdateInput) (*PoliciesUpdateOutput, error) {
	out, err := h.svc.Policies.Update(ctx, input.ID, input.Body.Name, input.Body.Description, input.Body.Enabled, input.Body.Priority)
	if err != nil {
		return nil, err
	}
	return &PoliciesUpdateOutput{Body: out}, nil
}

// ----- Delete -----

type PoliciesDeleteInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Policy ID"`
}

type PoliciesDeleteOutput struct{}

func (h *Policies) Delete(ctx context.Context, input *PoliciesDeleteInput) (*PoliciesDeleteOutput, error) {
	if err := h.svc.Policies.Delete(ctx, input.ID); err != nil {
		return nil, err
	}
	return &PoliciesDeleteOutput{}, nil
}

// ----- GetRule -----

type PoliciesGetRuleInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Policy ID"`
}

type PoliciesGetRuleOutput struct {
	Body model.Rule
}

func (h *Policies) GetRule(ctx context.Context, input *PoliciesGetRuleInput) (*PoliciesGetRuleOutput, error) {
	out, err := h.svc.Policies.GetRule(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &PoliciesGetRuleOutput{Body: out}, nil
}

// ----- CreateRule -----

type PoliciesCreateRuleInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Policy ID"`
	Body ruleWriteBody
}

type PoliciesCreateRuleOutput struct {
	Body model.Rule
}

func (h *Policies) CreateRule(ctx context.Context, input *PoliciesCreateRuleInput) (*PoliciesCreateRuleOutput, error) {
	out, err := h.svc.Policies.CreateRule(ctx, input.ID, input.Body.LeftOperand, input.Body.Operator, input.Body.RightOperand)
	if err != nil {
		return nil, err
	}
	return &PoliciesCreateRuleOutput{Body: out}, nil
}

// ----- UpdateRule -----

type PoliciesUpdateRuleInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Policy ID"`
	Body ruleWriteBody
}

type PoliciesUpdateRuleOutput struct {
	Body model.Rule
}

func (h *Policies) UpdateRule(ctx context.Context, input *PoliciesUpdateRuleInput) (*PoliciesUpdateRuleOutput, error) {
	existingRule, err := h.svc.Policies.GetRule(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	out, err := h.svc.Policies.UpdateRule(ctx, existingRule.ID, input.Body.LeftOperand, input.Body.Operator, input.Body.RightOperand)
	if err != nil {
		return nil, err
	}
	return &PoliciesUpdateRuleOutput{Body: out}, nil
}

// ----- DeleteRule -----

type PoliciesDeleteRuleInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Policy ID"`
}

type PoliciesDeleteRuleOutput struct{}

func (h *Policies) DeleteRule(ctx context.Context, input *PoliciesDeleteRuleInput) (*PoliciesDeleteRuleOutput, error) {
	rule, err := h.svc.Policies.GetRule(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Policies.DeleteRule(ctx, rule.ID); err != nil {
		return nil, err
	}
	return &PoliciesDeleteRuleOutput{}, nil
}

// ----- ListActions -----

type PoliciesListActionsInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Policy ID"`
}

type PoliciesListActionsOutput struct {
	Body []model.Action
}

func (h *Policies) ListActions(ctx context.Context, input *PoliciesListActionsInput) (*PoliciesListActionsOutput, error) {
	out, err := h.svc.Policies.ListActions(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &PoliciesListActionsOutput{Body: out}, nil
}

// ----- CreateAction -----

type PoliciesCreateActionInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Policy ID"`
	Body actionWriteBody
}

type PoliciesCreateActionOutput struct {
	Body model.Action
}

func (h *Policies) CreateAction(ctx context.Context, input *PoliciesCreateActionInput) (*PoliciesCreateActionOutput, error) {
	out, err := h.svc.Policies.CreateAction(ctx, input.ID, input.Body.Type, input.Body.Value, input.Body.Order)
	if err != nil {
		return nil, err
	}
	return &PoliciesCreateActionOutput{Body: out}, nil
}

// ----- GetAction -----

type PoliciesGetActionInput struct {
	ID       uuid.UUID `path:"id" format:"uuid" doc:"Policy ID"`
	ActionID uuid.UUID `path:"actionId" format:"uuid" doc:"Action ID"`
}

type PoliciesGetActionOutput struct {
	Body model.Action
}

func (h *Policies) GetAction(ctx context.Context, input *PoliciesGetActionInput) (*PoliciesGetActionOutput, error) {
	out, err := h.svc.Policies.GetAction(ctx, input.ActionID)
	if err != nil {
		return nil, err
	}
	return &PoliciesGetActionOutput{Body: out}, nil
}

// ----- UpdateAction -----

type PoliciesUpdateActionInput struct {
	ID       uuid.UUID `path:"id" format:"uuid" doc:"Policy ID"`
	ActionID uuid.UUID `path:"actionId" format:"uuid" doc:"Action ID"`
	Body     actionWriteBody
}

type PoliciesUpdateActionOutput struct {
	Body model.Action
}

func (h *Policies) UpdateAction(ctx context.Context, input *PoliciesUpdateActionInput) (*PoliciesUpdateActionOutput, error) {
	out, err := h.svc.Policies.UpdateAction(ctx, input.ActionID, input.Body.Type, input.Body.Value, input.Body.Order)
	if err != nil {
		return nil, err
	}
	return &PoliciesUpdateActionOutput{Body: out}, nil
}

// ----- DeleteAction -----

type PoliciesDeleteActionInput struct {
	ID       uuid.UUID `path:"id" format:"uuid" doc:"Policy ID"`
	ActionID uuid.UUID `path:"actionId" format:"uuid" doc:"Action ID"`
}

type PoliciesDeleteActionOutput struct{}

func (h *Policies) DeleteAction(ctx context.Context, input *PoliciesDeleteActionInput) (*PoliciesDeleteActionOutput, error) {
	if err := h.svc.Policies.DeleteAction(ctx, input.ActionID); err != nil {
		return nil, err
	}
	return &PoliciesDeleteActionOutput{}, nil
}

// ----- GetFields -----

type PoliciesGetFieldsInput struct{}

type PoliciesGetFieldsOutput struct {
	Body []model.FieldDefinition
}

func (h *Policies) GetFields(ctx context.Context, _ *PoliciesGetFieldsInput) (*PoliciesGetFieldsOutput, error) {
	out, err := h.svc.Policies.GetFieldDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	return &PoliciesGetFieldsOutput{Body: out}, nil
}

// ----- Evaluate -----

type PoliciesEvaluateInput struct {
	Body model.DownloadCandidate
}

type PoliciesEvaluateOutput struct {
	Body model.EvaluationTrace
}

func (h *Policies) Evaluate(ctx context.Context, input *PoliciesEvaluateInput) (*PoliciesEvaluateOutput, error) {
	q := parsing.Parse(input.Body.Title)
	evalCtx := model.NewEvaluationContext(input.Body, q)

	trace, err := h.svc.Policies.Evaluate(ctx, evalCtx)
	if err != nil {
		return nil, err
	}
	return &PoliciesEvaluateOutput{Body: trace}, nil
}

// ----- Register -----

func (h *Policies) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "policies-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/policies",
		Summary:     "List policies",
		Tags:        []string{"policies"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID:   "policies-create",
		Method:        http.MethodPost,
		Path:          "/api/v1/policies",
		Summary:       "Create policy",
		Tags:          []string{"policies"},
		DefaultStatus: http.StatusCreated,
		Errors:        errsWrite,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "policies-get-fields",
		Method:      http.MethodGet,
		Path:        "/api/v1/policies/fields",
		Summary:     "Get policy field definitions",
		Tags:        []string{"policies"},
	}, h.GetFields)

	huma.Register(api, huma.Operation{
		OperationID: "policies-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/policies/{id}",
		Summary:     "Get policy",
		Tags:        []string{"policies"},
		Errors:      errsRead,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "policies-update",
		Method:      http.MethodPut,
		Path:        "/api/v1/policies/{id}",
		Summary:     "Update policy",
		Tags:        []string{"policies"},
		Errors:      errsUpsert,
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID:   "policies-delete",
		Method:        http.MethodDelete,
		Path:          "/api/v1/policies/{id}",
		Summary:       "Delete policy",
		Tags:          []string{"policies"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsRead,
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID: "policies-get-rule",
		Method:      http.MethodGet,
		Path:        "/api/v1/policies/{id}/rule",
		Summary:     "Get rule for policy",
		Tags:        []string{"policies"},
		Errors:      errsRead,
	}, h.GetRule)

	huma.Register(api, huma.Operation{
		OperationID:   "policies-create-rule",
		Method:        http.MethodPost,
		Path:          "/api/v1/policies/{id}/rule",
		Summary:       "Create rule for policy",
		Tags:          []string{"policies"},
		DefaultStatus: http.StatusCreated,
		Errors:        errsUpsert,
	}, h.CreateRule)

	huma.Register(api, huma.Operation{
		OperationID: "policies-update-rule",
		Method:      http.MethodPut,
		Path:        "/api/v1/policies/{id}/rule",
		Summary:     "Update rule for policy",
		Tags:        []string{"policies"},
		Errors:      errsUpsert,
	}, h.UpdateRule)

	huma.Register(api, huma.Operation{
		OperationID:   "policies-delete-rule",
		Method:        http.MethodDelete,
		Path:          "/api/v1/policies/{id}/rule",
		Summary:       "Delete rule for policy",
		Tags:          []string{"policies"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsRead,
	}, h.DeleteRule)

	huma.Register(api, huma.Operation{
		OperationID: "policies-list-actions",
		Method:      http.MethodGet,
		Path:        "/api/v1/policies/{id}/actions",
		Summary:     "List actions for policy",
		Tags:        []string{"policies"},
		Errors:      errsRead,
	}, h.ListActions)

	huma.Register(api, huma.Operation{
		OperationID:   "policies-create-action",
		Method:        http.MethodPost,
		Path:          "/api/v1/policies/{id}/actions",
		Summary:       "Create action for policy",
		Tags:          []string{"policies"},
		DefaultStatus: http.StatusCreated,
		Errors:        errsUpsert,
	}, h.CreateAction)

	huma.Register(api, huma.Operation{
		OperationID: "policies-get-action",
		Method:      http.MethodGet,
		Path:        "/api/v1/policies/{id}/actions/{actionId}",
		Summary:     "Get action",
		Tags:        []string{"policies"},
		Errors:      errsRead,
	}, h.GetAction)

	huma.Register(api, huma.Operation{
		OperationID: "policies-update-action",
		Method:      http.MethodPut,
		Path:        "/api/v1/policies/{id}/actions/{actionId}",
		Summary:     "Update action",
		Tags:        []string{"policies"},
		Errors:      errsUpsert,
	}, h.UpdateAction)

	huma.Register(api, huma.Operation{
		OperationID:   "policies-delete-action",
		Method:        http.MethodDelete,
		Path:          "/api/v1/policies/{id}/actions/{actionId}",
		Summary:       "Delete action",
		Tags:          []string{"policies"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errsRead,
	}, h.DeleteAction)

	huma.Register(api, huma.Operation{
		OperationID: "policies-evaluate",
		Method:      http.MethodPost,
		Path:        "/api/v1/policies/evaluate",
		Summary:     "Evaluate policies against a download candidate",
		Tags:        []string{"policies"},
		Errors:      errsWrite,
	}, h.Evaluate)
}
