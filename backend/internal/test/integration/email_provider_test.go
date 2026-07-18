//go:build integration

package integration

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/kyleaupton/arrflix/internal/test/dbtest"
	"github.com/kyleaupton/arrflix/internal/test/testapp"
)

// validSMTPBody is a complete SMTP write payload. Tests clone and mutate it.
func validSMTPBody() map[string]any {
	return map[string]any{
		"provider":      "smtp",
		"fromAddress":   "arrflix@example.com",
		"fromName":      "Arrflix",
		"host":          "smtp.example.com",
		"port":          587,
		"security":      "starttls",
		"auth":          true,
		"username":      "smtp-user",
		"password":      "smtp-secret",
		"skipTlsVerify": false,
		"enabled":       true,
	}
}

// assertWriteOnly pins the write-only wire contract: the response never carries
// the password, but reports hasPassword and configured.
func assertWriteOnly(t *testing.T, m map[string]any, wantHasPassword, wantConfigured bool) {
	t.Helper()
	if _, leaked := m["password"]; leaked {
		t.Errorf("response leaked a password field: %v", m["password"])
	}
	if got := m["hasPassword"]; got != wantHasPassword {
		t.Errorf("hasPassword = %v, want %v", got, wantHasPassword)
	}
	if got := m["configured"]; got != wantConfigured {
		t.Errorf("configured = %v, want %v", got, wantConfigured)
	}
}

// TestEmailProvider_SaveAndGet saves a full SMTP config and re-reads it, proving
// the round-trip persists the fields, reports configured/hasPassword, and never
// echoes the password (write-only wire contract).
func TestEmailProvider_SaveAndGet(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	var saved map[string]any
	app.PUT(t, "/api/v1/settings/email", validSMTPBody(), &saved, http.StatusOK)
	assertWriteOnly(t, saved, true, true)

	var got map[string]any
	app.GET(t, "/api/v1/settings/email", &got, http.StatusOK)
	assertWriteOnly(t, got, true, true)
	if got["host"] != "smtp.example.com" {
		t.Errorf("host = %v, want smtp.example.com", got["host"])
	}
	if got["provider"] != "smtp" {
		t.Errorf("provider = %v, want smtp", got["provider"])
	}
}

// TestEmailProvider_GetUnconfigured proves the unconfigured GET is a 200 with
// configured:false rather than a 404.
func TestEmailProvider_GetUnconfigured(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	var got map[string]any
	app.GET(t, "/api/v1/settings/email", &got, http.StatusOK)
	if got["configured"] != false {
		t.Errorf("configured = %v, want false for an unconfigured provider", got["configured"])
	}
	if _, leaked := got["password"]; leaked {
		t.Errorf("unconfigured response leaked a password field")
	}
}

// TestEmailProvider_KeepPasswordOnUpdate proves the write-only-password contract
// on update: re-saving with an empty password and a changed host preserves the
// stored secret. The secret is verified through the repo, since it never rides
// the wire.
func TestEmailProvider_KeepPasswordOnUpdate(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	var out map[string]any
	app.PUT(t, "/api/v1/settings/email", validSMTPBody(), &out, http.StatusOK)

	// Re-save with an empty password (the keep-existing signal) and a new host.
	upd := validSMTPBody()
	upd["password"] = ""
	upd["host"] = "smtp.changed.example.com"
	app.PUT(t, "/api/v1/settings/email", upd, &out, http.StatusOK)

	if out["host"] != "smtp.changed.example.com" {
		t.Errorf("host = %v, want the updated host", out["host"])
	}
	assertWriteOnly(t, out, true, true)

	// The secret is write-only on the wire, so read it back through the repo.
	cfg, found, err := app.Repo.GetEmailProvider(ctx)
	if err != nil {
		t.Fatalf("get email provider: %v", err)
	}
	if !found {
		t.Fatal("expected a persisted email provider row")
	}
	if cfg.Password == nil || *cfg.Password != "smtp-secret" {
		t.Errorf("stored password = %v, want the original secret preserved", cfg.Password)
	}
}

// TestEmailProvider_RequiresSettingsWrite proves the endpoints sit behind the
// admin.settings gate: a viewer is denied read and write (403), and an
// unauthenticated caller is a 401.
func TestEmailProvider_RequiresSettingsWrite(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)
	ctx := context.Background()

	_, viewer := app.UserToken(t, ctx, "viewer1", "viewer")

	if code := app.Status(t, viewer, http.MethodPut, "/api/v1/settings/email", validSMTPBody()); code != http.StatusForbidden {
		t.Errorf("viewer save = %d, want 403", code)
	}
	if code := app.Status(t, viewer, http.MethodGet, "/api/v1/settings/email", nil); code != http.StatusForbidden {
		t.Errorf("viewer get = %d, want 403", code)
	}
	if code := app.Status(t, "", http.MethodGet, "/api/v1/settings/email", nil); code != http.StatusUnauthorized {
		t.Errorf("unauthenticated get = %d, want 401", code)
	}
}

// TestEmailProvider_TestConfig_SurfacesError proves the raw-error UX: testing an
// unsaved config against a closed port returns 502 with the verbatim connection
// error in the problem detail — no live SMTP server required.
func TestEmailProvider_TestConfig_SurfacesError(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	app := testapp.New(t, pool)

	// Grab a port, then close it so the dial is guaranteed to be refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	closedPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	body := map[string]any{
		"provider":      "smtp",
		"fromAddress":   "arrflix@example.com",
		"host":          "127.0.0.1",
		"port":          closedPort,
		"security":      "none",
		"auth":          false,
		"skipTlsVerify": false,
		"to":            "dest@example.com",
	}

	var prob struct {
		Detail string `json:"detail"`
	}
	app.POST(t, "/api/v1/settings/email/test-config", body, &prob, http.StatusBadGateway)
	if prob.Detail == "" {
		t.Fatal("expected a verbatim connection error in the problem detail, got empty")
	}
	if !strings.Contains(strings.ToLower(prob.Detail), "connect") {
		t.Errorf("problem detail = %q, want the verbatim connection error", prob.Detail)
	}
}
