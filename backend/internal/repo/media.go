package repo

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
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
	LibraryID *pgtype.UUID
	PageSize  int32
	Offset    int32
}

type MediaRepo interface {
	// Media items
	ListMediaItems(ctx context.Context) ([]dbgen.MediaItem, error)
	ListMediaItemsPaginated(ctx context.Context, params LibraryQueryParams) ([]dbgen.MediaItem, error)
	CountMediaItems(ctx context.Context, typeFilter, search *string) (int64, error)
	GetMediaItem(ctx context.Context, id pgtype.UUID) (dbgen.MediaItem, error)
	GetMediaItemByTmdbID(ctx context.Context, tmdbID int64) (dbgen.MediaItem, error)
	GetMediaItemByTmdbIDAndType(ctx context.Context, tmdbID int64, typ string) (dbgen.MediaItem, error)
	CreateMediaItem(ctx context.Context, typ, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error)
	UpsertMediaItem(ctx context.Context, typ, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error)
	UpdateMediaItem(ctx context.Context, id pgtype.UUID, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error)
	DeleteMediaItem(ctx context.Context, id pgtype.UUID) error

	// Metadata enrichment
	UpdateMediaItemMetadata(ctx context.Context, params dbgen.UpdateMediaItemMetadataParams) (dbgen.MediaItem, error)
	ListStaleMediaItems(ctx context.Context, staleBefore time.Time, batchSize int32) ([]dbgen.MediaItem, error)
	UpsertMediaMetadataSource(ctx context.Context, mediaItemID pgtype.UUID, source string, data []byte) error

	// Seasons
	ListSeasonsForMedia(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.MediaSeason, error)
	GetSeason(ctx context.Context, id pgtype.UUID) (dbgen.MediaSeason, error)
	GetSeasonByNumber(ctx context.Context, mediaItemID pgtype.UUID, seasonNumber int32) (dbgen.MediaSeason, error)
	UpsertSeason(ctx context.Context, mediaItemID pgtype.UUID, seasonNumber int32, airDate pgtype.Date) (dbgen.MediaSeason, error)

	// Episodes
	ListEpisodesForSeason(ctx context.Context, seasonID pgtype.UUID) ([]dbgen.MediaEpisode, error)
	GetEpisode(ctx context.Context, id pgtype.UUID) (dbgen.MediaEpisode, error)
	GetEpisodeByNumber(ctx context.Context, seasonID pgtype.UUID, episodeNumber int32) (dbgen.MediaEpisode, error)
	UpsertEpisode(ctx context.Context, seasonID pgtype.UUID, episodeNumber int32, title *string, airDate pgtype.Date, tmdbID *int64, tvdbID *int64) (dbgen.MediaEpisode, error)

	// File path loading for scanner
	ListMediaFilePathsForLibrary(ctx context.Context, libraryID pgtype.UUID) ([]string, error)
	ListUnmatchedFilePathsForLibrary(ctx context.Context, libraryID pgtype.UUID) ([]string, error)

	// Files (removed season_id and status)
	GetMediaFile(ctx context.Context, id pgtype.UUID) (dbgen.MediaFile, error)
	GetMediaFileByLibraryAndPath(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.MediaFile, error)
	CreateMediaFile(ctx context.Context, libraryID, mediaItemID pgtype.UUID, episodeID *pgtype.UUID, path string) (dbgen.MediaFile, error)
	ListMediaFilesForItem(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.ListMediaFilesForItemRow, error)
	ListEpisodeAvailabilityForSeries(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.ListEpisodeAvailabilityForSeriesRow, error)
	DeleteMediaFile(ctx context.Context, id pgtype.UUID) error

	// File state
	CreateMediaFileState(ctx context.Context, mediaFileID pgtype.UUID, fileExists bool, fileSize *int64) (dbgen.MediaFileState, error)
	UpsertMediaFileState(ctx context.Context, mediaFileID pgtype.UUID, fileExists bool, fileSize *int64) (dbgen.MediaFileState, error)
	GetMediaFileState(ctx context.Context, mediaFileID pgtype.UUID) (dbgen.MediaFileState, error)
	UpdateMediaFileState(ctx context.Context, mediaFileID pgtype.UUID, fileExists bool, fileSize *int64) (dbgen.MediaFileState, error)
	ListMissingFiles(ctx context.Context) ([]dbgen.ListMissingFilesRow, error)
	ListFilesNeedingVerification(ctx context.Context, beforeTime time.Time, limit int32) ([]dbgen.ListFilesNeedingVerificationRow, error)

	// File imports
	CreateMediaFileImport(ctx context.Context, arg dbgen.CreateMediaFileImportParams) (dbgen.MediaFileImport, error)
	GetMediaFileImport(ctx context.Context, id pgtype.UUID) (dbgen.MediaFileImport, error)
	ListImportsForMediaFile(ctx context.Context, mediaFileID pgtype.UUID) ([]dbgen.MediaFileImport, error)
	ListImportsForImportTask(ctx context.Context, importTaskID pgtype.UUID) ([]dbgen.MediaFileImport, error)
	ListRecentImports(ctx context.Context, limit int32) ([]dbgen.MediaFileImport, error)
	ListFailedImports(ctx context.Context, limit int32) ([]dbgen.MediaFileImport, error)

	// Unmatched files
	CreateUnmatchedFile(ctx context.Context, libraryID pgtype.UUID, path string, fileSize *int64, suggestedMatches []byte) (dbgen.UnmatchedFile, error)
	UpsertUnmatchedFile(ctx context.Context, libraryID pgtype.UUID, path string, fileSize *int64, suggestedMatches []byte) (dbgen.UnmatchedFile, error)
	GetUnmatchedFile(ctx context.Context, id pgtype.UUID) (dbgen.UnmatchedFile, error)
	GetUnmatchedFileByPath(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.UnmatchedFile, error)
	ListUnmatchedFiles(ctx context.Context) ([]dbgen.UnmatchedFile, error)
	ListUnmatchedFilesForLibrary(ctx context.Context, libraryID pgtype.UUID) ([]dbgen.UnmatchedFile, error)
	ListUnmatchedFilesPaginated(ctx context.Context, params UnmatchedFilesQueryParams) ([]dbgen.UnmatchedFile, error)
	CountUnmatchedFiles(ctx context.Context, libraryID *pgtype.UUID) (int64, error)
	ResolveUnmatchedFile(ctx context.Context, id pgtype.UUID, resolvedMediaFileID pgtype.UUID) (dbgen.UnmatchedFile, error)
	DismissUnmatchedFile(ctx context.Context, id pgtype.UUID) (dbgen.UnmatchedFile, error)
	UpdateUnmatchedFileSuggestions(ctx context.Context, id pgtype.UUID, suggestedMatches []byte) (dbgen.UnmatchedFile, error)
	DeleteUnmatchedFile(ctx context.Context, id pgtype.UUID) error
	DeleteResolvedUnmatchedFilesOlderThan(ctx context.Context, beforeTime time.Time) error
}

