package model

// FieldType represents the type of a Subject field as exposed to the rule
// editor: it selects the operator menu and the value widget.
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

// FieldDefinition is the editor-facing projection of a Subject field: the
// catalog metadata plus the operators valid for its type. Served by the
// routing fields endpoint to drive autocomplete, operator menus, and value
// widgets.
type FieldDefinition struct {
	Path          string      `json:"path"`                    // e.g., "candidate.size", "quality.resolution"
	Label         string      `json:"label"`                   // Display name
	Type          FieldType   `json:"type"`                    // "text", "number", "enum", "dynamic", "boolean"
	ValueType     string      `json:"valueType"`               // "string", "int64", "float64", "[]string", "bool"
	Phase         Phase       `json:"phase"`                   // earliest moment the field is knowable
	EnumValues    []EnumValue `json:"enumValues,omitempty"`    // For enum type
	DynamicSource string      `json:"dynamicSource,omitempty"` // API endpoint for dynamic fields
	Operators     []string    `json:"operators"`               // Valid operators for this field
}
