package rules

import (
	"github.com/kyleaupton/arrflix/internal/model"
)

// Registry is the entry point for evaluation and validation. It owns the
// immutable field index (built once from model.ListSubjectFields()) and the
// operator-type rules. Field metadata stays in model (the catalog); the
// lookup index and the type rules live here.
type Registry struct {
	fields map[string]model.SubjectFieldInfo
}

// NewRegistry builds a registry from the model's field catalog. The catalog
// is derived from static struct tags, so the registry is immutable after
// construction and safe for concurrent use.
func NewRegistry() *Registry {
	fields := make(map[string]model.SubjectFieldInfo)
	for _, f := range model.ListSubjectFields() {
		fields[f.Path] = f
	}
	return &Registry{fields: fields}
}

// Field looks up a field's catalog metadata by path.
func (r *Registry) Field(path string) (model.SubjectFieldInfo, bool) {
	f, ok := r.fields[path]
	return f, ok
}

// HasPhase reports whether the tree references any field of the given phase.
// Consumers use it to detect later-moment conditions (e.g. import-phase
// fields driving the import-time re-gate) and to drive phase badges in the
// rule editor.
func (r *Registry) HasPhase(cond *Condition, phase model.Phase) bool {
	if cond == nil {
		return false
	}
	for _, op := range []*Operand{cond.Left, cond.Right} {
		if op == nil || op.Kind != OperandField {
			continue
		}
		if info, ok := r.fields[op.Path]; ok && info.Phase == phase {
			return true
		}
	}
	for _, child := range cond.Children {
		if r.HasPhase(child, phase) {
			return true
		}
	}
	return false
}
