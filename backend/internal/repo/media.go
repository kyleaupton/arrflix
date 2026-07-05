package repo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
)

// LibraryQueryParams contains parameters for paginated library queries
type LibraryQueryParams struct {
	TypeFilter *string
	Search     *string
	SortBy     string
	SortDir    string
	PageSize   int32
	Offset     int32
}

// InboxQueryParams holds the paginated matcher-inbox query params.
// LibraryID and Outcome are optional filters.
type InboxQueryParams struct {
	LibraryID *uuid.UUID
	Outcome   *string
	PageSize  int32
	Offset    int32
}

type MediaRepo interface {
	// Media items
	ListMediaItems(ctx context.Context) ([]model.MediaItem, error)
	ListMediaItemsPaginated(ctx context.Context, params LibraryQueryParams) ([]model.MediaItem, error)
	CountMediaItems(ctx context.Context, typeFilter, search *string) (int64, error)
	GetMediaItem(ctx context.Context, id uuid.UUID) (model.MediaItem, error)
	GetMediaItemByTmdbID(ctx context.Context, tmdbID int64) (model.MediaItem, error)
	GetMediaItemByTmdbIDAndType(ctx context.Context, tmdbID int64, typ string) (model.MediaItem, error)
	CreateMediaItem(ctx context.Context, params CreateMediaItemParams) (model.MediaItem, error)
	UpsertMediaItem(ctx context.Context, params UpsertMediaItemParams) (model.MediaItem, error)
	UpdateMediaItem(ctx context.Context, params UpdateMediaItemParams) (model.MediaItem, error)
	DeleteMediaItem(ctx context.Context, id uuid.UUID) error

	// Metadata enrichment
	UpdateMediaItemMetadata(ctx context.Context, params UpdateMediaItemMetadataParams) (model.MediaItem, error)
	RecordMetadataFailure(ctx context.Context, id uuid.UUID, nextRefreshAt time.Time, errMsg string) error
	ListDueMediaItems(ctx context.Context, now time.Time, batchSize int32) ([]model.MediaItem, error)
	UpsertMediaMetadataSource(ctx context.Context, params UpsertMediaMetadataSourceParams) error
	UpsertMediaItemExternalID(ctx context.Context, mediaItemID uuid.UUID, source, externalID string) error

	// Seasons
	ListSeasonsForMedia(ctx context.Context, mediaItemID uuid.UUID) ([]model.MediaSeason, error)
	GetSeason(ctx context.Context, id uuid.UUID) (model.MediaSeason, error)
	GetSeasonByNumber(ctx context.Context, mediaItemID uuid.UUID, seasonNumber int32) (model.MediaSeason, error)
	UpsertSeason(ctx context.Context, params UpsertSeasonParams) (model.MediaSeason, error)

	// Episodes
	ListEpisodesForSeason(ctx context.Context, seasonID uuid.UUID) ([]model.MediaEpisode, error)
	GetEpisode(ctx context.Context, id uuid.UUID) (model.MediaEpisode, error)
	GetEpisodeByNumber(ctx context.Context, seasonID uuid.UUID, episodeNumber int32) (model.MediaEpisode, error)
	UpsertEpisode(ctx context.Context, params UpsertEpisodeParams) (model.MediaEpisode, error)
	DeprecateRemovedEpisodes(ctx context.Context, mediaItemID uuid.UUID, syncedTmdbIDs []int64) error

	// File path loading for scanner
	ListFilePathsForLibrary(ctx context.Context, libraryID uuid.UUID) ([]string, error)

	// Files. Identity is nullable; "unmatched" is media_item_id IS NULL.
	// Reads filter deleted_at IS NULL.
	GetFile(ctx context.Context, id uuid.UUID) (model.File, error)
	GetFileByLibraryAndPath(ctx context.Context, params GetFileByLibraryAndPathParams) (model.File, error)
	CreateFile(ctx context.Context, params CreateFileParams) (model.File, error)
	SetFileIdentity(ctx context.Context, params SetFileIdentityParams) (model.File, error)
	ClearFileIdentity(ctx context.Context, id uuid.UUID) (model.File, error)
	SoftDeleteFile(ctx context.Context, id uuid.UUID) (model.File, error)
	ListFilesForItem(ctx context.Context, mediaItemID uuid.UUID) ([]model.FileWithState, error)
	ListInboxItems(ctx context.Context, params InboxQueryParams) ([]model.InboxItem, error)
	GetInboxItem(ctx context.Context, fileID uuid.UUID) (model.InboxItem, error)
	CountInboxByOutcome(ctx context.Context, libraryID *uuid.UUID) (map[string]int64, error)
	ListEpisodeAvailabilityForSeries(ctx context.Context, mediaItemID uuid.UUID) ([]model.SeriesEpisodeAvailability, error)

	// File state
	CreateFileState(ctx context.Context, params CreateFileStateParams) (model.FileState, error)
	UpsertFileState(ctx context.Context, params UpsertFileStateParams) (model.FileState, error)
	GetFileState(ctx context.Context, fileID uuid.UUID) (model.FileState, error)
	UpdateFileState(ctx context.Context, params UpdateFileStateParams) (model.FileState, error)
	ListMissingFiles(ctx context.Context) ([]model.FileWithState, error)
	ListFilesNeedingVerification(ctx context.Context, beforeTime time.Time, limit int32) ([]model.FileWithState, error)

	// File imports
	CreateFileImport(ctx context.Context, params CreateFileImportParams) (model.FileImport, error)
	GetFileImport(ctx context.Context, id uuid.UUID) (model.FileImport, error)
	ListImportsForFile(ctx context.Context, fileID uuid.UUID) ([]model.FileImport, error)
	ListImportsForImportTask(ctx context.Context, importTaskID uuid.UUID) ([]model.FileImport, error)
	ListRecentImports(ctx context.Context, limit int32) ([]model.FileImport, error)
	ListFailedImports(ctx context.Context, limit int32) ([]model.FileImport, error)
}

