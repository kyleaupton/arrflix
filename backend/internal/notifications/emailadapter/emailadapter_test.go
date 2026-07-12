package emailadapter

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/email"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
)

// fakeTransport records the messages Send is asked to deliver and returns a
// canned error, so a test can assert what was sent and drive the retry branches.
type fakeTransport struct {
	sent []email.Message
	err  error
}

func (t *fakeTransport) Provider() email.Provider           { return email.ProviderSMTP }
func (t *fakeTransport) Test(context.Context, string) error { return nil }
func (t *fakeTransport) Send(_ context.Context, m email.Message) error {
	t.sent = append(t.sent, m)
	return t.err
}

// fakeMailer stands in for *email.Manager: a fixed configured flag and a canned
// transport (or build error).
type fakeMailer struct {
	configured bool
	transport  email.Transport
	buildErr   error
}

func (m fakeMailer) IsConfigured(context.Context) bool { return m.configured }
func (m fakeMailer) BuildFromStored(context.Context) (email.Transport, error) {
	return m.transport, m.buildErr
}

// fakeUsers resolves every id to one canned user (or error).
type fakeUsers struct {
	user model.User
	err  error
}

func (u fakeUsers) GetUserByID(context.Context, uuid.UUID) (model.User, error) {
	return u.user, u.err
}

func emailPtr(s string) *string { return &s }

func wantAvailableRow(t *testing.T, recipient *uuid.UUID) model.NotificationOutbox {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"media":    map[string]any{"title": "Sentinel", "year": 2024},
		"plexLink": "https://plex/watch/1",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return model.NotificationOutbox{
		ID:              uuid.New(),
		EventType:       "want.available",
		Channel:         string(model.ChannelEmail),
		RecipientUserID: recipient,
		Payload:         payload,
	}
}

func TestAdapter_IsConfigured(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := notifications.MustNewRenderer()

	on := New(fakeMailer{configured: true}, r, fakeUsers{})
	if !on.IsConfigured(ctx) {
		t.Fatal("IsConfigured = false, want true when the manager reports configured")
	}
	off := New(fakeMailer{configured: false}, r, fakeUsers{})
	if off.IsConfigured(ctx) {
		t.Fatal("IsConfigured = true, want false when the manager reports unconfigured")
	}
}

// TestAdapter_DeliverSends proves the happy path: the adapter resolves the
// recipient, renders the email, and hands a well-formed Message to the transport.
func TestAdapter_DeliverSends(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	ft := &fakeTransport{}
	a := New(
		fakeMailer{configured: true, transport: ft},
		notifications.MustNewRenderer(),
		fakeUsers{user: model.User{ID: id, Email: emailPtr("dest@test.local")}},
	)

	if err := a.Deliver(ctx, wantAvailableRow(t, &id)); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if len(ft.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(ft.sent))
	}
	msg := ft.sent[0]
	if len(msg.To) != 1 || msg.To[0] != "dest@test.local" {
		t.Fatalf("To = %v, want [dest@test.local]", msg.To)
	}
	if msg.Subject != "Sentinel is ready to watch" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
	if msg.HTMLBody == "" {
		t.Fatal("HTMLBody is empty, want the rendered HTML")
	}
}

// TestAdapter_DeliverPermanentWithoutEmail proves a recipient with no usable
// address is a permanent (non-retryable) failure — nothing is sent, and the
// worker will mark the row dead rather than retry forever. Covers both a nil
// recipient id and a resolved user with a nil/empty email.
func TestAdapter_DeliverPermanentWithoutEmail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	cases := []struct {
		name      string
		users     fakeUsers
		recipient *uuid.UUID
	}{
		{"nil recipient id", fakeUsers{user: model.User{Email: emailPtr("x@test.local")}}, nil},
		{"nil email", fakeUsers{user: model.User{ID: id, Email: nil}}, &id},
		{"empty email", fakeUsers{user: model.User{ID: id, Email: emailPtr("")}}, &id},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ft := &fakeTransport{}
			a := New(fakeMailer{configured: true, transport: ft}, notifications.MustNewRenderer(), tc.users)
			err := a.Deliver(ctx, wantAvailableRow(t, tc.recipient))
			if err == nil {
				t.Fatal("deliver = nil, want a permanent error")
			}
			if apperrors.IsRetryable(err) {
				t.Fatalf("error should be permanent (non-retryable), got retryable: %v", err)
			}
			if len(ft.sent) != 0 {
				t.Fatalf("sent %d messages, want 0 for an unaddressable recipient", len(ft.sent))
			}
		})
	}
}

// TestAdapter_DeliverRetriesTransportError proves a transient transport failure
// propagates as retryable, so the worker requeues with backoff rather than
// killing the row.
func TestAdapter_DeliverRetriesTransportError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := uuid.New()

	ft := &fakeTransport{err: apperrors.BadGatewayf("smtp connection reset")}
	a := New(
		fakeMailer{configured: true, transport: ft},
		notifications.MustNewRenderer(),
		fakeUsers{user: model.User{ID: id, Email: emailPtr("dest@test.local")}},
	)

	err := a.Deliver(ctx, wantAvailableRow(t, &id))
	if err == nil {
		t.Fatal("deliver = nil, want the transport error")
	}
	if !apperrors.IsRetryable(err) {
		t.Fatalf("transport blip should be retryable, got: %v", err)
	}
}

var _ notifications.ChannelAdapter = (*Adapter)(nil)
