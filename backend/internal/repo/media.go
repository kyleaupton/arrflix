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

// UnmatchedFilesQueryParams contains parameters for paginated unmatched files queries
type UnmatchedFilesQueryParams struct {
	LibraryID *uuid.UUID
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
	ListStaleMediaItems(ctx context.Context, staleBefore time.Time, batchSize int32) ([]model.MediaItem, error)
	UpsertMediaMetadataSource(ctx context.Context, params UpsertMediaMetadataSourceParams) error

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

	// File path loading for scanner
	ListMediaFilePathsForLibrary(ctx context.Context, libraryID uuid.UUID) ([]string, error)
	ListUnmatchedFilePathsForLibrary(ctx context.Context, libraryID uuid.UUID) ([]string, error)

	// Files (removed season_id and status)
	GetMediaFile(ctx context.Context, id uuid.UUID) (model.MediaFile, error)
	GetMediaFileByLibraryAndPath(ctx context.Context, params GetMediaFileByLibraryAndPathParams) (model.MediaFile, error)
	CreateMediaFile(ctx context.Context, params CreateMediaFileParams) (model.MediaFile, error)
	ListMediaFilesForItem(ctx context.Context, mediaItemID uuid.UUID) ([]model.MediaFileWithState, error)
	ListEpisodeAvailabilityForSeries(ctx context.Context, mediaItemID uuid.UUID) ([]model.SeriesEpisodeAvailability, error)
	DeleteMediaFile(ctx context.Context, id uuid.UUID) error

	// File state
	CreateMediaFileState(ctx context.Context, mediaFileID uuid.UUID, fileExists bool, fileSize *int64) (model.MediaFileState, error)
	UpsertMediaFileState(ctx context.Context, params UpsertMediaFileStateParams) (model.MediaFileState, error)
	GetMediaFileState(ctx context.Context, mediaFileID uuid.UUID) (model.MediaFileState, error)
	UpdateMediaFileState(ctx context.Context, mediaFileID uuid.UUID, fileExists bool, fileSize *int64) (model.MediaFileState, error)
	ListMissingFiles(ctx context.Context) ([]model.MediaFileWithState, error)
	ListFilesNeedingVerification(ctx context.Context, beforeTime time.Time, limit int32) ([]model.MediaFileWithState, error)

	// File imports
	CreateMediaFileImport(ctx context.Context, params CreateMediaFileImportParams) (model.MediaFileImport, error)
	GetMediaFileImport(ctx context.Context, id uuid.UUID) (model.MediaFileImport, error)
	ListImportsForMediaFile(ctx context.Context, mediaFileID uuid.UUID) ([]model.MediaFileImport, error)
	ListImportsForImportTask(ctx context.Context, importTaskID uuid.UUID) ([]model.MediaFileImport, error)
	ListRecentImports(ctx context.Context, limit int32) ([]model.MediaFileImport, error)
	ListFailedImports(ctx context.Context, limit int32) ([]model.MediaFileImport, error)

	// Unmatched files
	CreateUnmatchedFile(ctx context.Context, params CreateUnmatchedFileParams) (model.UnmatchedFile, error)
	UpsertUnmatchedFile(ctx context.Context, params UpsertUnmatchedFileParams) (model.UnmatchedFile, error)
	GetUnmatchedFile(ctx context.Context, id uuid.UUID) (model.UnmatchedFile, error)
	GetUnmatchedFileByPath(ctx context.Context, params GetUnmatchedFileByPathParams) (model.UnmatchedFile, error)
	ListUnmatchedFiles(ctx context.Context) ([]model.UnmatchedFile, error)
	ListUnmatchedFilesForLibrary(ctx context.Context, libraryID uuid.UUID) ([]model.UnmatchedFile, error)
	ListUnmatchedFilesPaginated(ctx context.Context, params UnmatchedFilesQueryParams) ([]model.UnmatchedFile, error)
	CountUnmatchedFiles(ctx context.Context, libraryID *uuid.UUID) (int64, error)
	ResolveUnmatchedFile(ctx context.Context, params ResolveUnmatchedFileParams) (model.UnmatchedFile, error)
	DismissUnmatchedFile(ctx context.Context, id uuid.UUID) (model.UnmatchedFile, error)
	UpdateUnmatchedFileSuggestions(ctx context.Context, params UpdateUnmatchedFileSuggestionsParams) (model.UnmatchedFile, error)
	DeleteUnmatchedFile(ctx context.Context, id uuid.UUID) error
	DeleteResolvedUnmatchedFilesOlderThan(ctx context.Context, beforeTime time.Time) error
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
	ImdbID        *string
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
		ID:            uuidFromPgtype(row.ID),
		Type:          row.Type,
		Title:         row.Title,
		Year:          row.Year,
		TmdbID:        row.TmdbID,
		PosterPath:    row.PosterPath,
		BackdropPath:  row.BackdropPath,
		Overview:      row.Overview,
		VoteAverage:   row.VoteAverage,
		VoteCount:     row.VoteCount,
		Runtime:       row.Runtime,
		Status:        row.Status,
		Certification: row.Certification,
		InProduction:  row.InProduction,
		ImdbID:        row.ImdbID,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
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
		ImdbID:        params.ImdbID,
	})
	if err != nil {
		return model.MediaItem{}, apperrors.FromPg(err, "update metadata for media item %s", params.ID)
	}
	return toModelMediaItem(row), nil
}