func (r *Repository) ListMediaItems(ctx context.Context) ([]dbgen.MediaItem, error) {
	items, err := r.Q.ListMediaItems(ctx)
	return items, apperrors.FromPg(err, "list media items")
}

func (r *Repository) ListMediaItemsPaginated(ctx context.Context, params LibraryQueryParams) ([]dbgen.MediaItem, error) {
	items, err := r.Q.ListMediaItemsPaginated(ctx, dbgen.ListMediaItemsPaginatedParams{
		TypeFilter: params.TypeFilter,
		Search:     params.Search,
		SortBy:     params.SortBy,
		SortDir:    params.SortDir,
		PageSize:   params.PageSize,
		OffsetVal:  params.Offset,
	})
	return items, apperrors.FromPg(err, "list media items paginated")
}

func (r *Repository) CountMediaItems(ctx context.Context, typeFilter, search *string) (int64, error) {
	count, err := r.Q.CountMediaItems(ctx, dbgen.CountMediaItemsParams{
		TypeFilter: typeFilter,
		Search:     search,
	})
	return count, apperrors.FromPg(err, "count media items")
}

func (r *Repository) GetMediaItem(ctx context.Context, id pgtype.UUID) (dbgen.MediaItem, error) {
	item, err := r.Q.GetMediaItem(ctx, id)
	return item, apperrors.FromPg(err, "media item %s not found", id)
}

