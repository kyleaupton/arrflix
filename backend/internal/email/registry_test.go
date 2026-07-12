package email_test

import (
	"testing"

	"github.com/kyleaupton/arrflix/internal/email"
	"github.com/kyleaupton/arrflix/internal/email/smtp"
)

func TestRegistry_BuildsSMTP(t *testing.T) {
	reg := email.NewRegistry()
	smtp.Register(reg)

	tr, err := reg.Build(email.ConfigRecord{
		Provider:    email.ProviderSMTP,
		FromAddress: "from@example.com",
		Host:        "smtp.example.com",
		Port:        587,
		Security:    "starttls",
	})
	if err != nil {
		t.Fatalf("build smtp transport: %v", err)
	}
	if tr == nil {
		t.Fatal("build returned a nil transport")
	}
	if got := tr.Provider(); got != email.ProviderSMTP {
		t.Errorf("transport provider = %q, want %q", got, email.ProviderSMTP)
	}
}

func TestRegistry_UnknownProvider(t *testing.T) {
	reg := email.NewRegistry()
	smtp.Register(reg)

	if _, err := reg.Build(email.ConfigRecord{Provider: "resend"}); err == nil {
		t.Fatal("expected an error building an unregistered provider, got nil")
	}
}
