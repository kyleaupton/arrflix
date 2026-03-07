package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
)

type PolicyRepo interface {
	ListPolicies(ctx context.Context) ([]dbgen.Policy, error)
	GetPolicy(ctx context.Context, id pgtype.UUID) (dbgen.Policy, error)
	CreatePolicy(ctx context.Context, name string, description *string, enabled bool, priority int32) (dbgen.Policy, error)
	UpdatePolicy(ctx context.Context, id pgtype.UUID, name string, description *string, enabled bool, priority int32) (dbgen.Policy, error)
	DeletePolicy(ctx context.Context, id pgtype.UUID) error

	GetRuleForPolicy(ctx context.Context, policyID pgtype.UUID) (dbgen.Rule, error)
	CreateRule(ctx context.Context, policyID pgtype.UUID, leftOperand, operator, rightOperand string) (dbgen.Rule, error)
	UpdateRule(ctx context.Context, id pgtype.UUID, leftOperand, operator, rightOperand string) (dbgen.Rule, error)
	DeleteRule(ctx context.Context, id pgtype.UUID) error
	DeleteRuleForPolicy(ctx context.Context, policyID pgtype.UUID) error

	ListActionsForPolicy(ctx context.Context, policyID pgtype.UUID) ([]dbgen.Action, error)
	GetAction(ctx context.Context, id pgtype.UUID) (dbgen.Action, error)
	CreateAction(ctx context.Context, policyID pgtype.UUID, actionType, value string, order int32) (dbgen.Action, error)
	UpdateAction(ctx context.Context, id pgtype.UUID, actionType, value string, order int32) (dbgen.Action, error)
	DeleteAction(ctx context.Context, id pgtype.UUID) error

	ListAllRules(ctx context.Context) ([]dbgen.Rule, error)
	ListAllActions(ctx context.Context) ([]dbgen.Action, error)
	UpdatePolicyPriority(ctx context.Context, id pgtype.UUID, priority int32) error
}

func (r *Repository) ListPolicies(ctx context.Context) ([]dbgen.Policy, error) {
	return r.Q.ListPolicies(ctx)
}

func (r *Repository) GetPolicy(ctx context.Context, id pgtype.UUID) (dbgen.Policy, error) {
	return r.Q.GetPolicy(ctx, id)
}

func (r *Repository) CreatePolicy(ctx context.Context, name string, description *string, enabled bool, priority int32) (dbgen.Policy, error) {
	return r.Q.CreatePolicy(ctx, dbgen.CreatePolicyParams{
		Name:        name,
		Description: description,
		Enabled:     enabled,
		Priority:    priority,
	})
}

func (r *Repository) UpdatePolicy(ctx context.Context, id pgtype.UUID, name string, description *string, enabled bool, priority int32) (dbgen.Policy, error) {
	return r.Q.UpdatePolicy(ctx, dbgen.UpdatePolicyParams{
		ID:          id,
		Name:        name,
		Description: description,
		Enabled:     enabled,
		Priority:    priority,
	})
}

func (r *Repository) DeletePolicy(ctx context.Context, id pgtype.UUID) error {
	return r.Q.DeletePolicy(ctx, id)
}

func (r *Repository) GetRuleForPolicy(ctx context.Context, policyID pgtype.UUID) (dbgen.Rule, error) {
	return r.Q.GetRuleForPolicy(ctx, policyID)
}

func (r *Repository) CreateRule(ctx context.Context, policyID pgtype.UUID, leftOperand, operator, rightOperand string) (dbgen.Rule, error) {
	return r.Q.CreateRule(ctx, dbgen.CreateRuleParams{
		PolicyID:     policyID,
		LeftOperand:  leftOperand,
		Operator:     operator,
		RightOperand: rightOperand,
	})
}

func (r *Repository) UpdateRule(ctx context.Context, id pgtype.UUID, leftOperand, operator, rightOperand string) (dbgen.Rule, error) {
	return r.Q.UpdateRule(ctx, dbgen.UpdateRuleParams{
		ID:           id,
		LeftOperand:  leftOperand,
		Operator:     operator,
		RightOperand: rightOperand,
	})
}

func (r *Repository) DeleteRule(ctx context.Context, id pgtype.UUID) error {
	return r.Q.DeleteRule(ctx, id)
}

func (r *Repository) DeleteRuleForPolicy(ctx context.Context, policyID pgtype.UUID) error {
	return r.Q.DeleteRuleForPolicy(ctx, policyID)
}

func (r *Repository) ListActionsForPolicy(ctx context.Context, policyID pgtype.UUID) ([]dbgen.Action, error) {
	return r.Q.ListActionsForPolicy(ctx, policyID)
}

func (r *Repository) GetAction(ctx context.Context, id pgtype.UUID) (dbgen.Action, error) {
	return r.Q.GetAction(ctx, id)
}

func (r *Repository) CreateAction(ctx context.Context, policyID pgtype.UUID, actionType, value string, order int32) (dbgen.Action, error) {
	return r.Q.CreateAction(ctx, dbgen.CreateActionParams{
		PolicyID:    policyID,
		Type:        actionType,
		Value:       value,
		ActionOrder: order,
	})
}

func (r *Repository) UpdateAction(ctx context.Context, id pgtype.UUID, actionType, value string, order int32) (dbgen.Action, error) {
	return r.Q.UpdateAction(ctx, dbgen.UpdateActionParams{
		ID:          id,
		Type:        actionType,
		Value:       value,
		ActionOrder: order,
	})
}

func (r *Repository) DeleteAction(ctx context.Context, id pgtype.UUID) error {
	return r.Q.DeleteAction(ctx, id)
}

func (r *Repository) ListAllRules(ctx context.Context) ([]dbgen.Rule, error) {
	return r.Q.ListAllRules(ctx)
}

func (r *Repository) ListAllActions(ctx context.Context) ([]dbgen.Action, error) {
	return r.Q.ListAllActions(ctx)
}

func (r *Repository) UpdatePolicyPriority(ctx context.Context, id pgtype.UUID, priority int32) error {
	return r.Q.UpdatePolicyPriority(ctx, dbgen.UpdatePolicyPriorityParams{
		ID:       id,
		Priority: priority,
	})
}
