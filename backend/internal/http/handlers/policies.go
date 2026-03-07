package handlers

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/release"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/labstack/echo/v4"
)

type Policies struct{ svc *service.Services }

func NewPolicies(s *service.Services) *Policies { return &Policies{svc: s} }

func (h *Policies) RegisterProtected(v1 *echo.Group) {
	v1.GET("/policies", h.List)
	v1.POST("/policies", h.Create)
	v1.GET("/policies/full", h.ListFull)
	v1.GET("/policies/fields", h.GetFields)
	v1.GET("/policies/:id", h.Get)
	v1.PUT("/policies/:id", h.Update)
	v1.DELETE("/policies/:id", h.Delete)

	v1.GET("/policies/:id/rule", h.GetRule)
	v1.POST("/policies/:id/rule", h.CreateRule)
	v1.PUT("/policies/:id/rule", h.UpdateRule)
	v1.DELETE("/policies/:id/rule", h.DeleteRule)

	v1.GET("/policies/:id/actions", h.ListActions)
	v1.POST("/policies/:id/actions", h.CreateAction)
	v1.GET("/policies/:id/actions/:actionId", h.GetAction)
	v1.PUT("/policies/:id/actions/:actionId", h.UpdateAction)
	v1.DELETE("/policies/:id/actions/:actionId", h.DeleteAction)

	v1.POST("/policies/evaluate", h.Evaluate)
}

// PolicyCreateRequest payload
type PolicyCreateRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Enabled     bool    `json:"enabled"`
	Priority    int32   `json:"priority"`
}

// PolicyUpdateRequest payload
type PolicyUpdateRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Enabled     bool    `json:"enabled"`
	Priority    int32   `json:"priority"`
}

// RuleCreateRequest payload
type RuleCreateRequest struct {
	LeftOperand  string `json:"left_operand"`
	Operator     string `json:"operator"`
	RightOperand string `json:"right_operand"`
}

// RuleUpdateRequest payload
type RuleUpdateRequest struct {
	LeftOperand  string `json:"left_operand"`
	Operator     string `json:"operator"`
	RightOperand string `json:"right_operand"`
}

// ActionCreateRequest payload
type ActionCreateRequest struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	Order int32  `json:"order"`
}

// ActionUpdateRequest payload
type ActionUpdateRequest struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	Order int32  `json:"order"`
}

// FullPolicy is a policy with its rule and actions nested
type FullPolicy struct {
	dbgen.Policy
	Rule    *dbgen.Rule    `json:"rule"`
	Actions []dbgen.Action `json:"actions"`
}

// List policies
// @Summary List policies
// @Tags    policies
// @Produce json
// @Success 200 {array} dbgen.Policy
// @Router  /v1/policies [get]
func (h *Policies) List(c echo.Context) error {
	ctx := c.Request().Context()
	policies, err := h.svc.Policies.List(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list policies"})
	}
	return c.JSON(http.StatusOK, policies)
}

// ListFull returns all policies with nested rules and actions
// @Summary List full policies
// @Tags    policies
// @Produce json
// @Success 200 {array} handlers.FullPolicy
// @Router  /v1/policies/full [get]
func (h *Policies) ListFull(c echo.Context) error {
	ctx := c.Request().Context()
	policies, rules, actions, err := h.svc.Policies.ListFull(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list policies"})
	}

	// Index rules by policy_id
	ruleMap := make(map[string]*dbgen.Rule)
	for i := range rules {
		key := rules[i].PolicyID.Bytes
		ruleMap[string(key[:])] = &rules[i]
	}

	// Index actions by policy_id
	actionMap := make(map[string][]dbgen.Action)
	for _, a := range actions {
		key := string(a.PolicyID.Bytes[:])
		actionMap[key] = append(actionMap[key], a)
	}

	result := make([]FullPolicy, len(policies))
	for i, p := range policies {
		key := string(p.ID.Bytes[:])
		result[i] = FullPolicy{
			Policy:  p,
			Rule:    ruleMap[key],
			Actions: actionMap[key],
		}
		if result[i].Actions == nil {
			result[i].Actions = []dbgen.Action{}
		}
	}

	return c.JSON(http.StatusOK, result)
}

