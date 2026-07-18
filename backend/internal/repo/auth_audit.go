package repo

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

// InsertAuthAuditParams is the domain-shaped input for InsertAuthAudit. Detail is
// a pre-marshaled JSON object — the service owns the per-event payload shape.
type InsertAuthAuditParams struct {
	UserID uuid.UUID
	Event  string
	Detail json.RawMessage
}

// InsertAuthAudit appends an auth audit row. Callers treat it as best-effort: a
// failed audit write must never fail the operation it records.
func (r *Repository) InsertAuthAudit(ctx context.Context, params InsertAuthAuditParams) error {
	return apperrors.FromPg(r.Q.InsertAuthAudit(ctx, dbgen.InsertAuthAuditParams{
		UserID: pgtypeFromUUID(params.UserID),
		Event:  params.Event,
		Detail: params.Detail,
	}), "insert auth audit %q", params.Event)
}