// CreateMediaItemParams is the domain-shaped input for CreateMediaItem. Mirrors
// the writeable subset of model.MediaItem (omits server-managed ID/CreatedAt/UpdatedAt
// and metadata fields populated by enrichment).
type CreateMediaItemParams struct {
	Type   string
	Title  string
	Year   *int32
	TmdbID *int64
}

// UpsertMediaItemParams is the domain-shaped input for UpsertMediaItem. Mirrors
// the writeable subset of model.MediaItem.
type UpsertMediaItemParams struct {
	Type   string
	Title  string
	Year   *int32
	TmdbID *int64
}

// UpdateMediaItemParams is the domain-shaped input for UpdateMediaItem. Mirrors
// the writeable subset of model.MediaItem plus the target ID.
type UpdateMediaItemParams struct {
	ID     uuid.UUID
	Title  string
	Year   *int32
	TmdbID *int64
}

// UpdateMediaItemMetadataParams is the domain-shaped input for UpdateMediaItemMetadata.
// Mirrors the metadata subset of model.MediaItem populated by enrichment.
// MetadataUpdatedAt and UpdatedAt are server-managed (set to now() in SQL) and
// not exposed here.
type UpdateMediaItemMetadataParams struct {
	ID            uuid.UUID
	PosterPath    *string
	BackdropPath  *string
	Overview      *string
	VoteAverage   *float64
	VoteCount     *int32
	Runtime       *int32
	Status        *string
	Certification *string
	Genres        []model.Genre
	ReleaseDate   *time.Time
	LastAirDate   *time.Time
	InProduction  *bool
	// NextRefreshAt is the state-derived due-time the caller computes from the
	// item's metadata (via cadence.MetadataRefreshAt). The success-path SQL also
	// clears the failure columns, so this method is only correct on a successful
	// materialization.
	NextRefreshAt *time.Time
}

// UpsertMediaMetadataSourceParams is the domain-shaped input for
// UpsertMediaMetadataSource. Mirrors the writeable subset of the
// media_metadata_source row.
type UpsertMediaMetadataSourceParams struct {
	MediaItemID uuid.UUID
	Source      string
	Data        json.RawMessage
}

// toModelMediaItem translates the persistence-shaped dbgen.MediaItem into the
// domain-shaped model.MediaItem. Lives next to the methods that use it.
func toModelMediaItem(row dbgen.MediaItem) model.MediaItem {
	m := model.MediaItem{
		ID:                   uuidFromPgtype(row.ID),
		Type:                 row.Type,
		SeriesType:           row.SeriesType,
		Title:                row.Title,
		Year:                 row.Year,
		TmdbID:               row.TmdbID,
		PosterPath:           row.PosterPath,
		BackdropPath:         row.BackdropPath,
		Overview:             row.Overview,
		VoteAverage:          row.VoteAverage,
		VoteCount:            row.VoteCount,
		Runtime:              row.Runtime,
		Status:               row.Status,
		Certification:        row.Certification,
		InProduction:         row.InProduction,
		MetadataLastError:    row.MetadataLastError,
		MetadataAttemptCount: row.MetadataAttemptCount,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
	if len(row.Genres) > 0 {
		var genres []model.Genre
		if err := json.Unmarshal(row.Genres, &genres); err == nil {
			m.Genres = genres
		}
		// Silent-empty on parse failure (legacy rows with TMDB's {id,name} shape will fall here).
	}
	if row.ReleaseDate.Valid {
		t := row.ReleaseDate.Time
		m.ReleaseDate = &t
	}
	if row.LastAirDate.Valid {
		t := row.LastAirDate.Time
		m.LastAirDate = &t
	}
	if row.MetadataUpdatedAt.Valid {
		t := row.MetadataUpdatedAt.Time
		m.MetadataUpdatedAt = &t
	}
	if row.NextRefreshAt.Valid {
		t := row.NextRefreshAt.Time
		m.NextRefreshAt = &t
	}
	if row.MetadataLastAttemptedAt.Valid {
		t := row.MetadataLastAttemptedAt.Time
		m.MetadataLastAttemptedAt = &t
	}
	return m
}

// pgDateFromTimePtr converts a *time.Time into the pgtype.Date shape that
// SQLC-generated query parameters expect. Nil maps to a NULL-shaped value.
func pgDateFromTimePtr(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{Valid: false}
	}
	return pgtype.Date{Time: *t, Valid: true}
}

// pgTimestamptzFromTimePtr converts a *time.Time into the pgtype.Timestamptz
// shape SQLC parameters expect. Nil maps to a NULL-shaped value.
func pgTimestamptzFromTimePtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{Valid: false}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func (r *Repository) ListMediaItems(ctx context.Context) ([]model.MediaItem, error) {
	rows, err := r.Q.ListMediaItems(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list media items")
	}
	out := make([]model.MediaItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMediaItem(row))
	}
	return out, nil
}

func (r *Repository) ListMediaItemsPaginated(ctx context.Context, params LibraryQueryParams) ([]model.MediaItem, error) {
	rows, err := r.Q.ListMediaItemsPaginated(ctx, dbgen.ListMediaItemsPaginatedParams{
		TypeFilter: params.TypeFilter,
		Search:     params.Search,
		SortBy:     params.SortBy,
		SortDir:    params.SortDir,
		PageSize:   params.PageSize,
		OffsetVal:  params.Offset,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "list media items paginated")
	}
	out := make([]model.MediaItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMediaItem(row))
	}
	return out, nil
}

func (r *Repository) CountMediaItems(ctx context.Context, typeFilter, search *string) (int64, error) {
	count, err := r.Q.CountMediaItems(ctx, dbgen.CountMediaItemsParams{
		TypeFilter: typeFilter,
		Search:     search,
	})
	return count, apperrors.FromPg(err, "count media items")
}

func (r *Repository) GetMediaItem(ctx context.Context, id uuid.UUID) (model.MediaItem, error) {
	row, err := r.Q.GetMediaItem(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.MediaItem{}, apperrors.FromPg(err, "media item %s not found", id)
	}
	return toModelMediaItem(row), nil
}

