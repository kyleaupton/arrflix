package push

import (
	"errors"
	"strings"
	"testing"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

// The sub claim webpush-go signs is the whole reason iOS push can silently fail:
// the library mailto:-prefixes anything that isn't an https URL, so a subject
// already carrying the prefix signs as "mailto:mailto:…" and Apple 403s it.
func TestSubscriberFor(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		want    string
	}{
		{"mailto prefix stripped for the library to re-add", "mailto:admin@example.com", "admin@example.com"},
		{"bare email passes through", "admin@example.com", "admin@example.com"},
		{"https url passes through untouched", "https://github.com/kyleaupton/arrflix", "https://github.com/kyleaupton/arrflix"},
		{"default subject is left alone", DefaultSubject, DefaultSubject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subscriberFor(tt.subject); got != tt.want {
				t.Fatalf("subscriberFor(%q) = %q, want %q", tt.subject, got, tt.want)
			}
		})
	}
}

// The upstream's reason is the difference between an actionable error and a bare
// status, so a rejection must carry it through to the caller.
func TestClassifyIncludesReason(t *testing.T) {
	err := classify(403, `{"reason":"BadJwtToken"}`)
	if err == nil {
		t.Fatal("classify(403) = nil, want error")
	}
	if !strings.Contains(err.Error(), "BadJwtToken") {
		t.Fatalf("classify(403) = %q, want it to quote the upstream reason", err)
	}
	if apperrors.IsRetryable(err) {
		t.Fatalf("classify(403) is retryable; a malformed sub claim won't fix itself")
	}
}

func TestReasonIsBounded(t *testing.T) {
	got := reason(strings.NewReader(strings.Repeat("x", maxReasonBytes*2)))
	if len(got) != maxReasonBytes {
		t.Fatalf("reason() length = %d, want %d", len(got), maxReasonBytes)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		wantGone  bool
		wantErr   bool
		wantRetry bool // only meaningful when wantErr && !wantGone
	}{
		{"201 created delivered", 201, false, false, false},
		{"200 ok delivered", 200, false, false, false},
		{"404 gone", 404, true, true, false},
		{"410 gone", 410, true, true, false},
		{"429 too many requests retryable", 429, false, true, true},
		{"500 retryable", 500, false, true, true},
		{"503 retryable", 503, false, true, true},
		{"400 bad request permanent", 400, false, true, false},
		{"413 payload too large permanent", 413, false, true, false},
		{"403 forbidden permanent", 403, false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classify(tt.status, "")
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("classify(%d) = %v, want nil", tt.status, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("classify(%d) = nil, want error", tt.status)
			}
			if gone := errors.Is(err, ErrSubscriptionGone); gone != tt.wantGone {
				t.Fatalf("classify(%d) gone = %v, want %v", tt.status, gone, tt.wantGone)
			}
			if tt.wantGone {
				return
			}
			if retry := apperrors.IsRetryable(err); retry != tt.wantRetry {
				t.Fatalf("classify(%d) retryable = %v, want %v", tt.status, retry, tt.wantRetry)
			}
		})
	}
}
