package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/email"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

// EmailProvider is the singleton email-provider settings surface: get/save the
// one config row and test-send against either the stored or an unsaved config.
// Test-send hits the SMTP transport directly via the email Manager — decoupled
// from the notification delivery pipeline (worker/outbox/renderer are Phase 2).
type EmailProvider struct {
	svc          *service.Services
	emailManager *email.Manager
}

func NewEmailProvider(s *service.Services, manager *email.Manager) *EmailProvider {
	return &EmailProvider{svc: s, emailManager: manager}
}

// ----- Shared body shape -----

// emailProviderWriteBody is the PUT/test-config write surface. Password is
// write-only: on save, an empty/omitted password preserves the stored secret;
// it is never echoed back (see emailProviderResponse).
type emailProviderWriteBody struct {
	Provider      string  `json:"provider" required:"true" enum:"smtp" doc:"Email provider implementation"`
	FromAddress   string  `json:"fromAddress" required:"true" minLength:"1" doc:"From/envelope address"`
	FromName      *string `json:"fromName,omitempty" doc:"Optional display name for the From address"`
	ReplyTo       *string `json:"replyTo,omitempty" doc:"Optional Reply-To address"`
	Host          *string `json:"host,omitempty" doc:"SMTP server hostname"`
	Port          *int    `json:"port,omitempty" doc:"SMTP server port (1-65535)"`
	Security      *string `json:"security,omitempty" enum:"starttls,implicit_tls,none" doc:"Transport security mode"`
	Auth          bool    `json:"auth" doc:"Whether the server requires SMTP authentication"`
	Username      *string `json:"username,omitempty" doc:"SMTP username; required when auth is enabled"`
	Password      *string `json:"password,omitempty" doc:"SMTP password; empty preserves existing"`
	SkipTLSVerify bool    `json:"skipTlsVerify" doc:"Skip TLS certificate verification (self-signed relays)"`
	Enabled       bool    `json:"enabled" doc:"Whether email sending is enabled"`
}

