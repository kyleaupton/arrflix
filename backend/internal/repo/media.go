package repo

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
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
	return r.Q.ListMediaItems(ctx)
}

func (r *Repository) ListMediaItemsPaginated(ctx context.Context, params LibraryQueryParams) ([]dbgen.MediaItem, error) {
	return r.Q.ListMediaItemsPaginated(ctx, dbgen.ListMediaItemsPaginatedParams{
		TypeFilter: params.TypeFilter,
		Search:     params.Search,
		SortBy:     params.SortBy,
		SortDir:    params.SortDir,
		PageSize:   params.PageSize,
		OffsetVal:  params.Offset,
	})
}

func (r *Repository) CountMediaItems(ctx context.Context, typeFilter, search *string) (int64, error) {
	return r.Q.CountMediaItems(ctx, dbgen.CountMediaItemsParams{
		TypeFilter: typeFilter,
		Search:     search,
	})
}

func (r *Repository) GetMediaItem(ctx context.Context, id pgtype.UUID) (dbgen.MediaItem, error) {
	return r.Q.GetMediaItem(ctx, id)
}

func (r *Repository) GetMediaItemByTmdbID(ctx context.Context, tmdbID int64) (dbgen.MediaItem, error) {
	return r.Q.GetMediaItemByTmdbID(ctx, &tmdbID)
}

func (r *Repository) GetMediaItemByTmdbIDAndType(ctx context.Context, tmdbID int64, typ string) (dbgen.MediaItem, error) {
	return r.Q.GetMediaItemByTmdbIDAndType(ctx, dbgen.GetMediaItemByTmdbIDAndTypeParams{
		TmdbID: &tmdbID,
		Type:   typ,
	})
}

func (r *Repository) CreateMediaItem(ctx context.Context, typ, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error) {
	return r.Q.CreateMediaItem(ctx, dbgen.CreateMediaItemParams{
		Type:   typ,
		Title:  title,
		Year:   year,
		TmdbID: tmdbID,
	})
}

func (r *Repository) UpsertMediaItem(ctx context.Context, typ, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error) {
	return r.Q.UpsertMediaItem(ctx, dbgen.UpsertMediaItemParams{
		Type:   typ,
		Title:  title,
		Year:   year,
		TmdbID: tmdbID,
	})
}

func (r *Repository) UpdateMediaItem(ctx context.Context, id pgtype.UUID, title string, year *int32, tmdbID *int64) (dbgen.MediaItem, error) {
	return r.Q.UpdateMediaItem(ctx, dbgen.UpdateMediaItemParams{
		ID:     id,
		Title:  title,
		Year:   year,
		TmdbID: tmdbID,
	})
}

func (r *Repository) DeleteMediaItem(ctx context.Context, id pgtype.UUID) error {
	return r.Q.DeleteMediaItem(ctx, id)
}

func (r *Repository) UpdateMediaItemMetadata(ctx context.Context, params dbgen.UpdateMediaItemMetadataParams) (dbgen.MediaItem, error) {
	return r.Q.UpdateMediaItemMetadata(ctx, params)
}

func (r *Repository) ListStaleMediaItems(ctx context.Context, staleBefore time.Time, batchSize int32) ([]dbgen.MediaItem, error) {
	return r.Q.ListStaleMediaItems(ctx, dbgen.ListStaleMediaItemsParams{
		StaleBefore: pgtype.Timestamptz{Time: staleBefore, Valid: true},
		BatchSize:   batchSize,
	})
}

func (r *Repository) UpsertMediaMetadataSource(ctx context.Context, mediaItemID pgtype.UUID, source string, data []byte) error {
	return r.Q.UpsertMediaMetadataSource(ctx, dbgen.UpsertMediaMetadataSourceParams{
		MediaItemID: mediaItemID,
		Source:      source,
		Data:        data,
	})
}

func (r *Repository) ListSeasonsForMedia(ctx context.Context, mediaID pgtype.UUID) ([]dbgen.MediaSeason, error) {
	return r.Q.ListSeasonsForMedia(ctx, mediaID)
}

