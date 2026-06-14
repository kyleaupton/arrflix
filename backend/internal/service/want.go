package service

import (
	"context"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// WantService is the user-driven write surface over a want's lifecycle. Today
// that's cancel; pause/resume and manual-grab unification land here later. It
// delegates download-job cancellation to DownloadJobsService so the (currently
// DB-only) cancel machinery lives in one place.
type WantService struct {
	repo         *repo.Repository
	downloadJobs *DownloadJobsService
}

func NewWantService(r *repo.Repository, downloadJobs *DownloadJobsService) *WantService {
	return &WantService{repo: r, downloadJobs: downloadJobs}
}

// CancelWant flips a non-terminal want (and its tracking) to 'canceled' and
// cancels any in-flight download job + its pending import tasks. Idempotent on
// an already-canceled want; a conflict on a want already terminal for another
// reason ('available'/'failed').
func (s *WantService) CancelWant(ctx context.Context, id uuid.UUID) (model.Want, error) {
	want, err := s.repo.GetWant(ctx, id) // 404 flows through
	if err != nil {
		return model.Want{}, err
	}

	canceled, ok, err := s.repo.CancelWant(ctx, id) // CAS: only from non-terminal
	if err != nil {
		return model.Want{}, err
	}
	if !ok {
		if want.Status == string(model.WantCanceled) {
			return want, nil // idempotent
		}
		return model.Want{}, apperrors.Conflictf("cannot cancel want in terminal state %q", want.Status).
			Op("WantService.CancelWant")
	}

	// Mirror the terminal state onto the tracking so the UI shows "Canceled".
	if _, err := s.repo.SetTrackingState(ctx, canceled.TrackingID, "canceled"); err != nil {
		return model.Want{}, err
	}

	jobs, err := s.repo.ListActiveDownloadJobsByWant(ctx, id)
	if err != nil {
		return model.Want{}, err
	}
	for _, j := range jobs {
		// NOTE: cancel is DB-only — the in-flight transfer is left running in the
		// downloader client (matches DownloadJobsService.Cancel). When client-side
		// removal (Client.Remove) is added, do it INSIDE DownloadJobsService.Cancel
		// so both the job-cancel and want-cancel endpoints inherit it — and
		// re-check authorization at that point: removing a live transfer (and
		// deleting data) is more destructive than flipping a DB row, so it must not
		// stay open to "any authenticated user" once per-action authz lands.
		if err := s.downloadJobs.Cancel(ctx, j.ID); err != nil {
			return model.Want{}, err
		}
	}

	return canceled, nil
}