func (r *Repository) GetMediaItemByTmdbID(ctx context.Context, tmdbID int64) (dbgen.MediaItem, error) {
	item, err := r.Q.GetMediaItemByTmdbID(ctx, &tmdbID)
	return item, apperrors.FromPg(err, "media item with tmdb id %d not found", tmdbID)
}

func (r *Repository) GetMediaItemByTmdbIDAndType(ctx context.Context, tmdbID int64, typ string) (dbgen.MediaItem, error) {
	item, err := r.Q.GetMediaItemByTmdbIDAndType(ctx, dbgen.GetMediaItemByTmdbIDAndTypeParams{
		TmdbID: &tmdbID,
		Type:   typ,
	})
	return item, apperrors.FromPg(err, "media item with tmdb id %d (type %s) not found", tmdbID, typ)
}

func (r *Repository) CreateMediaItem(ctx context.Context, typ, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error) {
	item, err := r.Q.CreateMediaItem(ctx, dbgen.CreateMediaItemParams{
		Type:   typ,
		Title:  title,
		Year:   year,
		TmdbID: tmdbID,
	})
	return item, apperrors.FromPg(err, "create media item %q", title)
}

func (r *Repository) UpsertMediaItem(ctx context.Context, typ, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error) {
	item, err := r.Q.UpsertMediaItem(ctx, dbgen.UpsertMediaItemParams{
		Type:   typ,
		Title:  title,
		Year:   year,
		TmdbID: tmdbID,
	})
	return item, apperrors.FromPg(err, "upsert media item %q", title)
}

func (r *Repository) UpdateMediaItem(ctx context.Context, id pgtype.UUID, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error) {
	item, err := r.Q.UpdateMediaItem(ctx, dbgen.UpdateMediaItemParams{
		ID:     id,
		Title:  title,
		Year:   year,
		TmdbID: tmdbID,
	})
	return item, apperrors.FromPg(err, "update media item %s", id)
}

func (r *Repository) DeleteMediaItem(ctx context.Context, id pgtype.UUID) error {
	return apperrors.FromPg(r.Q.DeleteMediaItem(ctx, id), "delete media item %s", id)
}

func (r *Repository) UpdateMediaItemMetadata(ctx context.Context, params dbgen.UpdateMediaItemMetadataParams) (dbgen.MediaItem, error) {
	item, err := r.Q.UpdateMediaItemMetadata(ctx, params)
	return item, apperrors.FromPg(err, "update metadata for media item %s", params.ID)
}

func (r *Repository) ListStaleMediaItems(ctx context.Context, staleBefore time.Time, batchSize int32) ([]dbgen.MediaItem, error) {
	items, err := r.Q.ListStaleMediaItems(ctx, dbgen.ListStaleMediaItemsParams{
		StaleBefore: pgtype.Timestamptz{Time: staleBefore, Valid: true},
		BatchSize:   batchSize,
	})
	return items, apperrors.FromPg(err, "list stale media items")
}

func (r *Repository) UpsertMediaMetadataSource(ctx context.Context, mediaItemID pgtype.UUID, source string, data []byte) error {
	return apperrors.FromPg(r.Q.UpsertMediaMetadataSource(ctx, dbgen.UpsertMediaMetadataSourceParams{
		MediaItemID: mediaItemID,
		Source:      source,
		Data:        data,
	}), "upsert metadata source %q for media item %s", source, mediaItemID)
}