func (r *Repository) GetSeason(ctx context.Context, id pgtype.UUID) (dbgen.MediaSeason, error) {
	return r.Q.GetSeason(ctx, id)
}

func (r *Repository) GetSeasonByNumber(ctx context.Context, mediaItemID pgtype.UUID, seasonNumber int32) (dbgen.MediaSeason, error) {
	return r.Q.GetSeasonByNumber(ctx, dbgen.GetSeasonByNumberParams{
		MediaItemID:  mediaItemID,
		SeasonNumber: seasonNumber,
	})
}

func (r *Repository) UpsertSeason(ctx context.Context, mediaItemID pgtype.UUID, seasonNumber int32, airDate pgtype.Date) (dbgen.MediaSeason, error) {
	return r.Q.UpsertSeason(ctx, dbgen.UpsertSeasonParams{
		MediaItemID:  mediaItemID,
		SeasonNumber: seasonNumber,
		AirDate:      airDate,
	})
}

func (r *Repository) ListEpisodesForSeason(ctx context.Context, seasonID pgtype.UUID) ([]dbgen.MediaEpisode, error) {
	return r.Q.ListEpisodesForSeason(ctx, seasonID)
}

func (r *Repository) GetEpisode(ctx context.Context, id pgtype.UUID) (dbgen.MediaEpisode, error) {
	return r.Q.GetEpisode(ctx, id)
}

func (r *Repository) GetEpisodeByNumber(ctx context.Context, seasonID pgtype.UUID, episodeNumber int32) (dbgen.MediaEpisode, error) {
	return r.Q.GetEpisodeByNumber(ctx, dbgen.GetEpisodeByNumberParams{
		SeasonID:      seasonID,
		EpisodeNumber: episodeNumber,
	})
}

func (r *Repository) UpsertEpisode(ctx context.Context, seasonID pgtype.UUID, episodeNumber int32, title *string, airDate pgtype.Date, tmdbID *int64, tvdbID *int64) (dbgen.MediaEpisode, error) {
	return r.Q.UpsertEpisode(ctx, dbgen.UpsertEpisodeParams{
		SeasonID:      seasonID,
		EpisodeNumber: episodeNumber,
		Title:         title,
		AirDate:       airDate,
		TmdbID:        tmdbID,
		TvdbID:        tvdbID,
	})
}

func (r *Repository) GetMediaFile(ctx context.Context, id pgtype.UUID) (dbgen.MediaFile, error) {
	return r.Q.GetMediaFile(ctx, id)
}

func (r *Repository) GetMediaFileByLibraryAndPath(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.MediaFile, error) {
	return r.Q.GetMediaFileByLibraryAndPath(ctx, dbgen.GetMediaFileByLibraryAndPathParams{
		LibraryID: libraryID,
		Path:      path,
	})
}

func (r *Repository) CreateMediaFile(ctx context.Context, libraryID, mediaItemID pgtype.UUID, episodeID *pgtype.UUID, path string) (dbgen.MediaFile, error) {
	var episode pgtype.UUID
	if episodeID != nil {
		episode = *episodeID
	}
	return r.Q.CreateMediaFile(ctx, dbgen.CreateMediaFileParams{
		LibraryID:   libraryID,
		MediaItemID: mediaItemID,
		EpisodeID:   episode,
		Path:        path,
	})
}

func (r *Repository) ListMediaFilesForItem(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.ListMediaFilesForItemRow, error) {
	return r.Q.ListMediaFilesForItem(ctx, mediaItemID)
}

func (r *Repository) ListEpisodeAvailabilityForSeries(ctx context.Context, mediaItemID pgtype.UUID) ([]dbgen.ListEpisodeAvailabilityForSeriesRow, error) {
	return r.Q.ListEpisodeAvailabilityForSeries(ctx, mediaItemID)
}

func (r *Repository) DeleteMediaFile(ctx context.Context, id pgtype.UUID) error {
	return r.Q.DeleteMediaFile(ctx, id)
}

