package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/routing"
	"github.com/kyleaupton/arrflix/internal/rules"
)

// RoutingService orchestrates the routing domain: rule CRUD with
// authoring-time validation against the field registry, the grab-time
// dispatch (pure routing.Dispatch plus default fallbacks from config), and
// the editor's field catalog.
type RoutingService struct {
	repo *repo.Repository
	reg  *rules.Registry
}

func NewRoutingService(r *repo.Repository) *RoutingService {
	return &RoutingService{repo: r, reg: rules.NewRegistry()}
}

// RoutingRuleInput is the writeable shape for rule create/update. Conditions
// carries the canonical condition-tree JSON; it is decoded, type-checked
// against the registry, and re-encoded canonically before storage.
type RoutingRuleInput struct {
	Name           string
	Enabled        bool
	Conditions     json.RawMessage
	DownloaderID   *uuid.UUID
	LibraryID      *uuid.UUID
	NameTemplateID *uuid.UUID
	Continue       bool
}

func (s *RoutingService) List(ctx context.Context) ([]model.RoutingRule, error) {
	return s.repo.ListRoutingRules(ctx)
}

func (s *RoutingService) Get(ctx context.Context, id uuid.UUID) (model.RoutingRule, error) {
	return s.repo.GetRoutingRule(ctx, id)
}

func (s *RoutingService) Create(ctx context.Context, in RoutingRuleInput) (model.RoutingRule, error) {
	conditions, err := s.validateInput(ctx, in, "RoutingService.Create")
	if err != nil {
		return model.RoutingRule{}, err
	}
	return s.repo.CreateRoutingRule(ctx, repo.CreateRoutingRuleParams{
		Name:           in.Name,
		Enabled:        in.Enabled,
		Conditions:     conditions,
		DownloaderID:   in.DownloaderID,
		LibraryID:      in.LibraryID,
		NameTemplateID: in.NameTemplateID,
		Continue:       in.Continue,
	})
}

func (s *RoutingService) Update(ctx context.Context, id uuid.UUID, in RoutingRuleInput) (model.RoutingRule, error) {
	conditions, err := s.validateInput(ctx, in, "RoutingService.Update")
	if err != nil {
		return model.RoutingRule{}, err
	}
	return s.repo.UpdateRoutingRule(ctx, repo.UpdateRoutingRuleParams{
		ID:             id,
		Name:           in.Name,
		Enabled:        in.Enabled,
		Conditions:     conditions,
		DownloaderID:   in.DownloaderID,
		LibraryID:      in.LibraryID,
		NameTemplateID: in.NameTemplateID,
		Continue:       in.Continue,
	})
}

func (s *RoutingService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteRoutingRule(ctx, id)
}

// Reorder reassigns positions from the full ordered id list. The list must be
// a permutation of every existing rule — reordering is a list-level
// operation, not a per-rule patch.
func (s *RoutingService) Reorder(ctx context.Context, ids []uuid.UUID) ([]model.RoutingRule, error) {
	existing, err := s.repo.ListRoutingRules(ctx)
	if err != nil {
		return nil, err
	}
	current := make(map[uuid.UUID]bool, len(existing))
	for _, r := range existing {
		current[r.ID] = true
	}
	seen := make(map[uuid.UUID]bool, len(ids))
	var fields []apperrors.FieldError
	for i, id := range ids {
		if seen[id] {
			fields = append(fields, apperrors.Field(fmt.Sprintf("body.ids[%d]", i), "duplicate rule id"))
		}
		seen[id] = true
		if !current[id] {
			fields = append(fields, apperrors.Field(fmt.Sprintf("body.ids[%d]", i), "unknown rule id"))
		}
	}
	if len(ids) != len(existing) {
		fields = append(fields, apperrors.Field("body.ids", fmt.Sprintf("must list all %d rules, got %d", len(existing), len(ids))))
	}
	if len(fields) > 0 {
		return nil, apperrors.Validation("invalid rule order", fields...).Op("RoutingService.Reorder")
	}

	if err := s.repo.UpdateRoutingRulePositions(ctx, ids); err != nil {
		return nil, err
	}
	return s.repo.ListRoutingRules(ctx)
}

