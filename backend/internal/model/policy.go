package model

import (
	"time"

	"github.com/google/uuid"
)

type Plan struct {
	DownloaderID   string `json:"downloaderId"`   // how to download
	LibraryID      string `json:"libraryId"`      // where to move/hardlink/copy the file to
	NameTemplateID string `json:"nameTemplateId"` // how to name the file
}

// Note: CandidateContext has been replaced by EvaluationContext in context.go
// which provides a unified context for both the policy engine and name templates.

// Policy is the domain shape for a policy with its condition rule and ordered
// action list nested. It mirrors the wire contract previously served by the
// `handlers.FullPolicy` shim — a flat policy row plus a single optional rule
// plus an ordered actions array. Composed at the repo boundary by combining
// the persistence-layer `dbgen.Policy`, `dbgen.Rule`, and `dbgen.Action` rows.
type Policy struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	Enabled     bool      `json:"enabled"`
	Priority    int32     `json:"priority"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`

	// Rule is the policy's condition rule; nil when the policy has no rule
	// configured yet. Wire field is "rule" to match the FullPolicy contract.
	Rule *Rule `json:"rule"`
	// Actions is the ordered list of actions applied when the rule matches.
	// Always serialized (empty array, never null) to match the FullPolicy
	// contract.
	Actions []Action `json:"actions"`
}

type Operator string

const (
	OpEq       Operator = "=="
	OpNe       Operator = "!="
	OpGt       Operator = ">"
	OpGte      Operator = ">="
	OpLt       Operator = "<"
	OpLte      Operator = "<="
	OpContains Operator = "contains"
	OpIn       Operator = "in"
	OpNotIn    Operator = "not in"
	OpAnd      Operator = "and"
	OpOr       Operator = "or"
	OpNot      Operator = "not"
)

// Rule is the domain shape for a rule row. Mirrors the wire contract previously
// served by raw `dbgen.Rule`; uses idiomatic Go types (uuid.UUID, time.Time).
// Logical operators (and, or, not) reference child rules by UUID stored as
// strings in LeftOperand / RightOperand — the engine resolves those references
// at evaluation time.
type Rule struct {
	ID           uuid.UUID `json:"id"`
	PolicyID     uuid.UUID `json:"policyId"`
	LeftOperand  string    `json:"leftOperand"`
	Operator     string    `json:"operator"`
	RightOperand string    `json:"rightOperand"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type ActionType string

const (
	ActionSetDownloader   ActionType = "set_downloader"
	ActionSetLibrary      ActionType = "set_library"
	ActionSetNameTemplate ActionType = "set_name_template"
	ActionStopProcessing  ActionType = "stop_processing"
)

// Action is the domain shape for an action row. Mirrors the wire contract
// previously served by raw `dbgen.Action`. Type is stored as a plain string
// (rather than typed `ActionType`) to preserve the FullPolicy wire format —
// callers compare against the `Action*` constants when needed.
type Action struct {
	ID        uuid.UUID `json:"id"`
	PolicyID  uuid.UUID `json:"policyId"`
	Type      string    `json:"type"`
	Value     string    `json:"value"`
	Order     int32     `json:"order"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// EvaluationTrace represents the detailed trace of policy evaluation
type EvaluationTrace struct {
	Policies  []PolicyEvaluation `json:"policies"`
	FinalPlan Plan               `json:"finalPlan"`
	Context   *ContextSnapshot   `json:"context,omitempty"` // Full evaluation context for debugging
}

// ContextSnapshot is a JSON-friendly representation of EvaluationContext
// Used to expose all available variables to the UI for debugging/transparency
type ContextSnapshot struct {
	Candidate map[string]any `json:"candidate"`
	Quality   map[string]any `json:"quality"`
	Release   map[string]any `json:"release"`
	Media     map[string]any `json:"media"`
	MediaInfo map[string]any `json:"mediainfo,omitempty"`
}

// PolicyEvaluation represents the evaluation result for a single policy
type PolicyEvaluation struct {
	PolicyID          string       `json:"policyId"`
	PolicyName        string       `json:"policyName"`
	Priority          int          `json:"priority"`
	Matched           bool         `json:"matched"`
	RuleEvaluated     *RuleInfo    `json:"ruleEvaluated,omitempty"`
	ActionsApplied    []ActionInfo `json:"actionsApplied"`
	StoppedProcessing bool         `json:"stoppedProcessing"`
}

// RuleInfo represents information about a rule
type RuleInfo struct {
	LeftOperand        string `json:"leftOperand"`
	LeftResolvedValue  any    `json:"leftResolvedValue,omitempty"`  // Resolved value of left operand
	Operator           string `json:"operator"`
	RightOperand       string `json:"rightOperand"`
	RightResolvedValue any    `json:"rightResolvedValue,omitempty"` // Resolved value of right operand
}

// ActionInfo represents information about an action
type ActionInfo struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	Order int32  `json:"order"`
}

// FieldType represents the type of a policy field
type FieldType string

const (
	FieldTypeText    FieldType = "text"
	FieldTypeNumber  FieldType = "number"
	FieldTypeEnum    FieldType = "enum"
	FieldTypeDynamic FieldType = "dynamic"
	FieldTypeBoolean FieldType = "boolean"
)

// EnumValue represents a single enum value option
type EnumValue struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// FieldDefinition represents metadata about a policy field
type FieldDefinition struct {
	Path          string      `json:"path"`                    // e.g., "candidate.size", "quality.resolution"
	Label         string      `json:"label"`                   // Display name
	Type          FieldType   `json:"type"`                    // "text", "number", "enum", "dynamic", "boolean"
	ValueType     string      `json:"valueType"`               // "string", "int64", "float64", "[]string", "bool"
	EnumValues    []EnumValue `json:"enumValues,omitempty"`    // For enum type
	DynamicSource string      `json:"dynamicSource,omitempty"` // API endpoint for dynamic fields
	Operators     []string    `json:"operators"`               // Valid operators for this field
}