func (r *Repository) GetMediaItemByTmdbID(ctx context.Context, tmdbID int64) (model.MediaItem, error) {
	row, err := r.Q.GetMediaItemByTmdbID(ctx, &tmdbID)
	if err != nil {
		return model.MediaItem{}, apperrors.FromPg(err, "media item with tmdb id %d not found", tmdbID)
	}
	return toModelMediaItem(row), nil
}

func (r *Repository) GetMediaItemByTmdbIDAndType(ctx context.Context, tmdbID int64, typ string) (model.MediaItem, error) {
	row, err := r.Q.GetMediaItemByTmdbIDAndType(ctx, dbgen.GetMediaItemByTmdbIDAndTypeParams{
		TmdbID: &tmdbID,
		Type:   typ,
	})
	if err != nil {
		return model.MediaItem{}, apperrors.FromPg(err, "media item with tmdb id %d (type %s) not found", tmdbID, typ)
	}
	return toModelMediaItem(row), nil
}

func (r *Repository) CreateMediaItem(ctx context.Context, params CreateMediaItemParams) (model.MediaItem, error) {
	row, err := r.Q.CreateMediaItem(ctx, dbgen.CreateMediaItemParams{
		Type:   params.Type,
		Title:  params.Title,
		Year:   params.Year,
		TmdbID: params.TmdbID,
	})
	if err != nil {
		return model.MediaItem{}, apperrors.FromPg(err, "create media item %q", params.Title)
	}
	return toModelMediaItem(row), nil
}

func (r *Repository) UpsertMediaItem(ctx context.Context, params UpsertMediaItemParams) (model.MediaItem, error) {
	row, err := r.Q.UpsertMediaItem(ctx, dbgen.UpsertMediaItemParams{
		Type:   params.Type,
		Title:  params.Title,
		Year:   params.Year,
		TmdbID: params.TmdbID,
	})
	if err != nil {
		return model.MediaItem{}, apperrors.FromPg(err, "upsert media item %q", params.Title)
	}
	return toModelMediaItem(row), nil
}

func (r *Repository) UpdateMediaItem(ctx context.Context, params UpdateMediaItemParams) (model.MediaItem, error) {
	row, err := r.Q.UpdateMediaItem(ctx, dbgen.UpdateMediaItemParams{
		ID:     pgtypeFromUUID(params.ID),
		Title:  params.Title,
		Year:   params.Year,
		TmdbID: params.TmdbID,
	})
	if err != nil {
		return model.MediaItem{}, apperrors.FromPg(err, "update media item %s", params.ID)
	}
	return toModelMediaItem(row), nil
}

func (r *Repository) DeleteMediaItem(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteMediaItem(ctx, pgtypeFromUUID(id)), "delete media item %s", id)
}

func (r *Repository) UpdateMediaItemMetadata(ctx context.Context, params UpdateMediaItemMetadataParams) (model.MediaItem, error) {
	var genresJSON []byte
	if len(params.Genres) > 0 {
		var err error
		genresJSON, err = json.Marshal(params.Genres)
		if err != nil {
			return model.MediaItem{}, apperrors.Internalf("marshal genres: %v", err).Op("Repository.UpdateMediaItemMetadata")
		}
	}
	row, err := r.Q.UpdateMediaItemMetadata(ctx, dbgen.UpdateMediaItemMetadataParams{
		ID:            pgtypeFromUUID(params.ID),
		PosterPath:    params.PosterPath,
		BackdropPath:  params.BackdropPath,
		Overview:      params.Overview,
		VoteAverage:   params.VoteAverage,
		VoteCount:     params.VoteCount,
		Runtime:       params.Runtime,
		Status:        params.Status,
		Certification: params.Certification,
		Genres:        genresJSON,
		ReleaseDate:   pgDateFromTimePtr(params.ReleaseDate),
		LastAirDate:   pgDateFromTimePtr(params.LastAirDate),
		InProduction:  params.InProduction,
		NextRefreshAt: pgTimestamptzFromTimePtr(params.NextRefreshAt),
	})
	if err != nil {
		return model.MediaItem{}, apperrors.FromPg(err, "update metadata for media item %s", params.ID)
	}
	return toModelMediaItem(row), nil
}

// RecordMetadataFailure records a failed metadata sync: it advances the attempt
// counter, stores the error, and pushes next_refresh_at out to the caller's
// back-off time. It never touches metadata_updated_at — a failed sync must not
// read as fresh.
func (r *Repository) RecordMetadataFailure(ctx context.Context, id uuid.UUID, nextRefreshAt time.Time, errMsg string) error {
	return apperrors.FromPg(r.Q.RecordMetadataFailure(ctx, dbgen.RecordMetadataFailureParams{
		ID:            pgtypeFromUUID(id),
		LastError:     &errMsg,
		NextRefreshAt: pgtype.Timestamptz{Time: nextRefreshAt, Valid: true},
	}), "record metadata failure for media item %s", id)
}

// ListDueMediaItems returns items whose next_refresh_at has passed (NULL = due
// immediately), oldest-due first, capped at batchSize. It is the enrichment
// sweep's due queue.
func (r *Repository) ListDueMediaItems(ctx context.Context, now time.Time, batchSize int32) ([]model.MediaItem, error) {
	rows, err := r.Q.ListDueMediaItems(ctx, dbgen.ListDueMediaItemsParams{
		Now:       pgtype.Timestamptz{Time: now, Valid: true},
		BatchSize: batchSize,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "list due media items")
	}
	out := make([]model.MediaItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMediaItem(row))
	}
	return out, nil
}

func (r *Repository) UpsertMediaMetadataSource(ctx context.Context, params UpsertMediaMetadataSourceParams) error {
	return apperrors.FromPg(r.Q.UpsertMediaMetadataSource(ctx, dbgen.UpsertMediaMetadataSourceParams{
		MediaItemID: pgtypeFromUUID(params.MediaItemID),
		Source:      params.Source,
		Data:        []byte(params.Data),
	}), "upsert metadata source %q for media item %s", params.Source, params.MediaItemID)
}