// validateInput runs every authoring-time check and returns the canonical
// condition bytes to store. All field problems are collected so the editor
// can flag everything at once.
func (s *RoutingService) validateInput(ctx context.Context, in RoutingRuleInput, op string) ([]byte, error) {
	var fields []apperrors.FieldError
	if in.Name == "" {
		fields = append(fields, apperrors.Field("body.name", "required"))
	}

	// Decode + type-check the condition tree against the registry.
	var canonical []byte
	if len(in.Conditions) == 0 {
		fields = append(fields, apperrors.Field("body.conditions", "required"))
	} else {
		var cond rules.Condition
		if err := json.Unmarshal(in.Conditions, &cond); err != nil {
			fields = append(fields, apperrors.Field("body.conditions", "not a valid condition tree"))
		} else if err := s.reg.Validate(&cond); err != nil {
			var verr *rules.ValidationError
			if errors.As(err, &verr) {
				for _, p := range verr.Problems {
					loc := "body.conditions"
					if p.Loc != "" {
						loc += "." + p.Loc
					}
					fields = append(fields, apperrors.Field(loc, p.Msg))
				}
			} else {
				fields = append(fields, apperrors.Field("body.conditions", err.Error()))
			}
		} else {
			// Re-encode the decoded tree so the stored bytes are the canonical
			// encoding regardless of the caller's whitespace or key order.
			canonical, _ = json.Marshal(&cond)
		}
	}

	// Referenced action targets must resolve at save time. Post-save
	// disappearance is handled by the schema's ON DELETE SET NULL plus the
	// dispatch fallback.
	if in.DownloaderID != nil {
		if _, err := s.repo.GetDownloader(ctx, *in.DownloaderID); apperrors.IsNotFound(err) {
			fields = append(fields, apperrors.Field("body.downloaderId", "downloader not found"))
		} else if err != nil {
			return nil, err
		}
	}
	if in.LibraryID != nil {
		if _, err := s.repo.GetLibrary(ctx, *in.LibraryID); apperrors.IsNotFound(err) {
			fields = append(fields, apperrors.Field("body.libraryId", "library not found"))
		} else if err != nil {
			return nil, err
		}
	}
	if in.NameTemplateID != nil {
		if _, err := s.repo.GetNameTemplate(ctx, *in.NameTemplateID); apperrors.IsNotFound(err) {
			fields = append(fields, apperrors.Field("body.nameTemplateId", "name template not found"))
		} else if err != nil {
			return nil, err
		}
	}

	if len(fields) > 0 {
		return nil, apperrors.Validation("invalid routing rule", fields...).Op(op)
	}
	return canonical, nil
}

// Dispatch evaluates the rule list against the subject at grab and returns
// the action set with every slot resolved — slots no rule filled fall back to
// the configured defaults. The Evaluation is routing's dispatch record (the
// returned Final reflects the post-fallback set; FellBack says which slots
// came from defaults).
func (s *RoutingService) Dispatch(ctx context.Context, subject model.Subject) (routing.ActionSet, routing.Evaluation, error) {
	rows, err := s.repo.ListRoutingRules(ctx)
	if err != nil {
		return routing.ActionSet{}, routing.Evaluation{}, err
	}

	rs := make([]routing.Rule, 0, len(rows))
	for _, row := range rows {
		var cond rules.Condition
		if err := json.Unmarshal(row.Conditions, &cond); err != nil {
			return routing.ActionSet{}, routing.Evaluation{}, apperrors.Internalf(
				"routing rule %s has undecodable conditions: %v", row.ID, err).
				Op("RoutingService.Dispatch").
				NotRetryable()
		}
		rs = append(rs, routing.Rule{
			ID:         row.ID,
			Name:       row.Name,
			Enabled:    row.Enabled,
			Position:   row.Position,
			Conditions: &cond,
			Actions: routing.ActionSet{
				DownloaderID:   row.DownloaderID,
				LibraryID:      row.LibraryID,
				NameTemplateID: row.NameTemplateID,
			},
			Continue: row.Continue,
		})
	}

	final, eval := routing.Dispatch(s.reg, rs, subject)

	// Fill unset slots from the configured defaults. The media type for the
	// library/template defaults comes from the matched media, falling back to
	// category-prefix inference when the subject wasn't matched.
	if final.DownloaderID == nil {
		downloader, err := s.repo.GetDefaultDownloader(ctx, "torrent")
		if err != nil {
			return final, eval, err
		}
		id := downloader.ID
		final.DownloaderID = &id
	}

	mediaType := model.MediaType(subject.Media.Type)
	if mediaType == "" {
		for _, cat := range subject.Release.Candidate.Categories {
			if strings.HasPrefix(cat, "Movies/") || cat == "Movies" {
				mediaType = model.MediaTypeMovie
				break
			}
			if strings.HasPrefix(cat, "TV/") || cat == "TV" {
				mediaType = model.MediaTypeSeries
				break
			}
		}
	}

	if final.LibraryID == nil {
		library, err := s.repo.GetDefaultLibrary(ctx, string(mediaType))
		if err != nil {
			return final, eval, err
		}
		id := library.ID
		final.LibraryID = &id
	}
	if final.NameTemplateID == nil {
		nameTemplate, err := s.repo.GetDefaultNameTemplate(ctx, string(mediaType))
		if err != nil {
			return final, eval, err
		}
		id := nameTemplate.ID
		final.NameTemplateID = &id
	}

	eval.Final = final
	return final, eval, nil
}

// GetFieldDefinitions returns the editor's field catalog, generated from the
// Subject struct tags. Unassembled namespaces (want.* — registered, but no
// producer populates them yet) are omitted so the editor can't offer fields
// that always evaluate indeterminate.
func (s *RoutingService) GetFieldDefinitions(ctx context.Context) ([]model.FieldDefinition, error) {
	subjectFields := model.ListSubjectFields()
	fields := make([]model.FieldDefinition, 0, len(subjectFields))

	for _, sf := range subjectFields {
		if strings.HasPrefix(sf.Path, "want.") {
			continue
		}
		fields = append(fields, subjectFieldToDefinition(sf))
	}

	return fields, nil
}

// subjectFieldToDefinition converts a SubjectFieldInfo to a FieldDefinition
func subjectFieldToDefinition(cf model.SubjectFieldInfo) model.FieldDefinition {
	fieldDef := model.FieldDefinition{
		Path:      cf.Path,
		Label:     cf.Label,
		ValueType: cf.ValueType,
		Phase:     cf.Phase,
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
