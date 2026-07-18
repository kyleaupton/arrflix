package service

import (
	"context"
	"encoding/json"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// EmailProviderService is pure persistence + validation for the singleton
// email provider. The test-send lives in the handler (which owns the email
// Manager), mirroring how downloader connection tests live in the handler.
type EmailProviderService struct {
	repo *repo.Repository
}

func NewEmailProviderService(r *repo.Repository) *EmailProviderService {
	return &EmailProviderService{repo: r}
}

// SaveEmailProviderParams is the domain-shaped write input. A nil Password
// signals "keep existing" (the write-only-password contract); the handler has
// already collapsed an empty-string password to nil before calling.
type SaveEmailProviderParams struct {
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

// Get returns the singleton provider. The bool reports whether one is
// configured (false is the unconfigured state, not an error).
func (s *EmailProviderService) Get(ctx context.Context) (model.EmailProvider, bool, error) {
	return s.repo.GetEmailProvider(ctx)
}

// validateEmailProviderInput collects every field-level problem before
// returning. Returns nil when the input is valid.
func validateEmailProviderInput(p SaveEmailProviderParams) *apperrors.Error {
	var fields []apperrors.FieldError
	if p.FromAddress == "" {
		fields = append(fields, apperrors.Field("body.fromAddress", "required"))
	}
	if p.Provider != "smtp" {
		fields = append(fields, apperrors.Field("body.provider", "must be 'smtp'"))
	}
	if p.Provider == "smtp" {
		if p.Host == nil || *p.Host == "" {
			fields = append(fields, apperrors.Field("body.host", "required"))
		}
		if p.Port == nil || *p.Port < 1 || *p.Port > 65535 {
			fields = append(fields, apperrors.Field("body.port", "must be between 1 and 65535"))
		}
		sec := ""
		if p.Security != nil {
			sec = *p.Security
		}
		if sec != "starttls" && sec != "implicit_tls" && sec != "none" {
			fields = append(fields, apperrors.Field("body.security", "must be 'starttls', 'implicit_tls', or 'none'"))
		}
	}
	if p.Auth && (p.Username == nil || *p.Username == "") {
		fields = append(fields, apperrors.Field("body.username", "required when auth is enabled"))
	}
	if len(fields) > 0 {
		return apperrors.Validation("invalid email provider", fields...)
	}
	return nil
}

// Save upserts the singleton provider. Get-or-create keeps this off the
// expression-index ON CONFLICT path. Write-only password: a nil incoming
// password reuses the stored secret (mirrors DownloadersService.Update).
func (s *EmailProviderService) Save(ctx context.Context, params SaveEmailProviderParams) (model.EmailProvider, error) {
	if err := validateEmailProviderInput(params); err != nil {
		return model.EmailProvider{}, err.Op("EmailProviderService.Save")
	}

	existing, found, err := s.repo.GetEmailProvider(ctx)
	if err != nil {
		return model.EmailProvider{}, err
	}

	password := params.Password
	if password == nil && found {
		password = existing.Password
	}

	if !found {
		return s.repo.CreateEmailProvider(ctx, repo.CreateEmailProviderParams{
			Provider:      params.Provider,
			FromAddress:   params.FromAddress,
			FromName:      params.FromName,
			ReplyTo:       params.ReplyTo,
			Host:          params.Host,
			Port:          params.Port,
			Security:      params.Security,
			Auth:          params.Auth,
			Username:      params.Username,
			Password:      password,
			SkipTLSVerify: params.SkipTLSVerify,
			ConfigJSON:    params.ConfigJSON,
			Enabled:       params.Enabled,
		})
	}

	return s.repo.UpdateEmailProvider(ctx, repo.UpdateEmailProviderParams{
		ID:            existing.ID,
		Provider:      params.Provider,
		FromAddress:   params.FromAddress,
		FromName:      params.FromName,
		ReplyTo:       params.ReplyTo,
		Host:          params.Host,
		Port:          params.Port,
		Security:      params.Security,
		Auth:          params.Auth,
		Username:      params.Username,
		Password:      password,
		SkipTLSVerify: params.SkipTLSVerify,
		ConfigJSON:    params.ConfigJSON,
		Enabled:       params.Enabled,
	})
}