// GetMediaItemExternalID reads a secondary-namespace cross-reference
// (imdb/tvdb/…) for a media item. A missing row is normal pre-enrichment, not
// an error: it returns (nil, nil) so callers can treat absence as "no id yet".
func (r *Repository) GetMediaItemExternalID(ctx context.Context, mediaItemID uuid.UUID, source string) (*string, error) {
	id, err := r.Q.GetMediaItemExternalID(ctx, dbgen.GetMediaItemExternalIDParams{
		MediaItemID: pgtypeFromUUID(mediaItemID),
		Source:      source,
	})
	if err != nil {
		err = apperrors.FromPg(err, "external id %q for media item %s", source, mediaItemID)
		if apperrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &id, nil
}

// UpsertMediaItemExternalID writes a secondary-namespace cross-reference
// (imdb/tvdb/…) for a media item. One row per (item, source); re-running
// refreshes the external_id in place.
func (r *Repository) UpsertMediaItemExternalID(ctx context.Context, mediaItemID uuid.UUID, source, externalID string) error {
	return apperrors.FromPg(r.Q.UpsertMediaItemExternalID(ctx, dbgen.UpsertMediaItemExternalIDParams{
		MediaItemID: pgtypeFromUUID(mediaItemID),
		Source:      source,
		ExternalID:  externalID,
	}), "upsert external id %q for media item %s", source, mediaItemID)
}

// toModelMediaSeason translates the persistence-shaped dbgen.MediaSeason into
// the domain-shaped model.MediaSeason. Lives next to the methods that use it.
func toModelMediaSeason(row dbgen.MediaSeason) model.MediaSeason {
	s := model.MediaSeason{
		ID:           uuidFromPgtype(row.ID),
		MediaItemID:  uuidFromPgtype(row.MediaItemID),
		SeasonNumber: row.SeasonNumber,
		Name:         row.Name,
		Overview:     row.Overview,
		PosterPath:   row.PosterPath,
		CreatedAt:    row.CreatedAt,
	}
	if row.AirDate.Valid {
		t := row.AirDate.Time
		s.AirDate = &t
	}
	return s
}

// toModelMediaEpisode translates the persistence-shaped dbgen.MediaEpisode
// into the domain-shaped model.MediaEpisode.
func toModelMediaEpisode(row dbgen.MediaEpisode) model.MediaEpisode {
	e := model.MediaEpisode{
		ID:             uuidFromPgtype(row.ID),
		SeasonID:       uuidFromPgtype(row.SeasonID),
		EpisodeNumber:  row.EpisodeNumber,
		Title:          row.Title,
		Overview:       row.Overview,
		StillPath:      row.StillPath,
		VoteAverage:    row.VoteAverage,
		Runtime:        row.Runtime,
		AbsoluteNumber: row.AbsoluteNumber,
		Deprecated:     row.Deprecated,
		TmdbID:         row.TmdbID,
		CreatedAt:      row.CreatedAt,
	}
	if row.AirDate.Valid {
		t := row.AirDate.Time
		e.AirDate = &t
	}
	return e
}

// UpsertSeasonParams is the domain-shaped input for UpsertSeason. Mirrors
// the writeable subset of model.MediaSeason (omits server-managed ID/CreatedAt).
type UpsertSeasonParams struct {
	MediaItemID  uuid.UUID
	SeasonNumber int32
	Name         *string
	Overview     *string
	PosterPath   *string
	AirDate      *time.Time
}

// UpsertEpisodeParams is the domain-shaped input for UpsertEpisode. Mirrors
// the writeable subset of model.MediaEpisode (omits server-managed ID/CreatedAt).
type UpsertEpisodeParams struct {
	SeasonID      uuid.UUID
	EpisodeNumber int32
	Title         *string
	AirDate       *time.Time
	Overview      *string
	StillPath     *string
	VoteAverage   *float64
	Runtime       *int32
	TmdbID        *int64
}

func (r *Repository) ListSeasonsForMedia(ctx context.Context, mediaID uuid.UUID) ([]model.MediaSeason, error) {
	rows, err := r.Q.ListSeasonsForMedia(ctx, pgtypeFromUUID(mediaID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list seasons for media %s", mediaID)
	}
	out := make([]model.MediaSeason, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMediaSeason(row))
	}
	return out, nil
}

func (r *Repository) GetSeason(ctx context.Context, id uuid.UUID) (model.MediaSeason, error) {
	row, err := r.Q.GetSeason(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.MediaSeason{}, apperrors.FromPg(err, "season %s not found", id)
	}
	return toModelMediaSeason(row), nil
}

func (r *Repository) GetSeasonByNumber(ctx context.Context, mediaItemID uuid.UUID, seasonNumber int32) (model.MediaSeason, error) {
	row, err := r.Q.GetSeasonByNumber(ctx, dbgen.GetSeasonByNumberParams{
		MediaItemID:  pgtypeFromUUID(mediaItemID),
		SeasonNumber: seasonNumber,
	})
	if err != nil {
		return model.MediaSeason{}, apperrors.FromPg(err, "season %d for media %s not found", seasonNumber, mediaItemID)
	}
	return toModelMediaSeason(row), nil
}

func (r *Repository) UpsertSeason(ctx context.Context, params UpsertSeasonParams) (model.MediaSeason, error) {
	airDate := pgtype.Date{Valid: false}
	if params.AirDate != nil {
		airDate = pgtype.Date{Time: *params.AirDate, Valid: true}
	}
	row, err := r.Q.UpsertSeason(ctx, dbgen.UpsertSeasonParams{
		MediaItemID:  pgtypeFromUUID(params.MediaItemID),
		SeasonNumber: params.SeasonNumber,
		Name:         params.Name,
		Overview:     params.Overview,
		PosterPath:   params.PosterPath,
		AirDate:      airDate,
	})
	if err != nil {
		return model.MediaSeason{}, apperrors.FromPg(err, "upsert season %d for media %s", params.SeasonNumber, params.MediaItemID)
	}
	return toModelMediaSeason(row), nil
}

func (r *Repository) ListEpisodesForSeason(ctx context.Context, seasonID uuid.UUID) ([]model.MediaEpisode, error) {
	rows, err := r.Q.ListEpisodesForSeason(ctx, pgtypeFromUUID(seasonID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list episodes for season %s", seasonID)
	}
	out := make([]model.MediaEpisode, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMediaEpisode(row))
	}
	return out, nil
}

func (r *Repository) GetEpisode(ctx context.Context, id uuid.UUID) (model.MediaEpisode, error) {
	row, err := r.Q.GetEpisode(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.MediaEpisode{}, apperrors.FromPg(err, "episode %s not found", id)
	}
	return toModelMediaEpisode(row), nil
}

func (r *Repository) GetEpisodeByNumber(ctx context.Context, seasonID uuid.UUID, episodeNumber int32) (model.MediaEpisode, error) {
	row, err := r.Q.GetEpisodeByNumber(ctx, dbgen.GetEpisodeByNumberParams{
		SeasonID:      pgtypeFromUUID(seasonID),
		EpisodeNumber: episodeNumber,
	})
	if err != nil {
		return model.MediaEpisode{}, apperrors.FromPg(err, "episode %d for season %s not found", episodeNumber, seasonID)
	}
	return toModelMediaEpisode(row), nil
}

func (r *Repository) UpsertEpisode(ctx context.Context, params UpsertEpisodeParams) (model.MediaEpisode, error) {
	airDate := pgtype.Date{Valid: false}
	if params.AirDate != nil {
		airDate = pgtype.Date{Time: *params.AirDate, Valid: true}
	}
	row, err := r.Q.UpsertEpisode(ctx, dbgen.UpsertEpisodeParams{
		SeasonID:      pgtypeFromUUID(params.SeasonID),
		EpisodeNumber: params.EpisodeNumber,
		Title:         params.Title,
		AirDate:       airDate,
		Overview:      params.Overview,
		StillPath:     params.StillPath,
		VoteAverage:   params.VoteAverage,
		Runtime:       params.Runtime,
		TmdbID:        params.TmdbID,
	})
	if err != nil {
		return model.MediaEpisode{}, apperrors.FromPg(err, "upsert episode %d for season %s", params.EpisodeNumber, params.SeasonID)
	}
	return toModelMediaEpisode(row), nil
}

// DeprecateRemovedEpisodes marks a series' episodes deprecated when TMDB no
// longer reports their id. Callers must skip this when syncedTmdbIDs is empty
// (a total sync failure) — an empty set would deprecate every episode.
func (r *Repository) DeprecateRemovedEpisodes(ctx context.Context, mediaItemID uuid.UUID, syncedTmdbIDs []int64) error {
	return apperrors.FromPg(r.Q.DeprecateRemovedEpisodes(ctx, dbgen.DeprecateRemovedEpisodesParams{
		MediaItemID:   pgtypeFromUUID(mediaItemID),
		SyncedTmdbIds: syncedTmdbIDs,
	}), "deprecate removed episodes for media %s", mediaItemID)
}

// CreateFileParams is the domain-shaped input for CreateFile. The id is
// supplied by the caller (the scan/match loop) so it stays stable as the
// match_decision.file_id join key. Identity fields are nullable.
type CreateFileParams struct {
	ID          uuid.UUID
	LibraryID   uuid.UUID
	Path        string
	MediaItemID *uuid.UUID
	EpisodeID   *uuid.UUID
	Edition     *string
}

// SetFileIdentityParams is the domain-shaped input for SetFileIdentity —
// points a file at a (media_item + optional episode + edition) in place.
type SetFileIdentityParams struct {
	ID          uuid.UUID
	MediaItemID uuid.UUID
	EpisodeID   *uuid.UUID
	Edition     *string
}

// GetFileByLibraryAndPathParams is the domain-shaped input for
// GetFileByLibraryAndPath.
type GetFileByLibraryAndPathParams struct {
	LibraryID uuid.UUID
	Path      string
}

// CreateFileStateParams is the domain-shaped input for CreateFileState.
// Omits server-managed LastVerifiedAt (set to now() in SQL).
type CreateFileStateParams struct {
	FileID    uuid.UUID
	Exists    bool
	SizeBytes *int64
	OsdbHash  *string
}

// UpsertFileStateParams is the domain-shaped input for UpsertFileState.
type UpsertFileStateParams struct {
	FileID    uuid.UUID
	Exists    bool
	SizeBytes *int64
	OsdbHash  *string
}

// UpdateFileStateParams is the domain-shaped input for UpdateFileState.
type UpdateFileStateParams struct {
	FileID    uuid.UUID
	Exists    bool
	SizeBytes *int64
	OsdbHash  *string
}

// toModelFile translates the persistence-shaped dbgen.File into the
// domain-shaped model.File.
func toModelFile(row dbgen.File) model.File {
	f := model.File{
		ID:        uuidFromPgtype(row.ID),
		LibraryID: uuidFromPgtype(row.LibraryID),
		Path:      row.Path,
		Edition:   row.Edition,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if row.MediaItemID.Valid {
		id := uuidFromPgtype(row.MediaItemID)
		f.MediaItemID = &id
	}
	if row.EpisodeID.Valid {
		id := uuidFromPgtype(row.EpisodeID)
		f.EpisodeID = &id
	}
	if row.DeletedAt.Valid {
		t := row.DeletedAt.Time
		f.DeletedAt = &t
	}
	return f
}

// toModelFileState translates the persistence-shaped dbgen.FileState into
// the domain-shaped model.FileState.
func toModelFileState(row dbgen.FileState) model.FileState {
	return model.FileState{
		FileID:         uuidFromPgtype(row.FileID),
		Exists:         row.Exists,
		SizeBytes:      row.SizeBytes,
		OsdbHash:       row.OsdbHash,
		LastVerifiedAt: row.LastVerifiedAt,
	}
}

// toModelFileWithState translates a ListFilesForItemRow (which LEFT-JOINs
// season/episode/state) into the domain composite.
func toModelFileWithState(row dbgen.ListFilesForItemRow) model.FileWithState {
	f := model.File{
		ID:        uuidFromPgtype(row.ID),
		LibraryID: uuidFromPgtype(row.LibraryID),
		Path:      row.Path,
		CreatedAt: row.CreatedAt,
	}
	if row.MediaItemID.Valid {
		id := uuidFromPgtype(row.MediaItemID)
		f.MediaItemID = &id
	}
	if row.EpisodeID.Valid {
		id := uuidFromPgtype(row.EpisodeID)
		f.EpisodeID = &id
	}
	out := model.FileWithState{
		File:          f,
		Exists:        row.Exists,
		SizeBytes:     row.SizeBytes,
		SeasonNumber:  row.SeasonNumber,
		EpisodeNumber: row.EpisodeNumber,
	}
	if row.SeasonID.Valid {
		id := uuidFromPgtype(row.SeasonID)
		out.SeasonID = &id
	}
	if row.LastVerifiedAt.Valid {
		t := row.LastVerifiedAt.Time
		out.LastVerifiedAt = &t
	}
	return out
}

// toModelFileWithStateFromMissing translates a ListMissingFilesRow into the
// domain composite. The query joins file_state but doesn't carry
// season/episode context.
func toModelFileWithStateFromMissing(row dbgen.ListMissingFilesRow) model.FileWithState {
	f := model.File{
		ID:        uuidFromPgtype(row.ID),
		LibraryID: uuidFromPgtype(row.LibraryID),
		Path:      row.Path,
		CreatedAt: row.CreatedAt,
	}
	if row.MediaItemID.Valid {
		id := uuidFromPgtype(row.MediaItemID)
		f.MediaItemID = &id
	}
	if row.EpisodeID.Valid {
		id := uuidFromPgtype(row.EpisodeID)
		f.EpisodeID = &id
	}
	// ListMissingFiles selects rows where exists = false; surface that
	// explicitly so callers don't have to assume.
	missing := false
	t := row.LastVerifiedAt
	return model.FileWithState{
		File:           f,
		Exists:         &missing,
		SizeBytes:      row.SizeBytes,
		LastVerifiedAt: &t,
	}
}

// toModelFileWithStateFromVerification translates a
// ListFilesNeedingVerificationRow into the domain composite.
func toModelFileWithStateFromVerification(row dbgen.ListFilesNeedingVerificationRow) model.FileWithState {
	f := model.File{
		ID:        uuidFromPgtype(row.ID),
		LibraryID: uuidFromPgtype(row.LibraryID),
		Path:      row.Path,
		CreatedAt: row.CreatedAt,
	}
	if row.MediaItemID.Valid {
		id := uuidFromPgtype(row.MediaItemID)
		f.MediaItemID = &id
	}
	if row.EpisodeID.Valid {
		id := uuidFromPgtype(row.EpisodeID)
		f.EpisodeID = &id
	}
	exists := row.Exists
	t := row.LastVerifiedAt
	return model.FileWithState{
		File:           f,
		Exists:         &exists,
		SizeBytes:      row.SizeBytes,
		LastVerifiedAt: &t,
	}
}

// toModelSeriesEpisodeAvailability translates a
// ListEpisodeAvailabilityForSeriesRow into the persistence-shape composite.
func toModelSeriesEpisodeAvailability(row dbgen.ListEpisodeAvailabilityForSeriesRow) model.SeriesEpisodeAvailability {
	out := model.SeriesEpisodeAvailability{
		SeasonNumber:  row.SeasonNumber,
		EpisodeNumber: row.EpisodeNumber,
		EpisodeID:     uuidFromPgtype(row.EpisodeID),
		Title:         row.Title,
		Deprecated:    row.Deprecated,
		FileExists:    row.Exists,
	}
	if row.AirDate.Valid {
		t := row.AirDate.Time
		out.AirDate = &t
	}
	if row.FileID.Valid {
		id := uuidFromPgtype(row.FileID)
		out.FileID = &id
	}
	if row.LibraryID.Valid {
		id := uuidFromPgtype(row.LibraryID)
		out.LibraryID = &id
	}
	return out
}

func (r *Repository) GetFile(ctx context.Context, id uuid.UUID) (model.File, error) {
	row, err := r.Q.GetFile(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.File{}, apperrors.FromPg(err, "file %s not found", id)
	}
	return toModelFile(row), nil
}

func (r *Repository) GetFileByLibraryAndPath(ctx context.Context, params GetFileByLibraryAndPathParams) (model.File, error) {
	row, err := r.Q.GetFileByLibraryAndPath(ctx, dbgen.GetFileByLibraryAndPathParams{
		LibraryID: pgtypeFromUUID(params.LibraryID),
		Path:      params.Path,
	})
	if err != nil {
		return model.File{}, apperrors.FromPg(err, "file at %q in library %s not found", params.Path, params.LibraryID)
	}
	return toModelFile(row), nil
}

func (r *Repository) CreateFile(ctx context.Context, params CreateFileParams) (model.File, error) {
	var mediaItem, episode pgtype.UUID
	if params.MediaItemID != nil {
		mediaItem = pgtypeFromUUID(*params.MediaItemID)
	}
	if params.EpisodeID != nil {
		episode = pgtypeFromUUID(*params.EpisodeID)
	}
	row, err := r.Q.CreateFile(ctx, dbgen.CreateFileParams{
		ID:          pgtypeFromUUID(params.ID),
		LibraryID:   pgtypeFromUUID(params.LibraryID),
		Path:        params.Path,
		MediaItemID: mediaItem,
		EpisodeID:   episode,
		Edition:     params.Edition,
	})
	if err != nil {
		return model.File{}, apperrors.FromPg(err, "create file %q in library %s", params.Path, params.LibraryID)
	}
	return toModelFile(row), nil
}

func (r *Repository) SetFileIdentity(ctx context.Context, params SetFileIdentityParams) (model.File, error) {
	var episode pgtype.UUID
	if params.EpisodeID != nil {
		episode = pgtypeFromUUID(*params.EpisodeID)
	}
	row, err := r.Q.SetFileIdentity(ctx, dbgen.SetFileIdentityParams{
		ID:          pgtypeFromUUID(params.ID),
		MediaItemID: pgtypeFromUUID(params.MediaItemID),
		EpisodeID:   episode,
		Edition:     params.Edition,
	})
	if err != nil {
		return model.File{}, apperrors.FromPg(err, "set identity for file %s", params.ID)
	}
	return toModelFile(row), nil
}

func (r *Repository) ClearFileIdentity(ctx context.Context, id uuid.UUID) (model.File, error) {
	row, err := r.Q.ClearFileIdentity(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.File{}, apperrors.FromPg(err, "clear identity for file %s", id)
	}
	return toModelFile(row), nil
}

func (r *Repository) SoftDeleteFile(ctx context.Context, id uuid.UUID) (model.File, error) {
	row, err := r.Q.SoftDeleteFile(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.File{}, apperrors.FromPg(err, "soft-delete file %s", id)
	}
	return toModelFile(row), nil
}

func (r *Repository) ListFilesForItem(ctx context.Context, mediaItemID uuid.UUID) ([]model.FileWithState, error) {
	rows, err := r.Q.ListFilesForItem(ctx, pgtypeFromUUID(mediaItemID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list files for item %s", mediaItemID)
	}
	out := make([]model.FileWithState, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelFileWithState(row))
	}
	return out, nil
}

// toModelInboxItem maps a ListInboxItemsRow / GetInboxItemRow (shared
// column shape) to model.InboxItem.
func toModelInboxItem(
	id, libraryID pgtype.UUID,
	path string,
	createdAt time.Time,
	sizeBytes *int64,
	outcome dbgen.MatchOutcome,
	confidence float64,
	displayTitle string,
	displayYear *int32,
	displayType string,
) model.InboxItem {
	item := model.InboxItem{
		ID:            uuidFromPgtype(id).String(),
		LibraryID:     uuidFromPgtype(libraryID).String(),
		Path:          path,
		FileSize:      sizeBytes,
		DiscoveredAt:  createdAt.Format(time.RFC3339),
		Outcome:       string(outcome),
		Confidence:    confidence,
		Title:         displayTitle,
		Type:          displayType,
		PartialSeries: outcome == dbgen.MatchOutcomePartialSeries,
	}
	// Year 0 means unknown — surface as nil, not a literal "0".
	if displayYear != nil && *displayYear != 0 {
		y := int(*displayYear)
		item.Year = &y
	}
	return item
}

func (r *Repository) ListInboxItems(ctx context.Context, params InboxQueryParams) ([]model.InboxItem, error) {
	var libID pgtype.UUID
	if params.LibraryID != nil {
		libID = pgtypeFromUUID(*params.LibraryID)
	}
	var outcome dbgen.NullMatchOutcome
	if params.Outcome != nil {
		outcome = dbgen.NullMatchOutcome{MatchOutcome: dbgen.MatchOutcome(*params.Outcome), Valid: true}
	}
	rows, err := r.Q.ListInboxItems(ctx, dbgen.ListInboxItemsParams{
		LibraryID: libID,
		Outcome:   outcome,
		PageSize:  params.PageSize,
		OffsetVal: params.Offset,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "list inbox items")
	}
	out := make([]model.InboxItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelInboxItem(
			row.ID, row.LibraryID, row.Path, row.CreatedAt, row.SizeBytes,
			row.Outcome, row.Confidence, row.DisplayTitle, row.DisplayYear, row.DisplayType,
		))
	}
	return out, nil
}

func (r *Repository) GetInboxItem(ctx context.Context, fileID uuid.UUID) (model.InboxItem, error) {
	row, err := r.Q.GetInboxItem(ctx, pgtypeFromUUID(fileID))
	if err != nil {
		return model.InboxItem{}, apperrors.FromPg(err, "inbox item %s not found", fileID)
	}
	return toModelInboxItem(
		row.ID, row.LibraryID, row.Path, row.CreatedAt, row.SizeBytes,
		row.Outcome, row.Confidence, row.DisplayTitle, row.DisplayYear, row.DisplayType,
	), nil
}

// CountInboxByOutcome returns per-band inbox totals for the optional
// library scope. Bands with zero files are absent from the map.
func (r *Repository) CountInboxByOutcome(ctx context.Context, libraryID *uuid.UUID) (map[string]int64, error) {
	var libID pgtype.UUID
	if libraryID != nil {
		libID = pgtypeFromUUID(*libraryID)
	}
	rows, err := r.Q.CountInboxByOutcome(ctx, libID)
	if err != nil {
		return nil, apperrors.FromPg(err, "count inbox by outcome")
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[string(row.Outcome)] = row.Count
	}
	return out, nil
}

func (r *Repository) ListEpisodeAvailabilityForSeries(ctx context.Context, mediaItemID uuid.UUID) ([]model.SeriesEpisodeAvailability, error) {
	rows, err := r.Q.ListEpisodeAvailabilityForSeries(ctx, pgtypeFromUUID(mediaItemID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list episode availability for series %s", mediaItemID)
	}
	out := make([]model.SeriesEpisodeAvailability, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelSeriesEpisodeAvailability(row))
	}
	return out, nil
}

func (r *Repository) ListFilePathsForLibrary(ctx context.Context, libraryID uuid.UUID) ([]string, error) {
	paths, err := r.Q.ListFilePathsForLibrary(ctx, pgtypeFromUUID(libraryID))
	return paths, apperrors.FromPg(err, "list file paths for library %s", libraryID)
}

// CheckMediaItemsInLibrary returns a map of tmdbID -> true for items that exist in library
func (r *Repository) CheckMediaItemsInLibrary(ctx context.Context, tmdbIDs []int64, typ string) (map[int64]bool, error) {
	result := make(map[int64]bool)
	if len(tmdbIDs) == 0 {
		return result, nil
	}

	rows, err := r.Q.GetMediaItemsByTmdbIDs(ctx, dbgen.GetMediaItemsByTmdbIDsParams{
		TmdbIds: tmdbIDs,
		Type:    typ,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "check media items in library (type %s)", typ)
	}

	for _, tmdbID := range rows {
		if tmdbID != nil {
			result[*tmdbID] = true
		}
	}
	return result, nil
}

// File State methods

func (r *Repository) CreateFileState(ctx context.Context, params CreateFileStateParams) (model.FileState, error) {
	row, err := r.Q.CreateFileState(ctx, dbgen.CreateFileStateParams{
		FileID:    pgtypeFromUUID(params.FileID),
		Exists:    params.Exists,
		SizeBytes: params.SizeBytes,
		OsdbHash:  params.OsdbHash,
	})
	if err != nil {
		return model.FileState{}, apperrors.FromPg(err, "create file state for %s", params.FileID)
	}
	return toModelFileState(row), nil
}

func (r *Repository) UpsertFileState(ctx context.Context, params UpsertFileStateParams) (model.FileState, error) {
	row, err := r.Q.UpsertFileState(ctx, dbgen.UpsertFileStateParams{
		FileID:    pgtypeFromUUID(params.FileID),
		Exists:    params.Exists,
		SizeBytes: params.SizeBytes,
		OsdbHash:  params.OsdbHash,
	})
	if err != nil {
		return model.FileState{}, apperrors.FromPg(err, "upsert file state for %s", params.FileID)
	}
	return toModelFileState(row), nil
}

func (r *Repository) GetFileState(ctx context.Context, fileID uuid.UUID) (model.FileState, error) {
	row, err := r.Q.GetFileState(ctx, pgtypeFromUUID(fileID))
	if err != nil {
		return model.FileState{}, apperrors.FromPg(err, "file state for %s not found", fileID)
	}
	return toModelFileState(row), nil
}

func (r *Repository) UpdateFileState(ctx context.Context, params UpdateFileStateParams) (model.FileState, error) {
	row, err := r.Q.UpdateFileState(ctx, dbgen.UpdateFileStateParams{
		FileID:    pgtypeFromUUID(params.FileID),
		Exists:    params.Exists,
		SizeBytes: params.SizeBytes,
		OsdbHash:  params.OsdbHash,
	})
	if err != nil {
		return model.FileState{}, apperrors.FromPg(err, "update file state for %s", params.FileID)
	}
	return toModelFileState(row), nil
}

func (r *Repository) ListMissingFiles(ctx context.Context) ([]model.FileWithState, error) {
	rows, err := r.Q.ListMissingFiles(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list missing files")
	}
	out := make([]model.FileWithState, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelFileWithStateFromMissing(row))
	}
	return out, nil
}

