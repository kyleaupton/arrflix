package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/titlestatus"
)

// activeDownloadStatuses are the download-job statuses that mean a transfer is
// still underway. Mirrors the non-terminal set the download-job queries filter
// on; note download_job spells it "cancelled" while wants spell it "canceled".
var activeDownloadStatuses = map[string]bool{
	"created":     true,
	"enqueued":    true,
	"downloading": true,
}

// TitleStatusService assembles the acquisition read model for one title and one
// viewer. It is the impure half of the projection: it gathers wants, files,
// jobs, and the viewer's request, then hands them to the pure derivation in
// internal/titlestatus, which decides what state they add up to.
//
// Every "not found" along the way is a normal state, not an error. A title
// nobody has requested has no media item, no tracking, and no wants — and the
// honest answer for it is not_requested, not a 404.
type TitleStatusService struct {
	repo *repo.Repository
	// now is injectable so tests can pin "is this episode aired yet?" instead of
	// racing the wall clock.
	now func() time.Time
}

func NewTitleStatusService(r *repo.Repository) *TitleStatusService {
	return &TitleStatusService{repo: r, now: time.Now}
}

// Get builds the projection for (mediaType, tmdbID) as seen by viewerID.
func (s *TitleStatusService) Get(ctx context.Context, viewerID uuid.UUID, mediaType model.MediaType, tmdbID int64) (model.TitleStatus, error) {
	in := titlestatus.Input{
		MediaType: titlestatus.MediaType(mediaType),
		Now:       s.now(),
	}

	req, err := s.viewerRequest(ctx, viewerID, mediaType, tmdbID)
	if err != nil {
		return model.TitleStatus{}, err
	}
	in.Request = req

	item, err := s.repo.GetMediaItemByTmdbIDAndType(ctx, tmdbID, string(mediaType))
	switch {
	case apperrors.IsNotFound(err):
		// Nothing local knows this title yet. A pending or denied request is
		// still meaningful, so derive from that alone rather than 404ing.
		return s.finish(in, nil, tmdbID, mediaType), nil
	case err != nil:
		return model.TitleStatus{}, err
	}

	wants, err := s.wantsForItem(ctx, item.ID)
	if err != nil {
		return model.TitleStatus{}, err
	}

	if mediaType == model.MediaTypeSeries {
		episodes, err := s.seriesItems(ctx, item.ID, tmdbID, wants)
		if err != nil {
			return model.TitleStatus{}, err
		}
		in.Items = make([]titlestatus.Item, 0, len(episodes))
		for _, e := range episodes {
			in.Items = append(in.Items, e.item)
		}
		return s.finish(in, episodes, tmdbID, mediaType), nil
	}

	movie, err := s.movieItem(ctx, item.ID, tmdbID, wants)
	if err != nil {
		return model.TitleStatus{}, err
	}
	in.Items = []titlestatus.Item{movie}
	return s.finish(in, nil, tmdbID, mediaType), nil
}

// finish runs the derivation and shapes the wire model. episodes is nil for
// movies; when present it is index-aligned with in.Items so each cell picks up
// its own derived state.
func (s *TitleStatusService) finish(in titlestatus.Input, episodes []episodeItem, tmdbID int64, mediaType model.MediaType) model.TitleStatus {
	res := titlestatus.Derive(in)

	out := model.TitleStatus{
		MediaType: string(mediaType),
		TmdbID:    tmdbID,
		State:     string(res.State),
		Phase:     string(res.Phase),
		Active:    res.Active,
		Library:   model.TitleLibrary{HasFiles: res.Library.HasFiles, FileCount: res.Library.FileCount},
		Counts: model.TitleCounts{
			Total:     res.Counts.Total,
			Available: res.Counts.Available,
			Working:   res.Counts.Working,
		},
	}

	if len(episodes) > 0 {
		out.Episodes = make([]model.TitleEpisodeStatus, 0, len(episodes))
		for i, e := range episodes {
			out.Episodes = append(out.Episodes, model.TitleEpisodeStatus{
				EpisodeID:     e.episodeID,
				SeasonNumber:  e.seasonNumber,
				EpisodeNumber: e.episodeNumber,
				State:         string(res.ItemStates[i]),
				AirDate:       e.item.ObtainableAt,
			})
		}
	}

	return out
}