func (r *Repository) ListMediaFilePathsForLibrary(ctx context.Context, libraryID pgtype.UUID) ([]string, error) {
	return r.Q.ListMediaFilePathsForLibrary(ctx, libraryID)
}

func (r *Repository) ListUnmatchedFilePathsForLibrary(ctx context.Context, libraryID pgtype.UUID) ([]string, error) {
	return r.Q.ListUnmatchedFilePathsForLibrary(ctx, libraryID)
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
		return nil, err
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
	return r.Q.CreateMediaFileState(ctx, dbgen.CreateMediaFileStateParams{
		MediaFileID: mediaFileID,
		FileExists:  fileExists,
		FileSize:    fileSize,
	})
}

func (r *Repository) UpsertMediaFileState(ctx context.Context, mediaFileID pgtype.UUID, fileExists bool, fileSize *int64) (dbgen.MediaFileState, error) {
	return r.Q.UpsertMediaFileState(ctx, dbgen.UpsertMediaFileStateParams{
		MediaFileID: mediaFileID,
		FileExists:  fileExists,
		FileSize:    fileSize,
	})
}

func (r *Repository) GetMediaFileState(ctx context.Context, mediaFileID pgtype.UUID) (dbgen.MediaFileState, error) {
	return r.Q.GetMediaFileState(ctx, mediaFileID)
}

func (r *Repository) UpdateMediaFileState(ctx context.Context, mediaFileID pgtype.UUID, fileExists bool, fileSize *int64) (dbgen.MediaFileState, error) {
	return r.Q.UpdateMediaFileState(ctx, dbgen.UpdateMediaFileStateParams{
		MediaFileID: mediaFileID,
		FileExists:  fileExists,
		FileSize:    fileSize,
	})
}

func (r *Repository) ListMissingFiles(ctx context.Context) ([]dbgen.ListMissingFilesRow, error) {
	return r.Q.ListMissingFiles(ctx)
}

func (r *Repository) ListFilesNeedingVerification(ctx context.Context, beforeTime time.Time, limit int32) ([]dbgen.ListFilesNeedingVerificationRow, error) {
	return r.Q.ListFilesNeedingVerification(ctx, dbgen.ListFilesNeedingVerificationParams{
		BeforeTime: beforeTime,
		LimitVal:   limit,
	})
}

// Media File Import methods

func (r *Repository) CreateMediaFileImport(ctx context.Context, arg dbgen.CreateMediaFileImportParams) (dbgen.MediaFileImport, error) {
	return r.Q.CreateMediaFileImport(ctx, arg)
}

func (r *Repository) GetMediaFileImport(ctx context.Context, id pgtype.UUID) (dbgen.MediaFileImport, error) {
	return r.Q.GetMediaFileImport(ctx, id)
}

func (r *Repository) ListImportsForMediaFile(ctx context.Context, mediaFileID pgtype.UUID) ([]dbgen.MediaFileImport, error) {
	return r.Q.ListImportsForMediaFile(ctx, mediaFileID)
}

func (r *Repository) ListImportsForImportTask(ctx context.Context, importTaskID pgtype.UUID) ([]dbgen.MediaFileImport, error) {
	return r.Q.ListImportsForImportTask(ctx, importTaskID)
}

func (r *Repository) ListRecentImports(ctx context.Context, limit int32) ([]dbgen.MediaFileImport, error) {
	return r.Q.ListRecentImports(ctx, limit)
}

func (r *Repository) ListFailedImports(ctx context.Context, limit int32) ([]dbgen.MediaFileImport, error) {
	return r.Q.ListFailedImports(ctx, limit)
}

// Unmatched File methods

func (r *Repository) CreateUnmatchedFile(ctx context.Context, libraryID pgtype.UUID, path string, fileSize *int64, suggestedMatches []byte) (dbgen.UnmatchedFile, error) {
	return r.Q.CreateUnmatchedFile(ctx, dbgen.CreateUnmatchedFileParams{
		LibraryID:        libraryID,
		Path:             path,
		FileSize:         fileSize,
		SuggestedMatches: suggestedMatches,
	})
}

