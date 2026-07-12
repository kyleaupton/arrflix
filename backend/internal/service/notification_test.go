package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
)

// TestSetPreference_ValidationRejects locks the v1 write gate: only a bundle-scope
// write to a known user bundle on a deliverable channel is accepted. A bad scope,
// value, or channel is a Validation error naming the offending field. The
// rejection paths never reach the repo, so a nil repo is safe here.
func TestSetPreference_ValidationRejects(t *testing.T) {
	t.Parallel()

	svc := NewNotificationService(nil)
	ctx := context.Background()

	cases := []struct {
		name    string
		scope   string
		value   string
		channel string
		field   string
	}{
		{"event scope rejected", string(model.PreferenceScopeEvent), notifications.BundleMyRequests, string(model.ChannelInApp), "body.scope"},
		{"unknown bundle rejected", string(model.PreferenceScopeBundle), "not_a_bundle", string(model.ChannelInApp), "body.value"},
		{"push not deliverable yet", string(model.PreferenceScopeBundle), notifications.BundleMyRequests, string(model.ChannelPush), "body.channel"},
		{"garbage channel rejected", string(model.PreferenceScopeBundle), notifications.BundleMyRequests, "carrier-pigeon", "body.channel"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := svc.SetPreference(ctx, uuid.New(), c.scope, c.value, c.channel, true)
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
