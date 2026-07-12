package email

import (
	"context"
	"fmt"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// Manager builds email Transports from the stored provider config or from an
// unsaved config record. It mirrors downloader.Manager's build surface but
// carries no client cache — Phase 1 only builds transports on demand for the
// test-send flow; a cached delivery transport is Phase 2's concern.
type Manager struct {
	registry *Registry
	repo     *repo.Repository
}

// NewManager creates an email manager.
func NewManager(registry *Registry, repo *repo.Repository) *Manager {
	return &Manager{registry: registry, repo: repo}
}

// BuildFromStored loads the singleton provider row and builds a Transport from
// it. Returns an error when no provider is configured — the caller (test-send
// against saved config) has nothing to test otherwise.
func (m *Manager) BuildFromStored(ctx context.Context) (Transport, error) {
	cfg, found, err := m.repo.GetEmailProvider(ctx)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("no email provider configured")
	}
	return m.registry.Build(recordFromModel(cfg))
}

// BuildFromConfig builds a Transport from a config record directly, with no DB
// lookup — for testing a configuration before it is saved.
func (m *Manager) BuildFromConfig(rec ConfigRecord) (Transport, error) {
	return m.registry.Build(rec)
}

// IsConfigured reports whether a usable provider is set up: a row exists and is
// enabled. It keeps provider-config knowledge in the email package so the
// notification worker can gate delivery on it (parking mail as awaiting_config
// until SMTP is configured) without reaching into the provider schema. A load
// error resolves false — the channel is treated as not-ready rather than
// silently delivering.
func (m *Manager) IsConfigured(ctx context.Context) bool {
	cfg, found, err := m.repo.GetEmailProvider(ctx)
	if err != nil {
		return false
	}
	return found && cfg.Enabled
}

// recordFromModel flattens the stored provider into the transport-agnostic
// ConfigRecord, dereferencing nullable SMTP columns to their zero value.
func recordFromModel(cfg model.EmailProvider) ConfigRecord {
	rec := ConfigRecord{
		Provider:      Provider(cfg.Provider),
		FromAddress:   cfg.FromAddress,
		Auth:          cfg.Auth,
		Username:      cfg.Username,
		Password:      cfg.Password,
		SkipTLSVerify: cfg.SkipTLSVerify,
		Config:        []byte(cfg.ConfigJSON),
	}
	if cfg.FromName != nil {
		rec.FromName = *cfg.FromName
	}
	if cfg.ReplyTo != nil {
		rec.ReplyTo = *cfg.ReplyTo
	}
	if cfg.Host != nil {
		rec.Host = *cfg.Host
	}
	if cfg.Port != nil {
		rec.Port = *cfg.Port
	}
	if cfg.Security != nil {
		rec.Security = *cfg.Security
	}
	return rec
}