func (r *Repository) ListFilesNeedingVerification(ctx context.Context, beforeTime time.Time, limit int32) ([]model.FileWithState, error) {
	rows, err := r.Q.ListFilesNeedingVerification(ctx, dbgen.ListFilesNeedingVerificationParams{
		BeforeTime: beforeTime,
		LimitVal:   limit,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "list files needing verification")
	}
	out := make([]model.FileWithState, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelFileWithStateFromVerification(row))
	}
	return out, nil
}

// File Import methods

// CreateFileImportParams is the domain-shaped input for CreateFileImport.
// Mirrors the writeable subset of model.FileImport (omits server-managed
// ID and AttemptedAt).
type CreateFileImportParams struct {
	FileID       uuid.UUID
	ImportTaskID uuid.UUID
	Method       string
	SourcePath   *string
	DestPath     string
	Success      bool
	ErrorMessage *string
}

// toModelFileImport translates the persistence-shaped dbgen.FileImport into
// the domain-shaped model.FileImport.
func toModelFileImport(row dbgen.FileImport) model.FileImport {
	return model.FileImport{
		ID:           uuidFromPgtype(row.ID),
		FileID:       uuidFromPgtype(row.FileID),
		ImportTaskID: uuidFromPgtype(row.ImportTaskID),
		Method:       row.Method,
		SourcePath:   row.SourcePath,
		DestPath:     row.DestPath,
		AttemptedAt:  row.AttemptedAt,
		Success:      row.Success,
		ErrorMessage: row.ErrorMessage,
	}
}

