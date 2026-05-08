package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
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
	pols, err := r.Q.ListPolicies(ctx)
	return pols, apperrors.FromPg(err, "list policies")
}

func (r *Repository) GetPolicy(ctx context.Context, id pgtype.UUID) (dbgen.Policy, error) {
	pol, err := r.Q.GetPolicy(ctx, id)
	return pol, apperrors.FromPg(err, "policy %s not found", id)
}

func (r *Repository) CreatePolicy(ctx context.Context, name string, description *string, enabled bool, priority int32) (dbgen.Policy, error) {
	pol, err := r.Q.CreatePolicy(ctx, dbgen.CreatePolicyParams{
		Name:        name,
		Description: description,
		Enabled:     enabled,
		Priority:    priority,
	})
	return pol, apperrors.FromPg(err, "create policy %q", name)
}

func (r *Repository) UpdatePolicy(ctx context.Context, id pgtype.UUID, name string, description *string, enabled bool, priority int32) (dbgen.Policy, error) {
	pol, err := r.Q.UpdatePolicy(ctx, dbgen.UpdatePolicyParams{
		ID:          id,
		Name:        name,
		Description: description,
		Enabled:     enabled,
		Priority:    priority,
	})
	return pol, apperrors.FromPg(err, "update policy %s", id)
}

func (r *Repository) DeletePolicy(ctx context.Context, id pgtype.UUID) error {
	return apperrors.FromPg(r.Q.DeletePolicy(ctx, id), "delete policy %s", id)
}

func (r *Repository) GetRuleForPolicy(ctx context.Context, policyID pgtype.UUID) (dbgen.Rule, error) {
	rule, err := r.Q.GetRuleForPolicy(ctx, policyID)
	return rule, apperrors.FromPg(err, "rule for policy %s not found", policyID)
}

func (r *Repository) CreateRule(ctx context.Context, policyID pgtype.UUID, leftOperand, operator, rightOperand string) (dbgen.Rule, error) {
	rule, err := r.Q.CreateRule(ctx, dbgen.CreateRuleParams{
		PolicyID:     policyID,
		LeftOperand:  leftOperand,
		Operator:     operator,
		RightOperand: rightOperand,
	})
	return rule, apperrors.FromPg(err, "create rule for policy %s", policyID)
}

func (r *Repository) UpdateRule(ctx context.Context, id pgtype.UUID, leftOperand, operator, rightOperand string) (dbgen.Rule, error) {
	rule, err := r.Q.UpdateRule(ctx, dbgen.UpdateRuleParams{
		ID:           id,
		LeftOperand:  leftOperand,
		Operator:     operator,
		RightOperand: rightOperand,
	})
	return rule, apperrors.FromPg(err, "update rule %s", id)
}

func (r *Repository) DeleteRule(ctx context.Context, id pgtype.UUID) error {
	return apperrors.FromPg(r.Q.DeleteRule(ctx, id), "delete rule %s", id)
}

func (r *Repository) DeleteRuleForPolicy(ctx context.Context, policyID pgtype.UUID) error {
	return apperrors.FromPg(r.Q.DeleteRuleForPolicy(ctx, policyID), "delete rule for policy %s", policyID)
}

func (r *Repository) ListActionsForPolicy(ctx context.Context, policyID pgtype.UUID) ([]dbgen.Action, error) {
	actions, err := r.Q.ListActionsForPolicy(ctx, policyID)
	return actions, apperrors.FromPg(err, "list actions for policy %s", policyID)
}

func (r *Repository) GetAction(ctx context.Context, id pgtype.UUID) (dbgen.Action, error) {
	action, err := r.Q.GetAction(ctx, id)
	return action, apperrors.FromPg(err, "action %s not found", id)
}

func (r *Repository) CreateAction(ctx context.Context, policyID pgtype.UUID, actionType, value string, order int32) (dbgen.Action, error) {
	action, err := r.Q.CreateAction(ctx, dbgen.CreateActionParams{
		PolicyID:    policyID,
		Type:        actionType,
		Value:       value,
		ActionOrder: order,
	})
	return action, apperrors.FromPg(err, "create action for policy %s", policyID)
}

func (r *Repository) UpdateAction(ctx context.Context, id pgtype.UUID, actionType, value string, order int32) (dbgen.Action, error) {
	action, err := r.Q.UpdateAction(ctx, dbgen.UpdateActionParams{
		ID:          id,
		Type:        actionType,
		Value:       value,
		ActionOrder: order,
	})
	return action, apperrors.FromPg(err, "update action %s", id)
}

func (r *Repository) DeleteAction(ctx context.Context, id pgtype.UUID) error {
	return apperrors.FromPg(r.Q.DeleteAction(ctx, id), "delete action %s", id)
}

func (r *Repository) ListAllRules(ctx context.Context) ([]dbgen.Rule, error) {
	rules, err := r.Q.ListAllRules(ctx)
	return rules, apperrors.FromPg(err, "list all rules")
}

func (r *Repository) ListAllActions(ctx context.Context) ([]dbgen.Action, error) {
	actions, err := r.Q.ListAllActions(ctx)
	return actions, apperrors.FromPg(err, "list all actions")
}

func (r *Repository) UpdatePolicyPriority(ctx context.Context, id pgtype.UUID, priority int32) error {
	return apperrors.FromPg(r.Q.UpdatePolicyPriority(ctx, dbgen.UpdatePolicyPriorityParams{
		ID:       id,
		Priority: priority,
	}), "update priority for policy %s", id)
}
