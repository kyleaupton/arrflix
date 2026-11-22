package service

import (
	"context"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// WantService is the user-driven write surface over a want's lifecycle. Today
// that's cancel; pause/resume and manual-grab unification land here later.
type WantService struct {
	repo *repo.Repository
}

func NewWantService(r *repo.Repository) *WantService {
	return &WantService{repo: r}
}

// CancelWant is the operator's stop: it flips the want's tracking to 'canceled'
// and stands the want down the way the last requester's withdrawal does
// (standDownWant). A want still searching, or whose job was never sent, is
// cancelled with that job; a want whose download was already handed to the
// downloader is left to finish and be imported (story 07). Stopping a transfer is
// the download job's own cancel. Returns the want as it stands afterwards.
// Idempotent on an already-canceled want; a conflict on a want already terminal
// for another reason ('available'/'failed').
func (s *WantService) CancelWant(ctx context.Context, id uuid.UUID) (model.Want, error) {
	var out model.Want
	err := s.repo.InTx(ctx, func(r *repo.Repository) error {
		want, err := r.GetWant(ctx, id) // 404 flows through
		if err != nil {
			return err
		}
		// Tracking before want, the lock order withdrawal and the download
		// worker's releaseOrRetire use.
		if _, err := r.LockTracking(ctx, want.TrackingID); err != nil {
			return err
		}

		stood, canceled, err := standDownWant(ctx, r, id)
		if err != nil {
			return err
		}
		if !canceled && model.WantStatus(stood.Status).IsTerminal() {
			if stood.Status == string(model.WantCanceled) {
				out = stood // idempotent
				return nil
			}
			return apperrors.Conflictf("cannot cancel want in terminal state %q", stood.Status).
				Op("WantService.CancelWant")
		}

		// Mirror the stop onto the tracking so the UI shows "Canceled" and a
		// download left to finish isn't searched again if it fails.
		if _, err := r.SetTrackingState(ctx, stood.TrackingID, string(model.TrackingCanceled)); err != nil {
			return err
		}
		out = stood
		return nil
	})
	if err != nil {
		return model.Want{}, err
	}
	return out, nil
}

// cancelWantCascade cancels a non-terminal want, then cancels each of its active
// download jobs that no other live want still needs, with the job's pending
// import tasks. A season pack covering an episode someone still wants keeps
// running (REQ-CANCEL-002). The bool reports whether the want was cancelled:
// false means it was already terminal and nothing changed.
//
// r must be a transactional repo: the want, job and import-task writes commit
// together.
func cancelWantCascade(ctx context.Context, r *repo.Repository, wantID uuid.UUID) (model.Want, bool, error) {
	canceled, ok, err := r.CancelWant(ctx, wantID) // CAS: only from non-terminal
	if err != nil || !ok {
		return model.Want{}, false, err
	}

	jobs, err := r.ListActiveDownloadJobsByWant(ctx, wantID)
	if err != nil {
		return model.Want{}, false, err
	}
	for _, j := range jobs {
		live, err := r.CountLiveWantsForDownloadJob(ctx, j.ID)
		if err != nil {
			return model.Want{}, false, err
		}
		if live > 0 {
			continue
		}
		if _, err := cancelDownloadJob(ctx, r, j.ID); err != nil {
			// The job reached a terminal state between the list and the cancel;
			// its CAS matched 0 rows, surfaced as NotFound. That is the outcome
			// the cancel wanted.
			if apperrors.IsNotFound(err) {
				continue
			}
			return model.Want{}, false, err
		}
	}
	return canceled, true, nil
}

// standDownWant cancels a want nobody justifies any more (its last requester
// withdrew, or an operator stopped acquisition), unless its download is already
// committed: a want that is downloading or importing, or that has a job
// already handed to the downloader, is left to finish (REQ-CANCEL-004). A job
// still waiting to be sent ('created') is cancelled with its want. The bool
// reports whether the want was cancelled.
//
// The want and its jobs are row-locked before the decision, so a concurrent
// grab or download worker can't hand the job off between the check and the
// cancel. r must be a transactional repo.
func standDownWant(ctx context.Context, r *repo.Repository, wantID uuid.UUID) (model.Want, bool, error) {
	want, err := r.LockWant(ctx, wantID)
	if err != nil {
		return model.Want{}, false, err
	}
	switch model.WantStatus(want.Status) {
	case model.WantDownloading, model.WantImported:
		return want, false, nil
	}
	if model.WantStatus(want.Status).IsTerminal() {
		return want, false, nil
	}

	jobs, err := r.LockDownloadJobsByWant(ctx, wantID)
	if err != nil {
		return model.Want{}, false, err
	}
	for _, j := range jobs {
		if downloadJobHandedOff(j.Status) {
			return want, false, nil
		}
	}
	return cancelWantCascade(ctx, r, wantID)
}

// downloadJobHandedOff reports whether a download job's status means it was
// already given to the downloader: 'enqueued' onward, short of a failure or
// cancel.
func downloadJobHandedOff(status string) bool {
	switch status {
	case "enqueued", "downloading", "completed":
		return true
	default:
		return false
	}
}

// cancelDownloadJob cancels a download job and its pending import tasks, and
// returns the cancelled job. A job already terminal surfaces as NotFound from the
// CAS. r must be a transactional repo so the two writes commit together.
func cancelDownloadJob(ctx context.Context, r *repo.Repository, jobID uuid.UUID) (model.DownloadJob, error) {
	job, err := r.CancelDownloadJob(ctx, jobID)
	if err != nil {
		return model.DownloadJob{}, err
	}
	return job, r.CancelPendingImportTasksForJob(ctx, jobID)
}
