package smtp

import "github.com/kyleaupton/arrflix/internal/email"

// Build creates an SMTP transport from a config record.
func Build(rec email.ConfigRecord) (email.Transport, error) {
	t := &transport{
		fromAddress:   rec.FromAddress,
		fromName:      rec.FromName,
		replyTo:       rec.ReplyTo,
		host:          rec.Host,
		port:          rec.Port,
		security:      rec.Security,
		auth:          rec.Auth,
		skipTLSVerify: rec.SkipTLSVerify,
	}
	if rec.Username != nil {
		t.username = *rec.Username
	}
	if rec.Password != nil {
		t.password = *rec.Password
	}
	return t, nil
}

// Register registers the SMTP builder with the registry.
func Register(registry *email.Registry) {
	registry.Register(email.ProviderSMTP, Build)
}
