package service

import (
	"testing"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

// validEmailParams is a fully-valid SMTP payload the failure cases mutate one
// field at a time from.
func validEmailParams() SaveEmailProviderParams {
	return SaveEmailProviderParams{
		Provider:    "smtp",
		FromAddress: "from@example.com",
		Host:        ptr("smtp.example.com"),
		Port:        ptr(587),
		Security:    ptr("starttls"),
		Auth:        true,
		Username:    ptr("user"),
	}
}

func TestValidateEmailProviderInput_Valid(t *testing.T) {
	if err := validateEmailProviderInput(validEmailParams()); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	// auth=false drops the username requirement.
	noAuth := validEmailParams()
	noAuth.Auth = false
	noAuth.Username = nil
	if err := validateEmailProviderInput(noAuth); err != nil {
		t.Fatalf("valid no-auth input rejected: %v", err)
	}
}

func TestValidateEmailProviderInput_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(*SaveEmailProviderParams)
		field string
	}{
		{"missing from_address", func(p *SaveEmailProviderParams) { p.FromAddress = "" }, "body.fromAddress"},
		{"smtp missing host", func(p *SaveEmailProviderParams) { p.Host = nil }, "body.host"},
		{"port out of range", func(p *SaveEmailProviderParams) { p.Port = ptr(0) }, "body.port"},
		{"missing port", func(p *SaveEmailProviderParams) { p.Port = nil }, "body.port"},
		{"bad security", func(p *SaveEmailProviderParams) { p.Security = ptr("ssl") }, "body.security"},
		{"auth without username", func(p *SaveEmailProviderParams) { p.Username = nil }, "body.username"},
		{"bad provider", func(p *SaveEmailProviderParams) { p.Provider = "resend" }, "body.provider"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := validEmailParams()
			c.mut(&p)

			err := validateEmailProviderInput(p)
			if err == nil {
				t.Fatalf("expected a validation error, got nil")
			}
			if !apperrors.IsValidation(err) {
				t.Fatalf("expected KindValidation, got %v", err)
			}

			found := false
			for _, f := range apperrors.FieldsOf(err) {
				if f.Location == c.field {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected field %q in %v", c.field, apperrors.FieldsOf(err))
			}
		})
	}
}