// emailProviderResponse is the read DTO. It omits password entirely (write-only
// wire contract) and adds hasPassword + configured so the FE can render state
// without ever receiving the secret.
type emailProviderResponse struct {
	ID            uuid.UUID `json:"id"`
	Provider      string    `json:"provider"`
	FromAddress   string    `json:"fromAddress"`
	FromName      *string   `json:"fromName,omitempty"`
	ReplyTo       *string   `json:"replyTo,omitempty"`
	Host          *string   `json:"host,omitempty"`
	Port          *int      `json:"port,omitempty"`
	Security      *string   `json:"security,omitempty"`
	Auth          bool      `json:"auth"`
	Username      *string   `json:"username,omitempty"`
	HasPassword   bool      `json:"hasPassword"`
	SkipTLSVerify bool      `json:"skipTlsVerify"`
	Enabled       bool      `json:"enabled"`
	Configured    bool      `json:"configured"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func toEmailProviderResponse(m model.EmailProvider) emailProviderResponse {
	return emailProviderResponse{
		ID:            m.ID,
		Provider:      m.Provider,
		FromAddress:   m.FromAddress,
		FromName:      m.FromName,
		ReplyTo:       m.ReplyTo,
		Host:          m.Host,
		Port:          m.Port,
		Security:      m.Security,
		Auth:          m.Auth,
		Username:      m.Username,
		HasPassword:   m.Password != nil && *m.Password != "",
		SkipTLSVerify: m.SkipTLSVerify,
		Enabled:       m.Enabled,
		Configured:    true,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
}

// recordFromWriteBody flattens the write body into a transport ConfigRecord for
// the test-before-save flow, dereferencing the optional SMTP fields.
func recordFromWriteBody(b emailProviderWriteBody) email.ConfigRecord {
	rec := email.ConfigRecord{
		Provider:      email.Provider(b.Provider),
		FromAddress:   b.FromAddress,
		Auth:          b.Auth,
		Username:      b.Username,
		Password:      b.Password,
		SkipTLSVerify: b.SkipTLSVerify,
	}
	if b.FromName != nil {
		rec.FromName = *b.FromName
	}
	if b.ReplyTo != nil {
		rec.ReplyTo = *b.ReplyTo
	}
	if b.Host != nil {
		rec.Host = *b.Host
	}
	if b.Port != nil {
		rec.Port = *b.Port
	}
	if b.Security != nil {
		rec.Security = *b.Security
	}
	return rec
}

// ----- Get -----

type EmailProviderGetInput struct{}

type EmailProviderGetOutput struct {
	Body emailProviderResponse
}

func (h *EmailProvider) Get(ctx context.Context, _ *EmailProviderGetInput) (*EmailProviderGetOutput, error) {
	cfg, found, err := h.svc.EmailProvider.Get(ctx)
	if err != nil {
		return nil, err
	}
	if !found {
		// Unconfigured is a 200 with configured:false, not a 404.
		return &EmailProviderGetOutput{Body: emailProviderResponse{Configured: false}}, nil
	}
	return &EmailProviderGetOutput{Body: toEmailProviderResponse(cfg)}, nil
}

// ----- Save -----

type EmailProviderSaveInput struct {
	Body emailProviderWriteBody
}

type EmailProviderSaveOutput struct {
	Body emailProviderResponse
}

func (h *EmailProvider) Save(ctx context.Context, input *EmailProviderSaveInput) (*EmailProviderSaveOutput, error) {
	// Empty-string password is a no-op signal: keep existing.
	var password *string
	if input.Body.Password != nil && *input.Body.Password != "" {
		password = input.Body.Password
	}

	cfg, err := h.svc.EmailProvider.Save(ctx, service.SaveEmailProviderParams{
		Provider:      input.Body.Provider,
		FromAddress:   input.Body.FromAddress,
		FromName:      input.Body.FromName,
		ReplyTo:       input.Body.ReplyTo,
		Host:          input.Body.Host,
		Port:          input.Body.Port,
		Security:      input.Body.Security,
		Auth:          input.Body.Auth,
		Username:      input.Body.Username,
		Password:      password,
		SkipTLSVerify: input.Body.SkipTLSVerify,
		Enabled:       input.Body.Enabled,
	})
	if err != nil {
		return nil, err
	}

	// Now that a usable relay exists, drain the recent backlog of email parked as
	// awaiting_config while SMTP was unconfigured. Best-effort (mirrors the
	// downloader InitializeDownloader hook): a drain failure never fails the save,
	// and the worker requeues on its next poll regardless.
	if cfg.Enabled {
		_, _ = h.svc.Notifications.RequeueAwaitingConfig(ctx, time.Now().Add(-service.AwaitingConfigDrainWindow))
	}

	return &EmailProviderSaveOutput{Body: toEmailProviderResponse(cfg)}, nil
}

// ----- Test send (shared) -----

// testEmailText is the plain-text alternative for the house-styled test email —
// the multipart fallback for clients that don't render the HTML body.
const testEmailText = "If you're reading this, your Arrflix email configuration is working. No action is needed."

// sendTestEmail renders the shared house-styled test email and sends it via the
// given transport. A render failure is Internal (a broken embedded template); a
// send failure is BadGateway, carrying the relay's own error to the operator.
func (h *EmailProvider) sendTestEmail(ctx context.Context, transport email.Transport, to string) error {
	const op = "EmailProviderHandler.sendTestEmail"
	subject, htmlBody, err := h.svc.Notifications.RenderTestEmail()
	if err != nil {
		return apperrors.Internalf("render test email: %v", err).Op(op)
	}
	if err := transport.Send(ctx, email.Message{
		To:       []string{to},
		Subject:  subject,
		TextBody: testEmailText,
		HTMLBody: htmlBody,
	}); err != nil {
		return apperrors.BadGatewayf("%v", err).Op(op)
	}
	return nil
}

// ----- Test (stored config) -----

type EmailProviderTestBody struct {
	To string `json:"to" required:"true" minLength:"1" doc:"Recipient address for the test email"`
}

type EmailProviderTestInput struct {
	Body EmailProviderTestBody
}

type EmailProviderTestOutput struct{}

func (h *EmailProvider) Test(ctx context.Context, input *EmailProviderTestInput) (*EmailProviderTestOutput, error) {
	transport, err := h.emailManager.BuildFromStored(ctx)
	if err != nil {
		return nil, apperrors.BadGatewayf("%v", err).Op("EmailProviderHandler.Test")
	}

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := h.sendTestEmail(testCtx, transport, input.Body.To); err != nil {
		return nil, err
	}
	return &EmailProviderTestOutput{}, nil
}

// ----- TestConfig (unsaved config) -----

// EmailProviderTestConfigBody re-declares the write fields inline (rather than
// embedding emailProviderWriteBody) because huma does not flatten an embedded
// struct into the request schema — an anonymous field would be dropped, leaving
// additionalProperties:false to reject the config on the wire. This mirrors
// DownloadersTestConfigBody.
type EmailProviderTestConfigBody struct {
	Provider      string  `json:"provider" required:"true" enum:"smtp" doc:"Email provider implementation"`
	FromAddress   string  `json:"fromAddress" required:"true" minLength:"1" doc:"From/envelope address"`
	FromName      *string `json:"fromName,omitempty" doc:"Optional display name for the From address"`
	ReplyTo       *string `json:"replyTo,omitempty" doc:"Optional Reply-To address"`
	Host          *string `json:"host,omitempty" doc:"SMTP server hostname"`
	Port          *int    `json:"port,omitempty" doc:"SMTP server port (1-65535)"`
	Security      *string `json:"security,omitempty" enum:"starttls,implicit_tls,none" doc:"Transport security mode"`
	Auth          bool    `json:"auth" doc:"Whether the server requires SMTP authentication"`
	Username      *string `json:"username,omitempty" doc:"SMTP username; required when auth is enabled"`
	Password      *string `json:"password,omitempty" doc:"SMTP password"`
	SkipTLSVerify bool    `json:"skipTlsVerify" doc:"Skip TLS certificate verification (self-signed relays)"`
	To            string  `json:"to" required:"true" minLength:"1" doc:"Recipient address for the test email"`
}

type EmailProviderTestConfigInput struct {
	Body EmailProviderTestConfigBody
}

type EmailProviderTestConfigOutput struct{}

func (h *EmailProvider) TestConfig(ctx context.Context, input *EmailProviderTestConfigInput) (*EmailProviderTestConfigOutput, error) {
	rec := recordFromWriteBody(emailProviderWriteBody{
		Provider:      input.Body.Provider,
		FromAddress:   input.Body.FromAddress,
		FromName:      input.Body.FromName,
		ReplyTo:       input.Body.ReplyTo,
		Host:          input.Body.Host,
		Port:          input.Body.Port,
		Security:      input.Body.Security,
		Auth:          input.Body.Auth,
		Username:      input.Body.Username,
		Password:      input.Body.Password,
		SkipTLSVerify: input.Body.SkipTLSVerify,
	})

	transport, err := h.emailManager.BuildFromConfig(rec)
	if err != nil {
		return nil, apperrors.BadGatewayf("%v", err).Op("EmailProviderHandler.TestConfig")
	}

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := h.sendTestEmail(testCtx, transport, input.Body.To); err != nil {
		return nil, err
	}
	return &EmailProviderTestConfigOutput{}, nil
}

// ----- Register -----

func (h *EmailProvider) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "email-provider-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/settings/email",
		Summary:     "Get email provider configuration",
		Tags:        []string{"settings"},
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "email-provider-save",
		Method:      http.MethodPut,
		Path:        "/api/v1/settings/email",
		Summary:     "Save email provider configuration",
		Tags:        []string{"settings"},
		Errors:      errsUpsert,
	}, h.Save)

	huma.Register(api, huma.Operation{
		OperationID: "email-provider-test",
		Method:      http.MethodPost,
		Path:        "/api/v1/settings/email/test",
		Summary:     "Send a test email using the stored configuration",
		Tags:        []string{"settings"},
		Errors:      errs(errsRead, errsUpstream),
	}, h.Test)

	huma.Register(api, huma.Operation{
		OperationID: "email-provider-test-config",
		Method:      http.MethodPost,
		Path:        "/api/v1/settings/email/test-config",
		Summary:     "Send a test email using an unsaved configuration",
		Tags:        []string{"settings"},
		Errors:      errs(errsWrite, errsUpstream),
	}, h.TestConfig)
}