func (r *Repository) UpsertUnmatchedFile(ctx context.Context, libraryID pgtype.UUID, path string, fileSize *int64, suggestedMatches []byte) (dbgen.UnmatchedFile, error) {
	return r.Q.UpsertUnmatchedFile(ctx, dbgen.UpsertUnmatchedFileParams{
		LibraryID:        libraryID,
		Path:             path,
		FileSize:         fileSize,
		SuggestedMatches: suggestedMatches,
	})
}

func (r *Repository) GetUnmatchedFile(ctx context.Context, id pgtype.UUID) (dbgen.UnmatchedFile, error) {
	return r.Q.GetUnmatchedFile(ctx, id)
}

func (r *Repository) GetUnmatchedFileByPath(ctx context.Context, libraryID pgtype.UUID, path string) (dbgen.UnmatchedFile, error) {
	return r.Q.GetUnmatchedFileByPath(ctx, dbgen.GetUnmatchedFileByPathParams{
		LibraryID: libraryID,
		Path:      path,
	})
}

func (r *Repository) ListUnmatchedFiles(ctx context.Context) ([]dbgen.UnmatchedFile, error) {
	return r.Q.ListUnmatchedFiles(ctx)
}

func (r *Repository) ListUnmatchedFilesForLibrary(ctx context.Context, libraryID pgtype.UUID) ([]dbgen.UnmatchedFile, error) {
	return r.Q.ListUnmatchedFilesForLibrary(ctx, libraryID)
}

func (r *Repository) ListUnmatchedFilesPaginated(ctx context.Context, params UnmatchedFilesQueryParams) ([]dbgen.UnmatchedFile, error) {
	var libID pgtype.UUID
	if params.LibraryID != nil {
		libID = *params.LibraryID
	}
	return r.Q.ListUnmatchedFilesPaginated(ctx, dbgen.ListUnmatchedFilesPaginatedParams{
		LibraryID: libID,
		PageSize:  params.PageSize,
		OffsetVal: params.Offset,
	})
}

func (r *Repository) CountUnmatchedFiles(ctx context.Context, libraryID *pgtype.UUID) (int64, error) {
	var libID pgtype.UUID
	if libraryID != nil {
		libID = *libraryID
	}
	return r.Q.CountUnmatchedFiles(ctx, libID)
}

func (r *Repository) ResolveUnmatchedFile(ctx context.Context, id pgtype.UUID, resolvedMediaFileID pgtype.UUID) (dbgen.UnmatchedFile, error) {
	return r.Q.ResolveUnmatchedFile(ctx, dbgen.ResolveUnmatchedFileParams{
		ID:                  id,
		ResolvedMediaFileID: resolvedMediaFileID,
	})
}

func (r *Repository) DismissUnmatchedFile(ctx context.Context, id pgtype.UUID) (dbgen.UnmatchedFile, error) {
	return r.Q.DismissUnmatchedFile(ctx, id)
}

func (r *Repository) UpdateUnmatchedFileSuggestions(ctx context.Context, id pgtype.UUID, suggestedMatches []byte) (dbgen.UnmatchedFile, error) {
	return r.Q.UpdateUnmatchedFileSuggestions(ctx, dbgen.UpdateUnmatchedFileSuggestionsParams{
		ID:               id,
		SuggestedMatches: suggestedMatches,
	})
}

func (r *Repository) DeleteUnmatchedFile(ctx context.Context, id pgtype.UUID) error {
	return r.Q.DeleteUnmatchedFile(ctx, id)
}

func (r *Repository) DeleteResolvedUnmatchedFilesOlderThan(ctx context.Context, beforeTime time.Time) error {
	return r.Q.DeleteResolvedUnmatchedFilesOlderThan(ctx, pgtype.Timestamptz{Time: beforeTime, Valid: true})
}
