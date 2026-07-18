package push

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// sendTimeout caps a single push POST. Push services are HTTP endpoints; a hung
// one must not stall the delivery worker.
const sendTimeout = 10 * time.Second

// Manager owns the VAPID identity lifecycle and builds Senders from it. It holds
// no cache — like email.Manager it reads the stored config per operation, so a
// subject edit takes effect on the next delivery.
type Manager struct {
	repo *repo.Repository
}

// NewManager creates a push manager.
func NewManager(r *repo.Repository) *Manager {
	return &Manager{repo: r}
}

// EnsureConfig returns the singleton VAPID config, generating and persisting a
// fresh keypair the first time. Call once at boot: it makes the keypair a
// startup invariant so PublicKey/BuildSender never race a missing row, and keys
// are generated exactly once (regenerating would invalidate every existing
// subscription).
func (m *Manager) EnsureConfig(ctx context.Context) (model.VAPIDConfig, error) {
	cfg, found, err := m.repo.GetVAPIDConfig(ctx)
	if err != nil {
		return model.VAPIDConfig{}, err
	}
	if found {
		return cfg, nil
	}
	// GenerateVAPIDKeys returns (privateKey, publicKey) in that order.
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return model.VAPIDConfig{}, apperrors.Internalf("generate vapid keys: %v", err)
	}
	return m.repo.CreateVAPIDConfig(ctx, pub, priv, DefaultSubject)
}

// PublicKey returns the applicationServerKey the browser needs to subscribe.
func (m *Manager) PublicKey(ctx context.Context) (string, error) {
	cfg, found, err := m.repo.GetVAPIDConfig(ctx)
	if err != nil {
		return "", err
	}
	if !found {
		return "", apperrors.Internalf("vapid config not initialized").NotRetryable()
	}
	return cfg.PublicKey, nil
}

// BuildSender loads the stored VAPID config and returns a Sender bound to it.
// Errors if EnsureConfig hasn't run — a broken boot invariant, non-retryable.
func (m *Manager) BuildSender(ctx context.Context) (Sender, error) {
	cfg, found, err := m.repo.GetVAPIDConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, apperrors.Internalf("vapid config not initialized").NotRetryable()
	}
	return &vapidSender{cfg: cfg, client: &http.Client{Timeout: sendTimeout}}, nil
}

// SendTest delivers the canned TestMessage to one subscription synchronously —
// the diagnostic behind a "send a test to this device" button. It bypasses the
// outbox worker on purpose: the caller wants an immediate delivered/failed
// answer for this exact browser, not an eventually-drained queue row. Errors
// propagate verbatim so the handler can translate ErrSubscriptionGone (prune the
// dead row) apart from a transient/permanent push-service failure.
func (m *Manager) SendTest(ctx context.Context, sub model.PushSubscription) error {
	sender, err := m.BuildSender(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(TestMessage)
	if err != nil {
		return apperrors.Internalf("marshal push test message: %v", err).NotRetryable()
	}
	return sender.Send(ctx, sub, payload)
}