func (r *Repository) ListSeasonsForMedia(ctx context.Context, mediaID pgtype.UUID) ([]dbgen.MediaSeason, error) {
	seasons, err := r.Q.ListSeasonsForMedia(ctx, mediaID)
	return seasons, apperrors.FromPg(err, "list seasons for media %s", mediaID)
}

func (r *Repository) GetSeason(ctx context.Context, id pgtype.UUID) (dbgen.MediaSeason, error) {
	season, err := r.Q.GetSeason(ctx, id)
	return season, apperrors.FromPg(err, "season %s not found", id)
}

func (r *Repository) GetSeasonByNumber(ctx context.Context, mediaItemID pgtype.UUID, seasonNumber int32) (dbgen.MediaSeason, error) {
	season, err := r.Q.GetSeasonByNumber(ctx, dbgen.GetSeasonByNumberParams{
		MediaItemID:  mediaItemID,
		SeasonNumber: seasonNumber,
	})
	return season, apperrors.FromPg(err, "season %d for media %s not found", seasonNumber, mediaItemID)
}

func (r *Repository) UpsertSeason(ctx context.Context, mediaItemID pgtype.UUID, seasonNumber int32, airDate pgtype.Date) (dbgen.MediaSeason, error) {
	season, err := r.Q.UpsertSeason(ctx, dbgen.UpsertSeasonParams{
		MediaItemID:  mediaItemID,
		SeasonNumber: seasonNumber,
		AirDate:      airDate,
	})
	return season, apperrors.FromPg(err, "upsert season %d for media %s", seasonNumber, mediaItemID)
}

func (r *Repository) ListEpisodesForSeason(ctx context.Context, seasonID pgtype.UUID) ([]dbgen.MediaEpisode, error) {
	episodes, err := r.Q.ListEpisodesForSeason(ctx, seasonID)
	return episodes, apperrors.FromPg(err, "list episodes for season %s", seasonID)
}

func (r *Repository) GetEpisode(ctx context.Context, id pgtype.UUID) (dbgen.MediaEpisode, error) {
	ep, err := r.Q.GetEpisode(ctx, id)
	return ep, apperrors.FromPg(err, "episode %s not found", id)
}

func (r *Repository) GetEpisodeByNumber(ctx context.Context, seasonID pgtype.UUID, episodeNumber int32) (dbgen.MediaEpisode, error) {
	ep, err := r.Q.GetEpisodeByNumber(ctx, dbgen.GetEpisodeByNumberParams{
		SeasonID:      seasonID,
		EpisodeNumber: episodeNumber,
	})
	return ep, apperrors.FromPg(err, "episode %d for season %s not found", episodeNumber, seasonID)
}

func (r *Repository) UpsertEpisode(ctx context.Context, seasonID pgtype.UUID, episodeNumber int32, title *string, airDate pgtype.Date, tmdbID *int64, tvdbID *int64) (dbgen.MediaEpisode, error) {
	ep, err := r.Q.UpsertEpisode(ctx, dbgen.UpsertEpisodeParams{
		SeasonID:      seasonID,
		EpisodeNumber: episodeNumber,
		Title:         title,
		AirDate:       airDate,
		TmdbID:        tmdbID,
		TvdbID:        tvdbID,
	})
	return ep, apperrors.FromPg(err, "upsert episode %d for season %s", episodeNumber, seasonID)
}

func (r *Repository) GetMediaFile(ctx context.Context, id pgtype.UUID) (dbgen.MediaFile, error) {
	mf, err := r.Q.GetMediaFile(ctx, id)
	return mf, apperrors.FromPg(err, "media file %s not found", id)
}

func (r *Repository) GetMediaFileByLibraryAndPath(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.MediaFile, error) {
	mf, err := r.Q.GetMediaFileByLibraryAndPath(ctx, dbgen.GetMediaFileByLibraryAndPathParams{
		LibraryID: libraryID,
		Path:      path,
	})
	return mf, apperrors.FromPg(err, "media file at %q in library %s not found", path, libraryID)
}

