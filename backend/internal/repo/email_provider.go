package repo

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

type EmailProviderRepo interface {
	GetEmailProvider(ctx context.Context) (model.EmailProvider, bool, error)
	CreateEmailProvider(ctx context.Context, params CreateEmailProviderParams) (model.EmailProvider, error)
	UpdateEmailProvider(ctx context.Context, params UpdateEmailProviderParams) (model.EmailProvider, error)
}

// CreateEmailProviderParams is the domain-shaped input for CreateEmailProvider.
// Mirrors the writeable subset of model.EmailProvider.
type CreateEmailProviderParams struct {
	Provider      string
	FromAddress   string
	FromName      *string
	ReplyTo       *string
	Host          *string
	Port          *int
	Security      *string
	Auth          bool
	Username      *string
	Password      *string
	SkipTLSVerify bool
	ConfigJSON    json.RawMessage
	Enabled       bool
}

// UpdateEmailProviderParams is the domain-shaped input for UpdateEmailProvider,
// mirroring the writeable subset of model.EmailProvider plus the target ID.
type UpdateEmailProviderParams struct {
	ID            uuid.UUID
	Provider      string
	FromAddress   string
	FromName      *string
	ReplyTo       *string
	Host          *string
	Port          *int
	Security      *string
	Auth          bool
	Username      *string
	Password      *string
	SkipTLSVerify bool
	ConfigJSON    json.RawMessage
	Enabled       bool
}

// toModelEmailProvider translates the persistence-shaped dbgen.EmailProvider
// into the domain-shaped model.EmailProvider. The nullable INT port becomes a
// *int; the reserved config_json column is passed through verbatim (nil when
// empty — unlike Downloader, email has no legacy "{}" wire contract to honor).
func toModelEmailProvider(row dbgen.EmailProvider) model.EmailProvider {
	var cfg json.RawMessage
	if len(row.ConfigJson) > 0 {
		cfg = json.RawMessage(row.ConfigJson)
	}
	return model.EmailProvider{
		ID:            uuidFromPgtype(row.ID),
		Provider:      row.Provider,
		FromAddress:   row.FromAddress,
		FromName:      row.FromName,
		ReplyTo:       row.ReplyTo,
		Host:          row.Host,
		Port:          intPtrFromInt32Ptr(row.Port),
		Security:      row.Security,
		Auth:          row.Auth,
		Username:      row.Username,
		Password:      row.Password,
		SkipTLSVerify: row.SkipTlsVerify,
		ConfigJSON:    cfg,
		Enabled:       row.Enabled,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

// GetEmailProvider returns the singleton email provider row. The bool reports
// whether a row exists: false (no row, surfaced as pgx.ErrNoRows) is the
// unconfigured state, not an error — the handler renders it as 200
// {configured:false} rather than 404. Mirrors MirrorWantStatus's (v, ok, err).
func (r *Repository) GetEmailProvider(ctx context.Context) (model.EmailProvider, bool, error) {
	row, err := r.Q.GetEmailProvider(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.EmailProvider{}, false, nil
		}
		return model.EmailProvider{}, false, apperrors.FromPg(err, "get email provider")
	}
	return toModelEmailProvider(row), true, nil
}

func (r *Repository) CreateEmailProvider(ctx context.Context, params CreateEmailProviderParams) (model.EmailProvider, error) {
	row, err := r.Q.CreateEmailProvider(ctx, dbgen.CreateEmailProviderParams{
		Provider:      params.Provider,
		FromAddress:   params.FromAddress,
		FromName:      params.FromName,
		ReplyTo:       params.ReplyTo,
		Host:          params.Host,
		Port:          int32PtrFromIntPtr(params.Port),
		Security:      params.Security,
		Auth:          params.Auth,
		Username:      params.Username,
		Password:      params.Password,
		SkipTlsVerify: params.SkipTLSVerify,
		ConfigJson:    []byte(params.ConfigJSON),
		Enabled:       params.Enabled,
	})
	if err != nil {
		return model.EmailProvider{}, apperrors.FromPg(err, "create email provider")
	}
	return toModelEmailProvider(row), nil
}

func (r *Repository) UpdateEmailProvider(ctx context.Context, params UpdateEmailProviderParams) (model.EmailProvider, error) {
	row, err := r.Q.UpdateEmailProvider(ctx, dbgen.UpdateEmailProviderParams{
		ID:            pgtypeFromUUID(params.ID),
		Provider:      params.Provider,
		FromAddress:   params.FromAddress,
		FromName:      params.FromName,
		ReplyTo:       params.ReplyTo,
		Host:          params.Host,
		Port:          int32PtrFromIntPtr(params.Port),
		Security:      params.Security,
		Auth:          params.Auth,
		Username:      params.Username,
		Password:      params.Password,
		SkipTlsVerify: params.SkipTLSVerify,
		ConfigJson:    []byte(params.ConfigJSON),
		Enabled:       params.Enabled,
	})
	if err != nil {
		return model.EmailProvider{}, apperrors.FromPg(err, "update email provider %s", params.ID)
	}
	return toModelEmailProvider(row), nil
}

// int32PtrFromIntPtr / intPtrFromInt32Ptr bridge the domain *int (JSON-shaped
// port) and the persistence *int32 (sqlc's nullable INT). nil round-trips as
// the SQL NULL that leaves a non-SMTP provider's port unset.
func int32PtrFromIntPtr(p *int) *int32 {
	if p == nil {
		return nil
	}
	v := int32(*p)
	return &v
}

func intPtrFromInt32Ptr(p *int32) *int {
	if p == nil {
		return nil
	}
	v := int(*p)
	return &v
}
