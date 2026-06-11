package repo

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type RoutingRepo interface {
	ListRoutingRules(ctx context.Context) ([]model.RoutingRule, error)
	GetRoutingRule(ctx context.Context, id uuid.UUID) (model.RoutingRule, error)
	CreateRoutingRule(ctx context.Context, params CreateRoutingRuleParams) (model.RoutingRule, error)
	UpdateRoutingRule(ctx context.Context, params UpdateRoutingRuleParams) (model.RoutingRule, error)
	DeleteRoutingRule(ctx context.Context, id uuid.UUID) error
	UpdateRoutingRulePositions(ctx context.Context, ids []uuid.UUID) error
}

// CreateRoutingRuleParams is the domain-shaped input for CreateRoutingRule.
// Mirrors the writeable subset of model.RoutingRule — Position is
// server-assigned (new rules append to the end of the list; reordering is the
// list-level UpdateRoutingRulePositions operation).
type CreateRoutingRuleParams struct {
	Name           string
	Enabled        bool
	Conditions     []byte
	DownloaderID   *uuid.UUID
	LibraryID      *uuid.UUID
	NameTemplateID *uuid.UUID
	Continue       bool
}

// UpdateRoutingRuleParams is the domain-shaped input for UpdateRoutingRule.
// Mirrors the writeable subset of model.RoutingRule plus the target ID;
// Position is excluded for the same reason as on create.
type UpdateRoutingRuleParams struct {
	ID             uuid.UUID
	Name           string
	Enabled        bool
	Conditions     []byte
	DownloaderID   *uuid.UUID
	LibraryID      *uuid.UUID
	NameTemplateID *uuid.UUID
	Continue       bool
}

// toModelRoutingRule translates the persistence-shaped dbgen.RoutingRule into
// the domain-shaped model.RoutingRule. Conditions bytes pass through verbatim
// — the JSONB column holds the canonical tree encoding.
func toModelRoutingRule(row dbgen.RoutingRule) model.RoutingRule {
	return model.RoutingRule{
		ID:             uuidFromPgtype(row.ID),
		Name:           row.Name,
		Enabled:        row.Enabled,
		Position:       row.Position,
		Conditions:     row.Conditions,
		DownloaderID:   uuidPtrFromPgtype(row.DownloaderID),
		LibraryID:      uuidPtrFromPgtype(row.LibraryID),
		NameTemplateID: uuidPtrFromPgtype(row.NameTemplateID),
		Continue:       row.Continue,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

// ListRoutingRules returns every routing rule in position order.
func (r *Repository) ListRoutingRules(ctx context.Context) ([]model.RoutingRule, error) {
	rows, err := r.Q.ListRoutingRules(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list routing rules")
	}
	out := make([]model.RoutingRule, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelRoutingRule(row))
	}
	return out, nil
}

func (r *Repository) GetRoutingRule(ctx context.Context, id uuid.UUID) (model.RoutingRule, error) {
	row, err := r.Q.GetRoutingRule(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.RoutingRule{}, apperrors.FromPg(err, "routing rule %s not found", id)
	}
	return toModelRoutingRule(row), nil
}

func (r *Repository) CreateRoutingRule(ctx context.Context, params CreateRoutingRuleParams) (model.RoutingRule, error) {
	row, err := r.Q.CreateRoutingRule(ctx, dbgen.CreateRoutingRuleParams{
		Name:           params.Name,
		Enabled:        params.Enabled,
		Conditions:     params.Conditions,
		DownloaderID:   pgtypeFromUUIDPtr(params.DownloaderID),
		LibraryID:      pgtypeFromUUIDPtr(params.LibraryID),
		NameTemplateID: pgtypeFromUUIDPtr(params.NameTemplateID),
		RuleContinue:   params.Continue,
	})
	if err != nil {
		return model.RoutingRule{}, apperrors.FromPg(err, "create routing rule %q", params.Name)
	}
	return toModelRoutingRule(row), nil
}

func (r *Repository) UpdateRoutingRule(ctx context.Context, params UpdateRoutingRuleParams) (model.RoutingRule, error) {
	row, err := r.Q.UpdateRoutingRule(ctx, dbgen.UpdateRoutingRuleParams{
		ID:             pgtypeFromUUID(params.ID),
		Name:           params.Name,
		Enabled:        params.Enabled,
		Conditions:     params.Conditions,
		DownloaderID:   pgtypeFromUUIDPtr(params.DownloaderID),
		LibraryID:      pgtypeFromUUIDPtr(params.LibraryID),
		NameTemplateID: pgtypeFromUUIDPtr(params.NameTemplateID),
		RuleContinue:   params.Continue,
	})
	if err != nil {
		return model.RoutingRule{}, apperrors.FromPg(err, "update routing rule %s", params.ID)
	}
	return toModelRoutingRule(row), nil
}

func (r *Repository) DeleteRoutingRule(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteRoutingRule(ctx, pgtypeFromUUID(id)), "delete routing rule %s", id)
}

// UpdateRoutingRulePositions reassigns positions from the full ordered id
// list — index in the slice becomes the rule's position. The position unique
// constraint is deferred, so a single statement can permute freely.
func (r *Repository) UpdateRoutingRulePositions(ctx context.Context, ids []uuid.UUID) error {
	pgIDs := make([]pgtype.UUID, len(ids))
	positions := make([]int32, len(ids))
	for i, id := range ids {
		pgIDs[i] = pgtypeFromUUID(id)
		positions[i] = int32(i)
	}
	return apperrors.FromPg(r.Q.UpdateRoutingRulePositions(ctx, dbgen.UpdateRoutingRulePositionsParams{
		Ids:       pgIDs,
		Positions: positions,
	}), "reorder routing rules")
}
