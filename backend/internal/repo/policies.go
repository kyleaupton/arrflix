package repo

import (
	"context"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type PolicyRepo interface {
	ListPolicies(ctx context.Context) ([]model.Policy, error)
	GetPolicy(ctx context.Context, id uuid.UUID) (model.Policy, error)
	CreatePolicy(ctx context.Context, params CreatePolicyParams) (model.Policy, error)
	UpdatePolicy(ctx context.Context, params UpdatePolicyParams) (model.Policy, error)
	DeletePolicy(ctx context.Context, id uuid.UUID) error
	UpdatePolicyPriority(ctx context.Context, id uuid.UUID, priority int32) error

	GetRuleForPolicy(ctx context.Context, policyID uuid.UUID) (model.Rule, error)
	GetRuleByID(ctx context.Context, id uuid.UUID) (model.Rule, error)
	CreateRule(ctx context.Context, params CreateRuleParams) (model.Rule, error)
	UpdateRule(ctx context.Context, params UpdateRuleParams) (model.Rule, error)
	DeleteRule(ctx context.Context, id uuid.UUID) error
	DeleteRuleForPolicy(ctx context.Context, policyID uuid.UUID) error

	ListActionsForPolicy(ctx context.Context, policyID uuid.UUID) ([]model.Action, error)
	GetAction(ctx context.Context, id uuid.UUID) (model.Action, error)
	CreateAction(ctx context.Context, params CreateActionParams) (model.Action, error)
	UpdateAction(ctx context.Context, params UpdateActionParams) (model.Action, error)
	DeleteAction(ctx context.Context, id uuid.UUID) error
}

// CreatePolicyParams is the domain-shaped input for CreatePolicy. Mirrors the
// writeable subset of model.Policy (omits server-managed ID/CreatedAt/UpdatedAt
// and the composed Rule/Actions which are managed via their own endpoints).
type CreatePolicyParams struct {
	Name        string
	Description *string
	Enabled     bool
	Priority    int32
}

// UpdatePolicyParams is the domain-shaped input for UpdatePolicy. Mirrors the
// writeable subset of model.Policy plus the target ID.
type UpdatePolicyParams struct {
	ID          uuid.UUID
	Name        string
	Description *string
	Enabled     bool
	Priority    int32
}

// CreateRuleParams is the domain-shaped input for CreateRule. Mirrors the
// writeable subset of model.Rule (omits server-managed ID/CreatedAt/UpdatedAt).
type CreateRuleParams struct {
	PolicyID     uuid.UUID
	LeftOperand  string
	Operator     string
	RightOperand string
}

// UpdateRuleParams is the domain-shaped input for UpdateRule. Mirrors the
// writeable subset of model.Rule plus the target ID. PolicyID is not included
// because rules cannot be reparented.
type UpdateRuleParams struct {
	ID           uuid.UUID
	LeftOperand  string
	Operator     string
	RightOperand string
}

// CreateActionParams is the domain-shaped input for CreateAction. Mirrors the
// writeable subset of model.Action (omits server-managed ID/CreatedAt/UpdatedAt).
type CreateActionParams struct {
	PolicyID uuid.UUID
	Type     string
	Value    string
	Order    int32
}

// UpdateActionParams is the domain-shaped input for UpdateAction. Mirrors the
// writeable subset of model.Action plus the target ID. PolicyID is not included
// because actions cannot be reparented.
type UpdateActionParams struct {
	ID    uuid.UUID
	Type  string
	Value string
	Order int32
}

// toModelRule translates the persistence-shaped dbgen.Rule into the
// domain-shaped model.Rule.
func toModelRule(row dbgen.Rule) model.Rule {
	return model.Rule{
		ID:           uuidFromPgtype(row.ID),
		PolicyID:     uuidFromPgtype(row.PolicyID),
		LeftOperand:  row.LeftOperand,
		Operator:     row.Operator,
		RightOperand: row.RightOperand,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

// toModelAction translates the persistence-shaped dbgen.Action into the
// domain-shaped model.Action.
func toModelAction(row dbgen.Action) model.Action {
	return model.Action{
		ID:        uuidFromPgtype(row.ID),
		PolicyID:  uuidFromPgtype(row.PolicyID),
		Type:      row.Type,
		Value:     row.Value,
		Order:     row.Order,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

// policyFromDbgen composes a flat dbgen.Policy row plus an optional rule plus
// an actions slice into the rich model.Policy. Server-managed fields are
// preserved; the actions slice is always non-nil so the wire format ships
// `"actions": []` rather than `"actions": null`.
func policyFromDbgen(row dbgen.Policy, rule *model.Rule, actions []model.Action) model.Policy {
	if actions == nil {
		actions = []model.Action{}
	}
	return model.Policy{
		ID:          uuidFromPgtype(row.ID),
		Name:        row.Name,
		Description: row.Description,
		Enabled:     row.Enabled,
		Priority:    row.Priority,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		Rule:        rule,
		Actions:     actions,
	}
}

// ListPolicies returns all policies with their rule + actions composed in.
//
// Composition strategy: bulk-fetch — one query for policies, one for all rules,
// one for all actions, then group in Go by policy ID. Avoids the N+1 that a
// per-policy fetch would produce on the policy management UI.
func (r *Repository) ListPolicies(ctx context.Context) ([]model.Policy, error) {
	rows, err := r.Q.ListPolicies(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list policies")
	}
	if len(rows) == 0 {
		return []model.Policy{}, nil
	}

	ruleRows, err := r.Q.ListAllRules(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list policy rules")
	}
	actionRows, err := r.Q.ListAllActions(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list policy actions")
	}

	// Index rules by policy_id (single-row policy → rule mapping; the schema
	// permits multiple rows per policy_id but the application contract is one,
	// matching the FullPolicy shim's prior behavior of taking the last row).
	ruleByPolicy := make(map[uuid.UUID]*model.Rule, len(ruleRows))
	for _, rr := range ruleRows {
		m := toModelRule(rr)
		ruleByPolicy[m.PolicyID] = &m
	}

	// Index actions by policy_id; actionRows are already ordered by policy_id, "order".
	actionsByPolicy := make(map[uuid.UUID][]model.Action, len(rows))
	for _, ar := range actionRows {
		m := toModelAction(ar)
		actionsByPolicy[m.PolicyID] = append(actionsByPolicy[m.PolicyID], m)
	}

	out := make([]model.Policy, 0, len(rows))
	for _, p := range rows {
		id := uuidFromPgtype(p.ID)
		out = append(out, policyFromDbgen(p, ruleByPolicy[id], actionsByPolicy[id]))
	}
	return out, nil
}

// GetPolicy fetches a single policy with its rule and actions composed in.
// Issues three queries (policy, rule-for-policy, actions-for-policy) — fine
// for a single-row read.
func (r *Repository) GetPolicy(ctx context.Context, id uuid.UUID) (model.Policy, error) {
	row, err := r.Q.GetPolicy(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.Policy{}, apperrors.FromPg(err, "policy %s not found", id)
	}

	// Rule is optional — a policy without a rule is valid (just doesn't match
	// anything). NotFound from this lookup is benign; any other error bubbles.
	var rulePtr *model.Rule
	ruleRow, ruleErr := r.Q.GetRuleForPolicy(ctx, pgtypeFromUUID(id))
	if ruleErr == nil {
		m := toModelRule(ruleRow)
		rulePtr = &m
	} else if e := apperrors.FromPg(ruleErr, "rule for policy %s not found", id); e != nil && !apperrors.IsNotFound(e) {
		return model.Policy{}, e
	}

	actionRows, err := r.Q.ListActionsForPolicy(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.Policy{}, apperrors.FromPg(err, "list actions for policy %s", id)
	}
	actions := make([]model.Action, 0, len(actionRows))
	for _, ar := range actionRows {
		actions = append(actions, toModelAction(ar))
	}

	return policyFromDbgen(row, rulePtr, actions), nil
}

func (r *Repository) CreatePolicy(ctx context.Context, params CreatePolicyParams) (model.Policy, error) {
	row, err := r.Q.CreatePolicy(ctx, dbgen.CreatePolicyParams{
		Name:        params.Name,
		Description: params.Description,
		Enabled:     params.Enabled,
		Priority:    params.Priority,
	})
	if err != nil {
		return model.Policy{}, apperrors.FromPg(err, "create policy %q", params.Name)
	}
	// Newly created policy has no rule and no actions yet.
	return policyFromDbgen(row, nil, nil), nil
}

func (r *Repository) UpdatePolicy(ctx context.Context, params UpdatePolicyParams) (model.Policy, error) {
	row, err := r.Q.UpdatePolicy(ctx, dbgen.UpdatePolicyParams{
		ID:          pgtypeFromUUID(params.ID),
		Name:        params.Name,
		Description: params.Description,
		Enabled:     params.Enabled,
		Priority:    params.Priority,
	})
	if err != nil {
		return model.Policy{}, apperrors.FromPg(err, "update policy %s", params.ID)
	}
	// Re-compose with current rule + actions so the caller sees the full shape.
	return r.composePolicyFromRow(ctx, row)
}

// composePolicyFromRow loads the rule + actions for a freshly-returned policy
// row and composes them into a model.Policy. Used after writes that return the
// updated policy row but not the joined data.
func (r *Repository) composePolicyFromRow(ctx context.Context, row dbgen.Policy) (model.Policy, error) {
	id := uuidFromPgtype(row.ID)

	var rulePtr *model.Rule
	ruleRow, ruleErr := r.Q.GetRuleForPolicy(ctx, row.ID)
	if ruleErr == nil {
		m := toModelRule(ruleRow)
		rulePtr = &m
	} else if e := apperrors.FromPg(ruleErr, "rule for policy %s not found", id); e != nil && !apperrors.IsNotFound(e) {
		return model.Policy{}, e
	}

	actionRows, err := r.Q.ListActionsForPolicy(ctx, row.ID)
	if err != nil {
		return model.Policy{}, apperrors.FromPg(err, "list actions for policy %s", id)
	}
	actions := make([]model.Action, 0, len(actionRows))
	for _, ar := range actionRows {
		actions = append(actions, toModelAction(ar))
	}

	return policyFromDbgen(row, rulePtr, actions), nil
}

func (r *Repository) DeletePolicy(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeletePolicy(ctx, pgtypeFromUUID(id)), "delete policy %s", id)
}

func (r *Repository) UpdatePolicyPriority(ctx context.Context, id uuid.UUID, priority int32) error {
	return apperrors.FromPg(r.Q.UpdatePolicyPriority(ctx, dbgen.UpdatePolicyPriorityParams{
		ID:       pgtypeFromUUID(id),
		Priority: priority,
	}), "update priority for policy %s", id)
}

func (r *Repository) GetRuleForPolicy(ctx context.Context, policyID uuid.UUID) (model.Rule, error) {
	row, err := r.Q.GetRuleForPolicy(ctx, pgtypeFromUUID(policyID))
	if err != nil {
		return model.Rule{}, apperrors.FromPg(err, "rule for policy %s not found", policyID)
	}
	return toModelRule(row), nil
}

// GetRuleByID fetches a single rule by its own ID. Used by the policy engine
// to follow logical-operator references (the LeftOperand / RightOperand of an
// `and` / `or` / `not` rule is the UUID of a child rule). The underlying
// query is a scan of all rules — small set, infrequent path, and avoids
// adding a new SQL query just for this case.
func (r *Repository) GetRuleByID(ctx context.Context, id uuid.UUID) (model.Rule, error) {
	rows, err := r.Q.ListAllRules(ctx)
	if err != nil {
		return model.Rule{}, apperrors.FromPg(err, "list policy rules")
	}
	for _, row := range rows {
		if uuidFromPgtype(row.ID) == id {
			return toModelRule(row), nil
		}
	}
	return model.Rule{}, apperrors.NotFoundf("rule %s not found", id)
}

func (r *Repository) CreateRule(ctx context.Context, params CreateRuleParams) (model.Rule, error) {
	row, err := r.Q.CreateRule(ctx, dbgen.CreateRuleParams{
		PolicyID:     pgtypeFromUUID(params.PolicyID),
		LeftOperand:  params.LeftOperand,
		Operator:     params.Operator,
		RightOperand: params.RightOperand,
	})
	if err != nil {
		return model.Rule{}, apperrors.FromPg(err, "create rule for policy %s", params.PolicyID)
	}
	return toModelRule(row), nil
}

func (r *Repository) UpdateRule(ctx context.Context, params UpdateRuleParams) (model.Rule, error) {
	row, err := r.Q.UpdateRule(ctx, dbgen.UpdateRuleParams{
		ID:           pgtypeFromUUID(params.ID),
		LeftOperand:  params.LeftOperand,
		Operator:     params.Operator,
		RightOperand: params.RightOperand,
	})
	if err != nil {
		return model.Rule{}, apperrors.FromPg(err, "update rule %s", params.ID)
	}
	return toModelRule(row), nil
}

func (r *Repository) DeleteRule(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteRule(ctx, pgtypeFromUUID(id)), "delete rule %s", id)
}

func (r *Repository) DeleteRuleForPolicy(ctx context.Context, policyID uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteRuleForPolicy(ctx, pgtypeFromUUID(policyID)), "delete rule for policy %s", policyID)
}

func (r *Repository) ListActionsForPolicy(ctx context.Context, policyID uuid.UUID) ([]model.Action, error) {
	rows, err := r.Q.ListActionsForPolicy(ctx, pgtypeFromUUID(policyID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list actions for policy %s", policyID)
	}
	out := make([]model.Action, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelAction(row))
	}
	return out, nil
}

func (r *Repository) GetAction(ctx context.Context, id uuid.UUID) (model.Action, error) {
	row, err := r.Q.GetAction(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.Action{}, apperrors.FromPg(err, "action %s not found", id)
	}
	return toModelAction(row), nil
}

func (r *Repository) CreateAction(ctx context.Context, params CreateActionParams) (model.Action, error) {
	row, err := r.Q.CreateAction(ctx, dbgen.CreateActionParams{
		PolicyID:    pgtypeFromUUID(params.PolicyID),
		Type:        params.Type,
		Value:       params.Value,
		ActionOrder: params.Order,
	})
	if err != nil {
		return model.Action{}, apperrors.FromPg(err, "create action for policy %s", params.PolicyID)
	}
	return toModelAction(row), nil
}

func (r *Repository) UpdateAction(ctx context.Context, params UpdateActionParams) (model.Action, error) {
	row, err := r.Q.UpdateAction(ctx, dbgen.UpdateActionParams{
		ID:          pgtypeFromUUID(params.ID),
		Type:        params.Type,
		Value:       params.Value,
		ActionOrder: params.Order,
	})
	if err != nil {
		return model.Action{}, apperrors.FromPg(err, "update action %s", params.ID)
	}
	return toModelAction(row), nil
}

func (r *Repository) DeleteAction(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteAction(ctx, pgtypeFromUUID(id)), "delete action %s", id)
}