func (r *Repository) ListStaleMediaItems(ctx context.Context, staleBefore time.Time, batchSize int32) ([]model.MediaItem, error) {
	rows, err := r.Q.ListStaleMediaItems(ctx, dbgen.ListStaleMediaItemsParams{
		StaleBefore: pgtype.Timestamptz{Time: staleBefore, Valid: true},
		BatchSize:   batchSize,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "list stale media items")
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

// toModelMediaSeason translates the persistence-shaped dbgen.MediaSeason into
// the domain-shaped model.MediaSeason. Lives next to the methods that use it.
func toModelMediaSeason(row dbgen.MediaSeason) model.MediaSeason {
	s := model.MediaSeason{
		ID:           uuidFromPgtype(row.ID),
		MediaItemID:  uuidFromPgtype(row.MediaItemID),
		SeasonNumber: row.SeasonNumber,
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
		ID:            uuidFromPgtype(row.ID),
		SeasonID:      uuidFromPgtype(row.SeasonID),
		EpisodeNumber: row.EpisodeNumber,
		Title:         row.Title,
		TmdbID:        row.TmdbID,
		TvdbID:        row.TvdbID,
		CreatedAt:     row.CreatedAt,
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
	AirDate      *time.Time
}

// UpsertEpisodeParams is the domain-shaped input for UpsertEpisode. Mirrors
// the writeable subset of model.MediaEpisode (omits server-managed ID/CreatedAt).
type UpsertEpisodeParams struct {
	SeasonID      uuid.UUID
	EpisodeNumber int32
	Title         *string
	AirDate       *time.Time
	TmdbID        *int64
	TvdbID        *int64
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
		TmdbID:        params.TmdbID,
		TvdbID:        params.TvdbID,
	})
	if err != nil {
		return model.MediaEpisode{}, apperrors.FromPg(err, "upsert episode %d for season %s", params.EpisodeNumber, params.SeasonID)
	}
	return toModelMediaEpisode(row), nil
}

// CreateMediaFileParams is the domain-shaped input for CreateMediaFile. Mirrors
// the writeable subset of model.MediaFile (omits server-managed ID/CreatedAt).
type CreateMediaFileParams struct {
	LibraryID   uuid.UUID
	MediaItemID uuid.UUID
	EpisodeID   *uuid.UUID
	Path        string
}

// GetMediaFileByLibraryAndPathParams is the domain-shaped input for
// GetMediaFileByLibraryAndPath.
type GetMediaFileByLibraryAndPathParams struct {
	LibraryID uuid.UUID
	Path      string
}

// UpsertMediaFileStateParams is the domain-shaped input for UpsertMediaFileState.
// Mirrors the writeable subset of model.MediaFileState (omits server-managed
// LastVerifiedAt, set to now() in SQL).
type UpsertMediaFileStateParams struct {
	MediaFileID uuid.UUID
	FileExists  bool
	FileSize    *int64
}

// toModelMediaFile translates the persistence-shaped dbgen.MediaFile into
// the domain-shaped model.MediaFile.
func toModelMediaFile(row dbgen.MediaFile) model.MediaFile {
	mf := model.MediaFile{
		ID:          uuidFromPgtype(row.ID),
		LibraryID:   uuidFromPgtype(row.LibraryID),
		MediaItemID: uuidFromPgtype(row.MediaItemID),
		Path:        row.Path,
		CreatedAt:   row.CreatedAt,
	}
	if row.EpisodeID.Valid {
		id := uuidFromPgtype(row.EpisodeID)
		mf.EpisodeID = &id
	}
	return mf
}

// toModelMediaFileState translates the persistence-shaped dbgen.MediaFileState
// into the domain-shaped model.MediaFileState.
func toModelMediaFileState(row dbgen.MediaFileState) model.MediaFileState {
	return model.MediaFileState{
		MediaFileID:    uuidFromPgtype(row.MediaFileID),
		FileExists:     row.FileExists,
		FileSize:       row.FileSize,
		LastVerifiedAt: row.LastVerifiedAt,
	}
}

// toModelMediaFileWithState translates a ListMediaFilesForItemRow (which
// LEFT-JOINs season/episode/state) into the domain composite.
func toModelMediaFileWithState(row dbgen.ListMediaFilesForItemRow) model.MediaFileWithState {
	mf := model.MediaFile{
		ID:          uuidFromPgtype(row.ID),
		LibraryID:   uuidFromPgtype(row.LibraryID),
		MediaItemID: uuidFromPgtype(row.MediaItemID),
		Path:        row.Path,
		CreatedAt:   row.CreatedAt,
	}
	if row.EpisodeID.Valid {
		id := uuidFromPgtype(row.EpisodeID)
		mf.EpisodeID = &id
	}
	out := model.MediaFileWithState{
		MediaFile:     mf,
		FileExists:    row.FileExists,
		FileSize:      row.FileSize,
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

// toModelMediaFileWithStateFromMissing translates a ListMissingFilesRow into
// the domain composite. The query joins media_file_state but doesn't carry
// season/episode context.
func toModelMediaFileWithStateFromMissing(row dbgen.ListMissingFilesRow) model.MediaFileWithState {
	mf := model.MediaFile{
		ID:          uuidFromPgtype(row.ID),
		LibraryID:   uuidFromPgtype(row.LibraryID),
		MediaItemID: uuidFromPgtype(row.MediaItemID),
		Path:        row.Path,
		CreatedAt:   row.CreatedAt,
	}
	if row.EpisodeID.Valid {
		id := uuidFromPgtype(row.EpisodeID)
		mf.EpisodeID = &id
	}
	// ListMissingFiles selects rows where file_exists = false; surface that
	// explicitly so callers don't have to assume.
	missing := false
	t := row.LastVerifiedAt
	return model.MediaFileWithState{
		MediaFile:      mf,
		FileExists:     &missing,
		FileSize:       row.FileSize,
		LastVerifiedAt: &t,
	}
}

// toModelMediaFileWithStateFromVerification translates a
// ListFilesNeedingVerificationRow into the domain composite.
func toModelMediaFileWithStateFromVerification(row dbgen.ListFilesNeedingVerificationRow) model.MediaFileWithState {
	mf := model.MediaFile{
		ID:          uuidFromPgtype(row.ID),
		LibraryID:   uuidFromPgtype(row.LibraryID),
		MediaItemID: uuidFromPgtype(row.MediaItemID),
		Path:        row.Path,
		CreatedAt:   row.CreatedAt,
	}
	if row.EpisodeID.Valid {
		id := uuidFromPgtype(row.EpisodeID)
		mf.EpisodeID = &id
	}
	exists := row.FileExists
	t := row.LastVerifiedAt
	return model.MediaFileWithState{
		MediaFile:      mf,
		FileExists:     &exists,
		FileSize:       row.FileSize,
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
		FileExists:    row.FileExists,
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

func (r *Repository) GetMediaFile(ctx context.Context, id uuid.UUID) (model.MediaFile, error) {
	row, err := r.Q.GetMediaFile(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.MediaFile{}, apperrors.FromPg(err, "media file %s not found", id)
	}
	return toModelMediaFile(row), nil
}

func (r *Repository) GetMediaFileByLibraryAndPath(ctx context.Context, params GetMediaFileByLibraryAndPathParams) (model.MediaFile, error) {
	row, err := r.Q.GetMediaFileByLibraryAndPath(ctx, dbgen.GetMediaFileByLibraryAndPathParams{
		LibraryID: pgtypeFromUUID(params.LibraryID),
		Path:      params.Path,
	})
	if err != nil {
		return model.MediaFile{}, apperrors.FromPg(err, "media file at %q in library %s not found", params.Path, params.LibraryID)
	}
	return toModelMediaFile(row), nil
}

func (r *Repository) CreateMediaFile(ctx context.Context, params CreateMediaFileParams) (model.MediaFile, error) {
	var episode pgtype.UUID
	if params.EpisodeID != nil {
		episode = pgtypeFromUUID(*params.EpisodeID)
	}
	row, err := r.Q.CreateMediaFile(ctx, dbgen.CreateMediaFileParams{
		LibraryID:   pgtypeFromUUID(params.LibraryID),
		MediaItemID: pgtypeFromUUID(params.MediaItemID),
		EpisodeID:   episode,
		Path:        params.Path,
	})
	if err != nil {
		return model.MediaFile{}, apperrors.FromPg(err, "create media file %q in library %s", params.Path, params.LibraryID)
	}
	return toModelMediaFile(row), nil
}

func (r *Repository) ListMediaFilesForItem(ctx context.Context, mediaItemID uuid.UUID) ([]model.MediaFileWithState, error) {
	rows, err := r.Q.ListMediaFilesForItem(ctx, pgtypeFromUUID(mediaItemID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list media files for item %s", mediaItemID)
	}
	out := make([]model.MediaFileWithState, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMediaFileWithState(row))
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

func (r *Repository) DeleteMediaFile(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteMediaFile(ctx, pgtypeFromUUID(id)), "delete media file %s", id)
}

func (r *Repository) ListMediaFilePathsForLibrary(ctx context.Context, libraryID uuid.UUID) ([]string, error) {
	paths, err := r.Q.ListMediaFilePathsForLibrary(ctx, pgtypeFromUUID(libraryID))
	return paths, apperrors.FromPg(err, "list media file paths for library %s", libraryID)
}

func (r *Repository) ListUnmatchedFilePathsForLibrary(ctx context.Context, libraryID uuid.UUID) ([]string, error) {
	paths, err := r.Q.ListUnmatchedFilePathsForLibrary(ctx, pgtypeFromUUID(libraryID))
	return paths, apperrors.FromPg(err, "list unmatched file paths for library %s", libraryID)
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

// Media File State methods

func (r *Repository) CreateMediaFileState(ctx context.Context, mediaFileID uuid.UUID, fileExists bool, fileSize *int64) (model.MediaFileState, error) {
	row, err := r.Q.CreateMediaFileState(ctx, dbgen.CreateMediaFileStateParams{
		MediaFileID: pgtypeFromUUID(mediaFileID),
		FileExists:  fileExists,
		FileSize:    fileSize,
	})
	if err != nil {
		return model.MediaFileState{}, apperrors.FromPg(err, "create media file state for %s", mediaFileID)
	}
	return toModelMediaFileState(row), nil
}

func (r *Repository) UpsertMediaFileState(ctx context.Context, params UpsertMediaFileStateParams) (model.MediaFileState, error) {
	row, err := r.Q.UpsertMediaFileState(ctx, dbgen.UpsertMediaFileStateParams{
		MediaFileID: pgtypeFromUUID(params.MediaFileID),
		FileExists:  params.FileExists,
		FileSize:    params.FileSize,
	})
	if err != nil {
		return model.MediaFileState{}, apperrors.FromPg(err, "upsert media file state for %s", params.MediaFileID)
	}
	return toModelMediaFileState(row), nil
}

func (r *Repository) GetMediaFileState(ctx context.Context, mediaFileID uuid.UUID) (model.MediaFileState, error) {
	row, err := r.Q.GetMediaFileState(ctx, pgtypeFromUUID(mediaFileID))
	if err != nil {
		return model.MediaFileState{}, apperrors.FromPg(err, "media file state for %s not found", mediaFileID)
	}
	return toModelMediaFileState(row), nil
}

func (r *Repository) UpdateMediaFileState(ctx context.Context, mediaFileID uuid.UUID, fileExists bool, fileSize *int64) (model.MediaFileState, error) {
	row, err := r.Q.UpdateMediaFileState(ctx, dbgen.UpdateMediaFileStateParams{
		MediaFileID: pgtypeFromUUID(mediaFileID),
		FileExists:  fileExists,
		FileSize:    fileSize,
	})
	if err != nil {
		return model.MediaFileState{}, apperrors.FromPg(err, "update media file state for %s", mediaFileID)
	}
	return toModelMediaFileState(row), nil
}

func (r *Repository) ListMissingFiles(ctx context.Context) ([]model.MediaFileWithState, error) {
	rows, err := r.Q.ListMissingFiles(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list missing files")
	}
	out := make([]model.MediaFileWithState, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMediaFileWithStateFromMissing(row))
	}
	return out, nil
}

func (r *Repository) ListFilesNeedingVerification(ctx context.Context, beforeTime time.Time, limit int32) ([]model.MediaFileWithState, error) {
	rows, err := r.Q.ListFilesNeedingVerification(ctx, dbgen.ListFilesNeedingVerificationParams{
		BeforeTime: beforeTime,
		LimitVal:   limit,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "list files needing verification")
	}
	out := make([]model.MediaFileWithState, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMediaFileWithStateFromVerification(row))
	}
	return out, nil
}

// Media File Import methods

// CreateMediaFileImportParams is the domain-shaped input for CreateMediaFileImport.
// Mirrors the writeable subset of model.MediaFileImport (omits server-managed
// ID and AttemptedAt).
type CreateMediaFileImportParams struct {
	MediaFileID  uuid.UUID
	ImportTaskID uuid.UUID
	Method       string
	SourcePath   *string
	DestPath     string
	Success      bool
	ErrorMessage *string
}

// toModelMediaFileImport translates the persistence-shaped dbgen.MediaFileImport
// into the domain-shaped model.MediaFileImport.
func toModelMediaFileImport(row dbgen.MediaFileImport) model.MediaFileImport {
	return model.MediaFileImport{
		ID:           uuidFromPgtype(row.ID),
		MediaFileID:  uuidFromPgtype(row.MediaFileID),
		ImportTaskID: uuidFromPgtype(row.ImportTaskID),
		Method:       row.Method,
		SourcePath:   row.SourcePath,
		DestPath:     row.DestPath,
		AttemptedAt:  row.AttemptedAt,
		Success:      row.Success,
		ErrorMessage: row.ErrorMessage,
	}
}

func (r *Repository) CreateMediaFileImport(ctx context.Context, params CreateMediaFileImportParams) (model.MediaFileImport, error) {
	row, err := r.Q.CreateMediaFileImport(ctx, dbgen.CreateMediaFileImportParams{
		MediaFileID:  pgtypeFromUUID(params.MediaFileID),
		ImportTaskID: pgtypeFromUUID(params.ImportTaskID),
		Method:       params.Method,
		SourcePath:   params.SourcePath,
		DestPath:     params.DestPath,
		Success:      params.Success,
		ErrorMessage: params.ErrorMessage,
	})
	if err != nil {
		return model.MediaFileImport{}, apperrors.FromPg(err, "create media file import for task %s", params.ImportTaskID)
	}
	return toModelMediaFileImport(row), nil
}

func (r *Repository) GetMediaFileImport(ctx context.Context, id uuid.UUID) (model.MediaFileImport, error) {
	row, err := r.Q.GetMediaFileImport(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.MediaFileImport{}, apperrors.FromPg(err, "media file import %s not found", id)
	}
	return toModelMediaFileImport(row), nil
}

func (r *Repository) ListImportsForMediaFile(ctx context.Context, mediaFileID uuid.UUID) ([]model.MediaFileImport, error) {
	rows, err := r.Q.ListImportsForMediaFile(ctx, pgtypeFromUUID(mediaFileID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list imports for media file %s", mediaFileID)
	}
	out := make([]model.MediaFileImport, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMediaFileImport(row))
	}
	return out, nil
}

func (r *Repository) ListImportsForImportTask(ctx context.Context, importTaskID uuid.UUID) ([]model.MediaFileImport, error) {
	rows, err := r.Q.ListImportsForImportTask(ctx, pgtypeFromUUID(importTaskID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list imports for import task %s", importTaskID)
	}
	out := make([]model.MediaFileImport, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMediaFileImport(row))
	}
	return out, nil
}

func (r *Repository) ListRecentImports(ctx context.Context, limit int32) ([]model.MediaFileImport, error) {
	rows, err := r.Q.ListRecentImports(ctx, limit)
	if err != nil {
		return nil, apperrors.FromPg(err, "list recent imports")
	}
	out := make([]model.MediaFileImport, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMediaFileImport(row))
	}
	return out, nil
}

func (r *Repository) ListFailedImports(ctx context.Context, limit int32) ([]model.MediaFileImport, error) {
	rows, err := r.Q.ListFailedImports(ctx, limit)
	if err != nil {
		return nil, apperrors.FromPg(err, "list failed imports")
	}
	out := make([]model.MediaFileImport, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelMediaFileImport(row))
	}
	return out, nil
}

// Unmatched File methods

// CreateUnmatchedFileParams is the domain-shaped input for CreateUnmatchedFile.
// Mirrors the writeable subset of model.UnmatchedFile (omits server-managed
// ID/DiscoveredAt and resolution fields).
type CreateUnmatchedFileParams struct {
	LibraryID        uuid.UUID
	Path             string
	FileSize         *int64
	SuggestedMatches []model.SuggestedMatch
}

// UpsertUnmatchedFileParams is the domain-shaped input for UpsertUnmatchedFile.
// Same shape as CreateUnmatchedFileParams; conflict on (library_id, path) updates
// the suggestions and file size.
type UpsertUnmatchedFileParams struct {
	LibraryID        uuid.UUID
	Path             string
	FileSize         *int64
	SuggestedMatches []model.SuggestedMatch
}

// GetUnmatchedFileByPathParams is the domain-shaped input for
// GetUnmatchedFileByPath.
type GetUnmatchedFileByPathParams struct {
	LibraryID uuid.UUID
	Path      string
}

// ResolveUnmatchedFileParams is the domain-shaped input for ResolveUnmatchedFile.
type ResolveUnmatchedFileParams struct {
	ID                  uuid.UUID
	ResolvedMediaFileID uuid.UUID
}

// UpdateUnmatchedFileSuggestionsParams is the domain-shaped input for
// UpdateUnmatchedFileSuggestions.
type UpdateUnmatchedFileSuggestionsParams struct {
	ID               uuid.UUID
	SuggestedMatches []model.SuggestedMatch
}

// toModelUnmatchedFile translates the persistence-shaped dbgen.UnmatchedFile
// into the domain-shaped model.UnmatchedFile.
func toModelUnmatchedFile(row dbgen.UnmatchedFile) model.UnmatchedFile {
	uf := model.UnmatchedFile{
		ID:           uuidFromPgtype(row.ID),
		LibraryID:    uuidFromPgtype(row.LibraryID),
		Path:         row.Path,
		FileSize:     row.FileSize,
		DiscoveredAt: row.DiscoveredAt,
	}
	if len(row.SuggestedMatches) > 0 {
		var matches []model.SuggestedMatch
		if err := json.Unmarshal(row.SuggestedMatches, &matches); err == nil {
			uf.SuggestedMatches = matches
		}
		// Silent-empty on parse failure: bad rows shouldn't fail the whole query.
	}
	if row.ResolvedAt.Valid {
		t := row.ResolvedAt.Time
		uf.ResolvedAt = &t
	}
	if row.ResolvedMediaFileID.Valid {
		id := uuidFromPgtype(row.ResolvedMediaFileID)
		uf.ResolvedMediaFileID = &id
	}
	return uf
}

// marshalSuggestions serializes a SuggestedMatch slice to JSON bytes for
// persistence. Returns nil bytes for an empty slice (the column is nullable).
func marshalSuggestions(matches []model.SuggestedMatch, op string) ([]byte, error) {
	if len(matches) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(matches)
	if err != nil {
		return nil, apperrors.Internalf("marshal suggestions: %v", err).Op(op)
	}
	return b, nil
}

func (r *Repository) CreateUnmatchedFile(ctx context.Context, params CreateUnmatchedFileParams) (model.UnmatchedFile, error) {
	suggestionsJSON, err := marshalSuggestions(params.SuggestedMatches, "Repository.CreateUnmatchedFile")
	if err != nil {
		return model.UnmatchedFile{}, err
	}
	row, err := r.Q.CreateUnmatchedFile(ctx, dbgen.CreateUnmatchedFileParams{
		LibraryID:        pgtypeFromUUID(params.LibraryID),
		Path:             params.Path,
		FileSize:         params.FileSize,
		SuggestedMatches: suggestionsJSON,
	})
	if err != nil {
		return model.UnmatchedFile{}, apperrors.FromPg(err, "create unmatched file %q in library %s", params.Path, params.LibraryID)
	}
	return toModelUnmatchedFile(row), nil
}

func (r *Repository) UpsertUnmatchedFile(ctx context.Context, params UpsertUnmatchedFileParams) (model.UnmatchedFile, error) {
	suggestionsJSON, err := marshalSuggestions(params.SuggestedMatches, "Repository.UpsertUnmatchedFile")
	if err != nil {
		return model.UnmatchedFile{}, err
	}
	row, err := r.Q.UpsertUnmatchedFile(ctx, dbgen.UpsertUnmatchedFileParams{
		LibraryID:        pgtypeFromUUID(params.LibraryID),
		Path:             params.Path,
		FileSize:         params.FileSize,
		SuggestedMatches: suggestionsJSON,
	})
	if err != nil {
		return model.UnmatchedFile{}, apperrors.FromPg(err, "upsert unmatched file %q in library %s", params.Path, params.LibraryID)
	}
	return toModelUnmatchedFile(row), nil
}

func (r *Repository) GetUnmatchedFile(ctx context.Context, id uuid.UUID) (model.UnmatchedFile, error) {
	row, err := r.Q.GetUnmatchedFile(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.UnmatchedFile{}, apperrors.FromPg(err, "unmatched file %s not found", id)
	}
	return toModelUnmatchedFile(row), nil
}

func (r *Repository) GetUnmatchedFileByPath(ctx context.Context, params GetUnmatchedFileByPathParams) (model.UnmatchedFile, error) {
	row, err := r.Q.GetUnmatchedFileByPath(ctx, dbgen.GetUnmatchedFileByPathParams{
		LibraryID: pgtypeFromUUID(params.LibraryID),
		Path:      params.Path,
	})
	if err != nil {
		return model.UnmatchedFile{}, apperrors.FromPg(err, "unmatched file at %q in library %s not found", params.Path, params.LibraryID)
	}
	return toModelUnmatchedFile(row), nil
}

func (r *Repository) ListUnmatchedFiles(ctx context.Context) ([]model.UnmatchedFile, error) {
	rows, err := r.Q.ListUnmatchedFiles(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list unmatched files")
	}
	out := make([]model.UnmatchedFile, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelUnmatchedFile(row))
	}
	return out, nil
}

func (r *Repository) ListUnmatchedFilesForLibrary(ctx context.Context, libraryID uuid.UUID) ([]model.UnmatchedFile, error) {
	rows, err := r.Q.ListUnmatchedFilesForLibrary(ctx, pgtypeFromUUID(libraryID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list unmatched files for library %s", libraryID)
	}
	out := make([]model.UnmatchedFile, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelUnmatchedFile(row))
	}
	return out, nil
}

func (r *Repository) ListUnmatchedFilesPaginated(ctx context.Context, params UnmatchedFilesQueryParams) ([]model.UnmatchedFile, error) {
	var libID pgtype.UUID
	if params.LibraryID != nil {
		libID = pgtypeFromUUID(*params.LibraryID)
	}
	rows, err := r.Q.ListUnmatchedFilesPaginated(ctx, dbgen.ListUnmatchedFilesPaginatedParams{
		LibraryID: libID,
		PageSize:  params.PageSize,
		OffsetVal: params.Offset,
	})
	if err != nil {
		return nil, apperrors.FromPg(err, "list unmatched files paginated")
	}
	out := make([]model.UnmatchedFile, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelUnmatchedFile(row))
	}
	return out, nil
}

func (r *Repository) CountUnmatchedFiles(ctx context.Context, libraryID *uuid.UUID) (int64, error) {
	var libID pgtype.UUID
	if libraryID != nil {
		libID = pgtypeFromUUID(*libraryID)
	}
	count, err := r.Q.CountUnmatchedFiles(ctx, libID)
	return count, apperrors.FromPg(err, "count unmatched files")
}

func (r *Repository) ResolveUnmatchedFile(ctx context.Context, params ResolveUnmatchedFileParams) (model.UnmatchedFile, error) {
	row, err := r.Q.ResolveUnmatchedFile(ctx, dbgen.ResolveUnmatchedFileParams{
		ID:                  pgtypeFromUUID(params.ID),
		ResolvedMediaFileID: pgtypeFromUUID(params.ResolvedMediaFileID),
	})
	if err != nil {
		return model.UnmatchedFile{}, apperrors.FromPg(err, "resolve unmatched file %s", params.ID)
	}
	return toModelUnmatchedFile(row), nil
}

func (r *Repository) DismissUnmatchedFile(ctx context.Context, id uuid.UUID) (model.UnmatchedFile, error) {
	row, err := r.Q.DismissUnmatchedFile(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.UnmatchedFile{}, apperrors.FromPg(err, "dismiss unmatched file %s", id)
	}
	return toModelUnmatchedFile(row), nil
}

func (r *Repository) UpdateUnmatchedFileSuggestions(ctx context.Context, params UpdateUnmatchedFileSuggestionsParams) (model.UnmatchedFile, error) {
	suggestionsJSON, err := marshalSuggestions(params.SuggestedMatches, "Repository.UpdateUnmatchedFileSuggestions")
	if err != nil {
		return model.UnmatchedFile{}, err
	}
	row, err := r.Q.UpdateUnmatchedFileSuggestions(ctx, dbgen.UpdateUnmatchedFileSuggestionsParams{
		ID:               pgtypeFromUUID(params.ID),
		SuggestedMatches: suggestionsJSON,
	})
	if err != nil {
		return model.UnmatchedFile{}, apperrors.FromPg(err, "update suggestions for unmatched file %s", params.ID)
	}
	return toModelUnmatchedFile(row), nil
}

func (r *Repository) DeleteUnmatchedFile(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteUnmatchedFile(ctx, pgtypeFromUUID(id)), "delete unmatched file %s", id)
}

func (r *Repository) DeleteResolvedUnmatchedFilesOlderThan(ctx context.Context, beforeTime time.Time) error {
	return apperrors.FromPg(r.Q.DeleteResolvedUnmatchedFilesOlderThan(ctx, pgtype.Timestamptz{Time: beforeTime, Valid: true}), "delete resolved unmatched files older than %s", beforeTime)
}
