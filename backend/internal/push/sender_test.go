package push

import (
	"errors"
	"testing"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

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
			err := classify(tt.status)
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