func (r *Repository) CreateMediaFile(ctx context.Context, libraryID, mediaItemID pgtype.UUID, episodeID *pgtype.UUID, path string) (dbgen.MediaFile, error) {
	var episode pgtype.UUID
	if episodeID != nil {
		episode = *episodeID
	}
	mf, err := r.Q.CreateMediaFile(ctx, dbgen.CreateMediaFileParams{
		LibraryID:   libraryID,
		MediaItemID: mediaItemID,
		EpisodeID:   episode,
		Path:        path,
	})
	return mf, apperrors.FromPg(err, "create media file %q in library %s", path, libraryID)
}

func (r *Repository) ListMediaFilesForItem(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.ListMediaFilesForItemRow, error) {
	rows, err := r.Q.ListMediaFilesForItem(ctx, mediaItemID)
	return rows, apperrors.FromPg(err, "list media files for item %s", mediaItemID)
}

func (r *Repository) ListEpisodeAvailabilityForSeries(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.ListEpisodeAvailabilityForSeriesRow, error) {
	rows, err := r.Q.ListEpisodeAvailabilityForSeries(ctx, mediaItemID)
	return rows, apperrors.FromPg(err, "list episode availability for series %s", mediaItemID)
}

func (r *Repository) DeleteMediaFile(ctx context.Context, id pgtype.UUID) error {
	return apperrors.FromPg(r.Q.DeleteMediaFile(ctx, id), "delete media file %s", id)
}

func (r *Repository) ListMediaFilePathsForLibrary(ctx context.Context, libraryID pgtype.UUID) ([]string, error) {
	paths, err := r.Q.ListMediaFilePathsForLibrary(ctx, libraryID)
	return paths, apperrors.FromPg(err, "list media file paths for library %s", libraryID)
}