// Create policy
// @Summary Create policy
// @Tags    policies
// @Accept  json
// @Produce json
// @Param   payload body handlers.PolicyCreateRequest true "Create policy"
// @Success 201 {object} dbgen.Policy
// @Failure 400 {object} map[string]string
// @Router  /v1/policies [post]
func (h *Policies) Create(c echo.Context) error {
	var req PolicyCreateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	ctx := c.Request().Context()
	policy, err := h.svc.Policies.Create(ctx, req.Name, req.Description, req.Enabled, req.Priority)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, policy)
}

// Get policy
// @Summary Get policy
// @Tags    policies
// @Produce json
// @Success 200 {object} dbgen.Policy
// @Router  /v1/policies/{id} [get]
func (h *Policies) Get(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	policy, err := h.svc.Policies.Get(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return c.JSON(http.StatusOK, policy)
}

// Update policy
// @Summary Update policy
// @Tags    policies
// @Accept  json
// @Produce json
// @Param   id path string true "Policy ID"
// @Param   payload body handlers.PolicyUpdateRequest true "Update policy"
// @Success 200 {object} dbgen.Policy
// @Failure 400 {object} map[string]string
// @Router  /v1/policies/{id} [put]
func (h *Policies) Update(c echo.Context) error {
	var req PolicyUpdateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	policy, err := h.svc.Policies.Update(ctx, id, req.Name, req.Description, req.Enabled, req.Priority)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, policy)
}

// Delete policy
// @Summary Delete policy
// @Tags    policies
// @Param   id path string true "Policy ID"
// @Success 204 {string} string ""
// @Router  /v1/policies/{id} [delete]
func (h *Policies) Delete(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	if err := h.svc.Policies.Delete(ctx, id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete"})
	}
	return c.NoContent(http.StatusNoContent)
}

// Get rule for policy
// @Summary Get rule for policy
// @Tags    policies
// @Produce json
// @Success 200 {object} dbgen.Rule
// @Router  /v1/policies/{id}/rule [get]
func (h *Policies) GetRule(c echo.Context) error {
	var policyID pgtype.UUID
	if err := policyID.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	rule, err := h.svc.Policies.GetRule(ctx, policyID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return c.JSON(http.StatusOK, rule)
}

// Create rule for policy
// @Summary Create rule for policy
// @Tags    policies
// @Accept  json
// @Produce json
// @Param   id path string true "Policy ID"
// @Param   payload body handlers.RuleCreateRequest true "Create rule"
// @Success 201 {object} dbgen.Rule
// @Failure 400 {object} map[string]string
// @Router  /v1/policies/{id}/rule [post]
func (h *Policies) CreateRule(c echo.Context) error {
	var req RuleCreateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	var policyID pgtype.UUID
	if err := policyID.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	rule, err := h.svc.Policies.CreateRule(ctx, policyID, req.LeftOperand, req.Operator, req.RightOperand)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, rule)
}

// Update rule for policy
// @Summary Update rule for policy
// @Tags    policies
// @Accept  json
// @Produce json
// @Param   id path string true "Policy ID"
// @Param   payload body handlers.RuleUpdateRequest true "Update rule"
// @Success 200 {object} dbgen.Rule
// @Failure 400 {object} map[string]string
// @Router  /v1/policies/{id}/rule [put]
func (h *Policies) UpdateRule(c echo.Context) error {
	var req RuleUpdateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	var policyID pgtype.UUID
	if err := policyID.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	// Get existing rule to get its ID
	existingRule, err := h.svc.Policies.GetRule(ctx, policyID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "rule not found"})
	}
	rule, err := h.svc.Policies.UpdateRule(ctx, existingRule.ID, req.LeftOperand, req.Operator, req.RightOperand)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, rule)
}

// Delete rule for policy
// @Summary Delete rule for policy
// @Tags    policies
// @Param   id path string true "Policy ID"
// @Success 204 {string} string ""
// @Router  /v1/policies/{id}/rule [delete]
func (h *Policies) DeleteRule(c echo.Context) error {
	var policyID pgtype.UUID
	if err := policyID.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	rule, err := h.svc.Policies.GetRule(ctx, policyID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "rule not found"})
	}
	if err := h.svc.Policies.DeleteRule(ctx, rule.ID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete"})
	}
	return c.NoContent(http.StatusNoContent)
}

