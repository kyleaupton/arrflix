// Package jobutil holds the small plumbing the job workers (acquisition,
// download, import) share: want-status mirroring, retry backoff, and pointer
// adapters. It keeps each worker from re-implementing the same helpers and gives
// the want-mirror and backoff policies a single place to evolve.
package jobutil

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/realtime"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// Backoff is the exponential retry delay for a 1-based attempt number: 2^attempt
// seconds. Uncapped and jitter-free by design today — this is the single place
// to add a ceiling or jitter when the workers need one.
func Backoff(attempt int) time.Duration {
	return time.Duration(math.Pow(2, float64(attempt))) * time.Second
}

// MirrorWant reflects a download/import lifecycle transition onto the owning want
// and emits the SSE delta. It is the terminal-sticky mirror the download and
// import workers share: MirrorWantStatus's CAS leaves a want already
// 'available'/'failed'/'canceled' untouched, so a late transition can't
// resurrect a canceled want. A nil want id (the interactive/legacy path with no
// want) is a no-op, and a mirror failure is logged but never propagated — it must
// not break the pipeline.
func MirrorWant(ctx context.Context, r *repo.Repository, broker *sse.Broker, log *logger.Logger, wantID uuid.UUID, status model.WantStatus) {
	if wantID == uuid.Nil {
		return
	}
	want, ok, err := r.MirrorWantStatus(ctx, wantID, string(status))
	if err != nil {
		log.Warn().Err(err).Str("want_id", wantID.String()).Msg("failed to mirror want status")
		return
	}
	if !ok {
		return
	}
	realtime.Emit(ctx, broker, realtime.WantUpdated(want))
}

// Ptr returns a pointer to v, for wrapping scalars into the optional-pointer
// fields repo params expect.
func Ptr[T any](v T) *T { return &v }

// StrPtr returns nil for an empty string, else a pointer to s.
func StrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Deref returns the pointed-to string, or "" when the pointer is nil.
func Deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