// viewerRequest fetches the viewer's most recent request for the title. No
// request is the common case and yields nil, not an error.
func (s *TitleStatusService) viewerRequest(ctx context.Context, viewerID uuid.UUID, mediaType model.MediaType, tmdbID int64) (*titlestatus.Request, error) {
	if viewerID == uuid.Nil {
		return nil, nil
	}
	req, err := s.repo.FindLatestRequestForUser(ctx, viewerID, tmdbID, string(mediaType))
	switch {
	case apperrors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &titlestatus.Request{Status: req.Status}, nil
}

// wantsForItem returns the title's live wants keyed by episode id, with the
// zero UUID keying a movie's single want. An untracked title has none.
func (s *TitleStatusService) wantsForItem(ctx context.Context, mediaItemID uuid.UUID) (map[uuid.UUID]model.Want, error) {
	tracking, err := s.repo.FindTrackingByMediaItem(ctx, mediaItemID)
	switch {
	case apperrors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, err
	}

	wants, err := s.repo.ListWantsByTracking(ctx, tracking.ID)
	if err != nil {
		return nil, err
	}

	byEpisode := make(map[uuid.UUID]model.Want, len(wants))
	for _, w := range wants {
		key := uuid.Nil
		if w.EpisodeID != nil {
			key = *w.EpisodeID
		}
		byEpisode[key] = w
	}
	return byEpisode, nil
}

// movieItem builds the single atom for a movie.
//
// ObtainableAt is always nil: no obtainable date is persisted for a movie, so
// the projection cannot yet distinguish "not out yet" from "can't find it" on
// the movie path. That is REQ-UNREL-003, and it is a gap in the data rather
// than in this assembly — see specs/stories/09-not-out-yet.md.
func (s *TitleStatusService) movieItem(ctx context.Context, mediaItemID uuid.UUID, tmdbID int64, wants map[uuid.UUID]model.Want) (titlestatus.Item, error) {
	files, err := s.repo.ListFilesForItem(ctx, mediaItemID)
	if err != nil {
		return titlestatus.Item{}, err
	}

	item := titlestatus.Item{}
	for _, f := range files {
		if f.Exists != nil && *f.Exists {
			item.HasFile = true
			break
		}
	}

	if w, ok := wants[uuid.Nil]; ok {
		item.Want = &titlestatus.Want{Status: w.Status, Hold: w.Hold}
	}

	jobs, err := s.repo.ListDownloadJobsByTmdbMovieID(ctx, tmdbID)
	if err != nil {
		return titlestatus.Item{}, err
	}
	for _, j := range jobs {
		if activeDownloadStatuses[j.Status] {
			item.ActiveJob = true
			break
		}
	}

	item.InScope = inScope(item)
	return item, nil
}

// inScope reports whether an atom is one the title is trying to acquire or has
// already acquired.
//
// A want is the signal: wants are created eagerly for everything in scope, so
// having one means the title wants this atom. A file counts too — it may have
// been acquired before scope narrowed, or scanned in from disk — as does a live
// hand-grab, which is an acquisition without a want behind it.
//
// The effective scope itself is recomputed live from requester associations and
// is not materialized anywhere, so deriving it here would mean re-running that
// resolution on every read. This proxy reads the result of it instead.
func inScope(item titlestatus.Item) bool {
	return item.Want != nil || item.HasFile || item.ActiveJob
}

// episodeItem pairs a derived atom with the episode identity the wire model
// needs, so the two stay index-aligned through the derivation.
type episodeItem struct {
	episodeID     uuid.UUID
	seasonNumber  int32
	episodeNumber int32
	item          titlestatus.Item
}

// seriesItems builds one atom per live episode, in season/episode order.
// Deprecated episodes are skipped: they were removed upstream and are neither
// acquirable nor something a grid should show.
func (s *TitleStatusService) seriesItems(ctx context.Context, mediaItemID uuid.UUID, tmdbID int64, wants map[uuid.UUID]model.Want) ([]episodeItem, error) {
	episodes, err := s.repo.ListEpisodeAvailabilityForSeries(ctx, mediaItemID)
	if err != nil {
		return nil, err
	}

	jobs, err := s.repo.ListDownloadJobsByTmdbSeriesID(ctx, tmdbID)
	if err != nil {
		return nil, err
	}
	activeByEpisode := make(map[uuid.UUID]bool, len(jobs))
	for _, j := range jobs {
		if activeDownloadStatuses[j.Status] && j.EpisodeID != uuid.Nil {
			activeByEpisode[j.EpisodeID] = true
		}
	}

	out := make([]episodeItem, 0, len(episodes))
	for _, e := range episodes {
		if e.Deprecated {
			continue
		}

		item := titlestatus.Item{
			HasFile:      e.FileID != nil && e.FileExists != nil && *e.FileExists,
			ActiveJob:    activeByEpisode[e.EpisodeID],
			ObtainableAt: e.AirDate,
		}
		if w, ok := wants[e.EpisodeID]; ok {
			item.Want = &titlestatus.Want{Status: w.Status, Hold: w.Hold}
		}
		item.InScope = inScope(item)

		out = append(out, episodeItem{
			episodeID:     e.EpisodeID,
			seasonNumber:  e.SeasonNumber,
			episodeNumber: e.EpisodeNumber,
			item:          item,
		})
	}
	return out, nil
}
