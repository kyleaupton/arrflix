package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
)

// TestRegisterPushSubscription_ValidationRejects locks the create gate: a
// subscription missing any of endpoint/p256dh/auth is a Validation error and
// never reaches the repo, so a nil repo is safe here.
func TestRegisterPushSubscription_ValidationRejects(t *testing.T) {
	t.Parallel()

	svc := NewNotificationService(nil)
	ctx := context.Background()

	cases := []struct {
		name     string
		endpoint string
		p256dh   string
		auth     string
	}{
		{"empty endpoint", "", "key", "secret"},
		{"empty p256dh", "https://push.test/x", "", "secret"},
		{"empty auth", "https://push.test/x", "key", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := svc.RegisterPushSubscription(ctx, uuid.New(), c.endpoint, c.p256dh, c.auth, nil)
			if !apperrors.IsValidation(err) {
				t.Fatalf("expected KindValidation, got %v", err)
			}
		})
	}
}

// TestSetPreference_ValidationRejects locks the write gate: only a write to a known
// user bundle targeting the subscription master or a known outbound channel is
// accepted. A bad bundle or target is a Validation error naming the offending field.
// The rejection paths never reach the repo, so a nil repo is safe here.
func TestSetPreference_ValidationRejects(t *testing.T) {
	t.Parallel()

	svc := NewNotificationService(nil)
	ctx := context.Background()

	cases := []struct {
		name   string
		bundle string
		target string
		field  string
	}{
		{"unknown bundle rejected", "not_a_bundle", "subscribed", "body.bundle"},
		{"garbage target rejected", notifications.BundleMyRequests, "carrier-pigeon", "body.target"},
		{"in_app is not a target", notifications.BundleMyRequests, string(model.ChannelInApp), "body.target"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := svc.SetPreference(ctx, uuid.New(), c.bundle, c.target, true)
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