func (r *Repository) CreateFileImport(ctx context.Context, params CreateFileImportParams) (model.FileImport, error) {
	row, err := r.Q.CreateFileImport(ctx, dbgen.CreateFileImportParams{
		FileID: pgtypeFromUUID(params.FileID),
		// scan and manual-match imports don't have an import_task — the
		// column is nullable in the schema; map uuid.Nil → NULL so the
		// FK to import_task is satisfied for scan-discovered files.
		ImportTaskID: pgtypeFromUUIDOrNull(params.ImportTaskID),
		Method:       params.Method,
		SourcePath:   params.SourcePath,
		DestPath:     params.DestPath,
		Success:      params.Success,
		ErrorMessage: params.ErrorMessage,
	})
	if err != nil {
		return model.FileImport{}, apperrors.FromPg(err, "create file import for file %s", params.FileID)
	}
	return toModelFileImport(row), nil
}

func (r *Repository) GetFileImport(ctx context.Context, id uuid.UUID) (model.FileImport, error) {
	row, err := r.Q.GetFileImport(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.FileImport{}, apperrors.FromPg(err, "file import %s not found", id)
	}
	return toModelFileImport(row), nil
}

func (r *Repository) ListImportsForFile(ctx context.Context, fileID uuid.UUID) ([]model.FileImport, error) {
	rows, err := r.Q.ListImportsForFile(ctx, pgtypeFromUUID(fileID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list imports for file %s", fileID)
	}
	out := make([]model.FileImport, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelFileImport(row))
	}
	return out, nil
}

func (r *Repository) ListImportsForImportTask(ctx context.Context, importTaskID uuid.UUID) ([]model.FileImport, error) {
	rows, err := r.Q.ListImportsForImportTask(ctx, pgtypeFromUUID(importTaskID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list imports for import task %s", importTaskID)
	}
	out := make([]model.FileImport, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelFileImport(row))
	}
	return out, nil
}

func (r *Repository) ListRecentImports(ctx context.Context, limit int32) ([]model.FileImport, error) {
	rows, err := r.Q.ListRecentImports(ctx, limit)
	if err != nil {
		return nil, apperrors.FromPg(err, "list recent imports")
	}
	out := make([]model.FileImport, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelFileImport(row))
	}
	return out, nil
}

func (r *Repository) ListFailedImports(ctx context.Context, limit int32) ([]model.FileImport, error) {
	rows, err := r.Q.ListFailedImports(ctx, limit)
	if err != nil {
		return nil, apperrors.FromPg(err, "list failed imports")
	}
	out := make([]model.FileImport, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelFileImport(row))
	}
	return out, nil
}