func (r *Repository) ListUnmatchedFilePathsForLibrary(ctx context.Context, libraryID pgtype.UUID) ([]string, error) {
	paths, err := r.Q.ListUnmatchedFilePathsForLibrary(ctx, libraryID)
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

func (r *Repository) CreateMediaFileState(ctx context.Context, mediaFileID pgtype.UUID, fileExists bool, fileSize *int64) (dbgen.MediaFileState, error) {
	state, err := r.Q.CreateMediaFileState(ctx, dbgen.CreateMediaFileStateParams{
		MediaFileID: mediaFileID,
		FileExists:  fileExists,
		FileSize:    fileSize,
	})
	return state, apperrors.FromPg(err, "create media file state for %s", mediaFileID)
}

func (r *Repository) UpsertMediaFileState(ctx context.Context, mediaFileID pgtype.UUID, fileExists bool, fileSize *int64) (dbgen.MediaFileState, error) {
	state, err := r.Q.UpsertMediaFileState(ctx, dbgen.UpsertMediaFileStateParams{
		MediaFileID: mediaFileID,
		FileExists:  fileExists,
		FileSize:    fileSize,
	})
	return state, apperrors.FromPg(err, "upsert media file state for %s", mediaFileID)
}

func (r *Repository) GetMediaFileState(ctx context.Context, mediaFileID pgtype.UUID) (dbgen.MediaFileState, error) {
	state, err := r.Q.GetMediaFileState(ctx, mediaFileID)
	return state, apperrors.FromPg(err, "media file state for %s not found", mediaFileID)
}

func (r *Repository) UpdateMediaFileState(ctx context.Context, mediaFileID pgtype.UUID, fileExists bool, fileSize *int64) (dbgen.MediaFileState, error) {
	state, err := r.Q.UpdateMediaFileState(ctx, dbgen.UpdateMediaFileStateParams{
		MediaFileID: mediaFileID,
		FileExists:  fileExists,
		FileSize:    fileSize,
	})
	return state, apperrors.FromPg(err, "update media file state for %s", mediaFileID)
}

func (r *Repository) ListMissingFiles(ctx context.Context) ([]dbgen.ListMissingFilesRow, error) {
	rows, err := r.Q.ListMissingFiles(ctx)
	return rows, apperrors.FromPg(err, "list missing files")
}

func (r *Repository) ListFilesNeedingVerification(ctx context.Context, beforeTime time.Time, limit int32) ([]dbgen.ListFilesNeedingVerificationRow, error) {
	rows, err := r.Q.ListFilesNeedingVerification(ctx, dbgen.ListFilesNeedingVerificationParams{
		BeforeTime: beforeTime,
		LimitVal:   limit,
	})
	return rows, apperrors.FromPg(err, "list files needing verification")
}

// Media File Import methods

func (r *Repository) CreateMediaFileImport(ctx context.Context, arg dbgen.CreateMediaFileImportParams) (dbgen.MediaFileImport, error) {
	imp, err := r.Q.CreateMediaFileImport(ctx, arg)
	return imp, apperrors.FromPg(err, "create media file import for task %s", arg.ImportTaskID)
}

func (r *Repository) GetMediaFileImport(ctx context.Context, id pgtype.UUID) (dbgen.MediaFileImport, error) {
	imp, err := r.Q.GetMediaFileImport(ctx, id)
	return imp, apperrors.FromPg(err, "media file import %s not found", id)
}

func (r *Repository) ListImportsForMediaFile(ctx context.Context, mediaFileID pgtype.UUID) ([]dbgen.MediaFileImport, error) {
	imps, err := r.Q.ListImportsForMediaFile(ctx, mediaFileID)
	return imps, apperrors.FromPg(err, "list imports for media file %s", mediaFileID)
}

func (r *Repository) ListImportsForImportTask(ctx context.Context, importTaskID pgtype.UUID) ([]dbgen.MediaFileImport, error) {
	imps, err := r.Q.ListImportsForImportTask(ctx, importTaskID)
	return imps, apperrors.FromPg(err, "list imports for import task %s", importTaskID)
}

func (r *Repository) ListRecentImports(ctx context.Context, limit int32) ([]dbgen.MediaFileImport, error) {
	imps, err := r.Q.ListRecentImports(ctx, limit)
	return imps, apperrors.FromPg(err, "list recent imports")
}

func (r *Repository) ListFailedImports(ctx context.Context, limit int32) ([]dbgen.MediaFileImport, error) {
	imps, err := r.Q.ListFailedImports(ctx, limit)
	return imps, apperrors.FromPg(err, "list failed imports")
}

// Unmatched File methods

func (r *Repository) CreateUnmatchedFile(ctx context.Context, libraryID pgtype.UUID, path string, fileSize *int64, suggestedMatches []byte) (dbgen.UnmatchedFile, error) {
	uf, err := r.Q.CreateUnmatchedFile(ctx, dbgen.CreateUnmatchedFileParams{
		LibraryID:        libraryID,
		Path:             path,
		FileSize:         fileSize,
		SuggestedMatches: suggestedMatches,
	})
	return uf, apperrors.FromPg(err, "create unmatched file %q in library %s", path, libraryID)
}

func (r *Repository) UpsertUnmatchedFile(ctx context.Context, libraryID pgtype.UUID, path string, fileSize *int64, suggestedMatches []byte) (dbgen.UnmatchedFile, error) {
	uf, err := r.Q.UpsertUnmatchedFile(ctx, dbgen.UpsertUnmatchedFileParams{
		LibraryID:        libraryID,
		Path:             path,
		FileSize:         fileSize,
		SuggestedMatches: suggestedMatches,
	})
	return uf, apperrors.FromPg(err, "upsert unmatched file %q in library %s", path, libraryID)
}

func (r *Repository) GetUnmatchedFile(ctx context.Context, id pgtype.UUID) (dbgen.UnmatchedFile, error) {
	uf, err := r.Q.GetUnmatchedFile(ctx, id)
	return uf, apperrors.FromPg(err, "unmatched file %s not found", id)
}

func (r *Repository) GetUnmatchedFileByPath(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.UnmatchedFile, error) {
	uf, err := r.Q.GetUnmatchedFileByPath(ctx, dbgen.GetUnmatchedFileByPathParams{
		LibraryID: libraryID,
		Path:      path,
	})
	return uf, apperrors.FromPg(err, "unmatched file at %q in library %s not found", path, libraryID)
}

func (r *Repository) ListUnmatchedFiles(ctx context.Context) ([]dbgen.UnmatchedFile, error) {
	ufs, err := r.Q.ListUnmatchedFiles(ctx)
	return ufs, apperrors.FromPg(err, "list unmatched files")
}

func (r *Repository) ListUnmatchedFilesForLibrary(ctx context.Context, libraryID pgtype.UUID) ([]dbgen.UnmatchedFile, error) {
	ufs, err := r.Q.ListUnmatchedFilesForLibrary(ctx, libraryID)
	return ufs, apperrors.FromPg(err, "list unmatched files for library %s", libraryID)
}

func (r *Repository) ListUnmatchedFilesPaginated(ctx context.Context, params UnmatchedFilesQueryParams) ([]dbgen.UnmatchedFile, error) {
	var libID pgtype.UUID
	if params.LibraryID != nil {
		libID = *params.LibraryID
	}
	ufs, err := r.Q.ListUnmatchedFilesPaginated(ctx, dbgen.ListUnmatchedFilesPaginatedParams{
		LibraryID: libID,
		PageSize:  params.PageSize,
		OffsetVal: params.Offset,
	})
	return ufs, apperrors.FromPg(err, "list unmatched files paginated")
}

func (r *Repository) CountUnmatchedFiles(ctx context.Context, libraryID *pgtype.UUID) (int64, error) {
	var libID pgtype.UUID
	if libraryID != nil {
		libID = *libraryID
	}
	count, err := r.Q.CountUnmatchedFiles(ctx, libID)
	return count, apperrors.FromPg(err, "count unmatched files")
}

func (r *Repository) ResolveUnmatchedFile(ctx context.Context, id pgtype.UUID, resolvedMediaFileID pgtype.UUID) (dbgen.UnmatchedFile, error) {
	uf, err := r.Q.ResolveUnmatchedFile(ctx, dbgen.ResolveUnmatchedFileParams{
		ID:                  id,
		ResolvedMediaFileID: resolvedMediaFileID,
	})
	return uf, apperrors.FromPg(err, "resolve unmatched file %s", id)
}

func (r *Repository) DismissUnmatchedFile(ctx context.Context, id pgtype.UUID) (dbgen.UnmatchedFile, error) {
	uf, err := r.Q.DismissUnmatchedFile(ctx, id)
	return uf, apperrors.FromPg(err, "dismiss unmatched file %s", id)
}

func (r *Repository) UpdateUnmatchedFileSuggestions(ctx context.Context, id pgtype.UUID, suggestedMatches []byte) (dbgen.UnmatchedFile, error) {
	uf, err := r.Q.UpdateUnmatchedFileSuggestions(ctx, dbgen.UpdateUnmatchedFileSuggestionsParams{
		ID:               id,
		SuggestedMatches: suggestedMatches,
	})
	return uf, apperrors.FromPg(err, "update suggestions for unmatched file %s", id)
}

func (r *Repository) DeleteUnmatchedFile(ctx context.Context, id pgtype.UUID) error {
	return apperrors.FromPg(r.Q.DeleteUnmatchedFile(ctx, id), "delete unmatched file %s", id)
}

func (r *Repository) DeleteResolvedUnmatchedFilesOlderThan(ctx context.Context, beforeTime time.Time) error {
	return apperrors.FromPg(r.Q.DeleteResolvedUnmatchedFilesOlderThan(ctx, pgtype.Timestamptz{Time: beforeTime, Valid: true}), "delete resolved unmatched files older than %s", beforeTime)
}