// List actions for policy
// @Summary List actions for policy
// @Tags    policies
// @Produce json
// @Success 200 {array} dbgen.Action
// @Router  /v1/policies/{id}/actions [get]
func (h *Policies) ListActions(c echo.Context) error {
	var policyID pgtype.UUID
	if err := policyID.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	actions, err := h.svc.Policies.ListActions(ctx, policyID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list actions"})
	}
	return c.JSON(http.StatusOK, actions)
}

// Create action for policy
// @Summary Create action for policy
// @Tags    policies
// @Accept  json
// @Produce json
// @Param   id path string true "Policy ID"
// @Param   payload body handlers.ActionCreateRequest true "Create action"
// @Success 201 {object} dbgen.Action
// @Failure 400 {object} map[string]string
// @Router  /v1/policies/{id}/actions [post]
func (h *Policies) CreateAction(c echo.Context) error {
	var req ActionCreateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	var policyID pgtype.UUID
	if err := policyID.Scan(c.Param("id")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	ctx := c.Request().Context()
	action, err := h.svc.Policies.CreateAction(ctx, policyID, req.Type, req.Value, req.Order)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, action)
}

// Get action
// @Summary Get action
// @Tags    policies
// @Produce json
// @Success 200 {object} dbgen.Action
// @Router  /v1/policies/{id}/actions/{actionId} [get]
func (h *Policies) GetAction(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("actionId")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid action id"})
	}
	ctx := c.Request().Context()
	action, err := h.svc.Policies.GetAction(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return c.JSON(http.StatusOK, action)
}

// Update action
// @Summary Update action
// @Tags    policies
// @Accept  json
// @Produce json
// @Param   id path string true "Policy ID"
// @Param   actionId path string true "Action ID"
// @Param   payload body handlers.ActionUpdateRequest true "Update action"
// @Success 200 {object} dbgen.Action
// @Failure 400 {object} map[string]string
// @Router  /v1/policies/{id}/actions/{actionId} [put]
func (h *Policies) UpdateAction(c echo.Context) error {
	var req ActionUpdateRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	var id pgtype.UUID
	if err := id.Scan(c.Param("actionId")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid action id"})
	}
	ctx := c.Request().Context()
	action, err := h.svc.Policies.UpdateAction(ctx, id, req.Type, req.Value, req.Order)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, action)
}

// Delete action
// @Summary Delete action
// @Tags    policies
// @Param   id path string true "Policy ID"
// @Param   actionId path string true "Action ID"
// @Success 204 {string} string ""
// @Router  /v1/policies/{id}/actions/{actionId} [delete]
func (h *Policies) DeleteAction(c echo.Context) error {
	var id pgtype.UUID
	if err := id.Scan(c.Param("actionId")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid action id"})
	}
	ctx := c.Request().Context()
	if err := h.svc.Policies.DeleteAction(ctx, id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete"})
	}
	return c.NoContent(http.StatusNoContent)
}

// GetFields returns all available field definitions for policy rules
// @Summary Get field definitions
// @Tags    policies
// @Produce json
// @Success 200 {array} model.FieldDefinition
// @Router  /v1/policies/fields [get]
func (h *Policies) GetFields(c echo.Context) error {
	ctx := c.Request().Context()
	fields, err := h.svc.Policies.GetFieldDefinitions(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, fields)
}

// Evaluate policies
// @Summary Evaluate policies against torrent metadata
// @Tags    policies
// @Accept  json
// @Produce json
// @Param   payload body model.DownloadCandidate true "Download candidate"
// @Success 200 {object} model.EvaluationTrace
// @Failure 400 {object} map[string]string
// @Router  /v1/policies/evaluate [post]
func (h *Policies) Evaluate(c echo.Context) error {
	var req model.DownloadCandidate
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	// Convert DownloadCandidate to EvaluationContext
	q := release.Parse(req.Title)
	evalCtx := model.NewEvaluationContext(req, q)

	ctx := c.Request().Context()
	trace, err := h.svc.Policies.Evaluate(ctx, evalCtx)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, trace)
}
