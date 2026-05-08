package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/policy"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type PoliciesService struct {
	repo   *repo.Repository
	engine *policy.Engine
}

func NewPoliciesService(r *repo.Repository, logg *logger.Logger) *PoliciesService {
	return &PoliciesService{
		repo:   r,
		engine: policy.NewEngine(r, logg),
	}
}

func (s *PoliciesService) List(ctx context.Context) ([]dbgen.Policy, error) {
	return s.repo.ListPolicies(ctx)
}

// ListFull returns all policies with their rules and actions assembled.
func (s *PoliciesService) ListFull(ctx context.Context) ([]dbgen.Policy, []dbgen.Rule, []dbgen.Action, error) {
	policies, err := s.repo.ListPolicies(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	rules, err := s.repo.ListAllRules(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	actions, err := s.repo.ListAllActions(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	return policies, rules, actions, nil
}

func (s *PoliciesService) Get(ctx context.Context, id pgtype.UUID) (dbgen.Policy, error) {
	return s.repo.GetPolicy(ctx, id)
}

func (s *PoliciesService) Create(ctx context.Context, name string, description *string, enabled bool, priority int32) (dbgen.Policy, error) {
	if name == "" {
		return dbgen.Policy{}, apperrors.Validation("invalid policy",
			apperrors.Field("body.name", "required"),
		).Op("PoliciesService.Create")
	}
	return s.repo.CreatePolicy(ctx, name, description, enabled, priority)
}

func (s *PoliciesService) Update(ctx context.Context, id pgtype.UUID, name string, description *string, enabled bool, priority int32) (dbgen.Policy, error) {
	if name == "" {
		return dbgen.Policy{}, apperrors.Validation("invalid policy",
			apperrors.Field("body.name", "required"),
		).Op("PoliciesService.Update")
	}
	return s.repo.UpdatePolicy(ctx, id, name, description, enabled, priority)
}

func (s *PoliciesService) Delete(ctx context.Context, id pgtype.UUID) error {
	return s.repo.DeletePolicy(ctx, id)
}

func (s *PoliciesService) GetRule(ctx context.Context, policyID pgtype.UUID) (dbgen.Rule, error) {
	return s.repo.GetRuleForPolicy(ctx, policyID)
}

// validRuleOperators is the set of operators accepted by rule construction.
var validRuleOperators = map[string]bool{
	"==": true, "!=": true, ">": true, ">=": true, "<": true, "<=": true,
	"contains": true, "in": true, "not in": true, "and": true, "or": true, "not": true,
}

func (s *PoliciesService) CreateRule(ctx context.Context, policyID pgtype.UUID, leftOperand, operator, rightOperand string) (dbgen.Rule, error) {
	if !validRuleOperators[operator] {
		return dbgen.Rule{}, apperrors.Validation("invalid rule",
			apperrors.Field("body.operator", "must be one of: ==, !=, >, >=, <, <=, contains, in, not in, and, or, not"),
		).Op("PoliciesService.CreateRule")
	}

	return s.repo.CreateRule(ctx, policyID, leftOperand, operator, rightOperand)
}

func (s *PoliciesService) UpdateRule(ctx context.Context, id pgtype.UUID, leftOperand, operator, rightOperand string) (dbgen.Rule, error) {
	if !validRuleOperators[operator] {
		return dbgen.Rule{}, apperrors.Validation("invalid rule",
			apperrors.Field("body.operator", "must be one of: ==, !=, >, >=, <, <=, contains, in, not in, and, or, not"),
		).Op("PoliciesService.UpdateRule")
	}

	return s.repo.UpdateRule(ctx, id, leftOperand, operator, rightOperand)
}

func (s *PoliciesService) DeleteRule(ctx context.Context, id pgtype.UUID) error {
	return s.repo.DeleteRule(ctx, id)
}

func (s *PoliciesService) ListActions(ctx context.Context, policyID pgtype.UUID) ([]dbgen.Action, error) {
	return s.repo.ListActionsForPolicy(ctx, policyID)
}

func (s *PoliciesService) GetAction(ctx context.Context, id pgtype.UUID) (dbgen.Action, error) {
	return s.repo.GetAction(ctx, id)
}

// validActionTypes is the set of action types accepted on creation/update.
var validActionTypes = map[string]bool{
	"set_downloader":    true,
	"set_library":       true,
	"set_name_template": true,
	"stop_processing":   true,
}

// validateActionInput collects field-level errors for action create/update.
func validateActionInput(actionType, value string) *apperrors.Error {
	var fields []apperrors.FieldError
	if !validActionTypes[actionType] {
		fields = append(fields, apperrors.Field("body.action_type", "must be one of: set_downloader, set_library, set_name_template, stop_processing"))
	}
	if value == "" && actionType != "stop_processing" {
		fields = append(fields, apperrors.Field("body.value", "required for this action type"))
	}
	if len(fields) > 0 {
		return apperrors.Validation("invalid action", fields...)
	}
	return nil
}

func (s *PoliciesService) CreateAction(ctx context.Context, policyID pgtype.UUID, actionType, value string, order int32) (dbgen.Action, error) {
	if err := validateActionInput(actionType, value); err != nil {
		return dbgen.Action{}, err.Op("PoliciesService.CreateAction")
	}

	return s.repo.CreateAction(ctx, policyID, actionType, value, order)
}

func (s *PoliciesService) UpdateAction(ctx context.Context, id pgtype.UUID, actionType, value string, order int32) (dbgen.Action, error) {
	if err := validateActionInput(actionType, value); err != nil {
		return dbgen.Action{}, err.Op("PoliciesService.UpdateAction")
	}

	return s.repo.UpdateAction(ctx, id, actionType, value, order)
}

func (s *PoliciesService) DeleteAction(ctx context.Context, id pgtype.UUID) error {
	return s.repo.DeleteAction(ctx, id)
}

// Evaluate evaluates policies against the evaluation context and returns an EvaluationTrace
func (s *PoliciesService) Evaluate(ctx context.Context, evalCtx model.EvaluationContext) (model.EvaluationTrace, error) {
	return s.engine.Evaluate(ctx, evalCtx)
}

// GetFieldDefinitions returns all available field definitions for policy rules
// These are auto-generated from the unified EvaluationContext struct tags
func (s *PoliciesService) GetFieldDefinitions(ctx context.Context) ([]model.FieldDefinition, error) {
	// Get auto-generated fields from the unified context model
	contextFields := model.ListContextFields()
	fields := make([]model.FieldDefinition, 0, len(contextFields))

	for _, cf := range contextFields {
		fieldDef := contextFieldToDefinition(cf)
		fields = append(fields, fieldDef)
	}

	return fields, nil
}

// contextFieldToDefinition converts a ContextFieldInfo to a FieldDefinition
func contextFieldToDefinition(cf model.ContextFieldInfo) model.FieldDefinition {
	fieldDef := model.FieldDefinition{
		Path:      cf.Path,
		Label:     cf.Label,
		ValueType: cf.ValueType,
	}

	// Convert type string to FieldType and set appropriate operators
	switch cf.Type {
	case "number":
		fieldDef.Type = model.FieldTypeNumber
		fieldDef.Operators = []string{"==", "!=", ">", ">=", "<", "<="}
	case "text":
		fieldDef.Type = model.FieldTypeText
		fieldDef.Operators = []string{"==", "!=", "contains", "in", "not in"}
	case "enum":
		fieldDef.Type = model.FieldTypeEnum
		fieldDef.Operators = []string{"==", "!=", "in", "not in"}
		// Convert enum values from []string to []EnumValue
		if len(cf.EnumValues) > 0 {
			fieldDef.EnumValues = make([]model.EnumValue, len(cf.EnumValues))
			for i, v := range cf.EnumValues {
				fieldDef.EnumValues[i] = model.EnumValue{Value: v, Label: v}
			}
		}
	case "boolean":
		fieldDef.Type = model.FieldTypeBoolean
		fieldDef.EnumValues = []model.EnumValue{
			{Value: "true", Label: "True"},
			{Value: "false", Label: "False"},
		}
		fieldDef.Operators = []string{"==", "!="}
	case "dynamic":
		fieldDef.Type = model.FieldTypeDynamic
		fieldDef.DynamicSource = cf.DynamicSource
		fieldDef.Operators = []string{"==", "!=", "in", "not in"}
	default:
		fieldDef.Type = model.FieldTypeText
		fieldDef.Operators = []string{"==", "!=", "contains"}
	}

	return fieldDef
}
