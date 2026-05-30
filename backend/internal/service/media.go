package service

import (
	"context"
	"math"
	"sort"
	"strconv"
	"time"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type MediaService struct {
	repo     *repo.Repository
	logger   *logger.Logger
	tmdb     *TmdbService
	settings *SettingsService
}

func NewMediaService(r *repo.Repository, l *logger.Logger, tmdb *TmdbService, settings *SettingsService) *MediaService {
	return &MediaService{repo: r, logger: l, tmdb: tmdb, settings: settings}
}

func (s *MediaService) ListLibraryItems(ctx context.Context) ([]model.MediaItem, error) {
	return s.repo.ListMediaItems(ctx)
}

// LibraryQueryParams contains query parameters for the paginated library endpoint
type LibraryQueryParams struct {
	Page     int
	PageSize int
	Type     string
	Search   string
	SortBy   string
	SortDir  string
}

// ListLibraryItemsPaginated returns a paginated list of library items with TMDB enrichment
func (s *MediaService) ListLibraryItemsPaginated(ctx context.Context, params LibraryQueryParams) (model.PaginatedLibraryResponse, error) {
	// Validate and set defaults
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}
	if params.SortBy == "" {
		params.SortBy = "createdAt"
	}
	if params.SortDir == "" {
		params.SortDir = "desc"
	}

	offset := (params.Page - 1) * params.PageSize

	// Convert empty strings to nil for SQL
	var typeFilter, search *string
	if params.Type != "" {
		typeFilter = &params.Type
	}
	if params.Search != "" {
		search = &params.Search
	}

	// Get total count for pagination metadata
	total, err := s.repo.CountMediaItems(ctx, typeFilter, search)
	if err != nil {
		return model.PaginatedLibraryResponse{}, err
	}

	// Get paginated items
	dbItems, err := s.repo.ListMediaItemsPaginated(ctx, repo.LibraryQueryParams{
		TypeFilter: typeFilter,
		Search:     search,
		SortBy:     params.SortBy,
		SortDir:    params.SortDir,
		PageSize:   int32(params.PageSize),
		Offset:     int32(offset),
	})
	if err != nil {
		return model.PaginatedLibraryResponse{}, err
	}

	// Map DB items directly — poster_path is now stored on the media_item row
	items := make([]model.LibraryItem, len(dbItems))
	for i, item := range dbItems {
		items[i] = model.LibraryItem{
			ID:         item.ID.String(),
			Type:       item.Type,
			Title:      item.Title,
			Year:       item.Year,
			TmdbID:     item.TmdbID,
			PosterPath: coalesce(item.PosterPath, ""),
			CreatedAt:  item.CreatedAt.Format(time.RFC3339),
		}
	}

	// Calculate total pages
	totalPages := int(math.Ceil(float64(total) / float64(params.PageSize)))

	return model.PaginatedLibraryResponse{
		Data: items,
		Pagination: model.Pagination{
			Total:      total,
			Page:       params.Page,
			PageSize:   params.PageSize,
			TotalPages: totalPages,
		},
	}, nil
}

// extractMovieCertification extracts US certification (fallback to GB, CA, AU)
func extractMovieCertification(releaseDates *tmdb.MovieReleaseDates) string {
	if releaseDates == nil || releaseDates.Results == nil {
		return ""
	}
	priorityCountries := []string{"US", "GB", "CA", "AU"}
	for _, country := range priorityCountries {
		for _, result := range releaseDates.Results {
			if result.Iso3166_1 == country {
				for _, rd := range result.ReleaseDates {
					if rd.Certification != "" {
						return rd.Certification
					}
				}
			}
		}
	}
	return ""
}

// extractTVCertification extracts US certification (fallback to GB, CA, AU)
func extractTVCertification(contentRatings *tmdb.TVContentRatings) string {
	if contentRatings == nil || contentRatings.Results == nil {
		return ""
	}
	priorityCountries := []string{"US", "GB", "CA", "AU"}
	for _, country := range priorityCountries {
		for _, result := range contentRatings.Results {
			if result.Iso3166_1 == country && result.Rating != "" {
				return result.Rating
			}
		}
	}
	return ""
}

// extractEpisodeRuntime returns the first (most common) episode runtime
func extractEpisodeRuntime(runtimes []int) *int {
	if len(runtimes) == 0 {
		return nil
	}
	return &runtimes[0]
}

// extractWatchProviders extracts watch providers for a specific region from TMDB results
func extractWatchProviders(results *tmdb.WatchProviderResults, region string) *model.WatchProviders {
	if results == nil || results.Results == nil {
		return nil
	}

	regionData, ok := results.Results[region]
	if !ok {
		return nil
	}

	wp := &model.WatchProviders{
		Link: regionData.Link,
	}

	// Extract flatrate (streaming) providers
	if regionData.Flatrate != nil {
		wp.Flatrate = make([]model.WatchProvider, len(*regionData.Flatrate))
		for i, p := range *regionData.Flatrate {
			wp.Flatrate[i] = model.WatchProvider{
				ProviderID:      p.ProviderID,
				ProviderName:    p.ProviderName,
				LogoPath:        p.LogoPath,
				DisplayPriority: p.DisplayPriority,
			}
		}
	}

	// Extract rent providers
	if regionData.Rent != nil {
		wp.Rent = make([]model.WatchProvider, len(*regionData.Rent))
		for i, p := range *regionData.Rent {
			wp.Rent[i] = model.WatchProvider{
				ProviderID:      p.ProviderID,
				ProviderName:    p.ProviderName,
				LogoPath:        p.LogoPath,
				DisplayPriority: p.DisplayPriority,
			}
		}
	}

	// Extract buy providers
	if regionData.Buy != nil {
		wp.Buy = make([]model.WatchProvider, len(*regionData.Buy))
		for i, p := range *regionData.Buy {
			wp.Buy[i] = model.WatchProvider{
				ProviderID:      p.ProviderID,
				ProviderName:    p.ProviderName,
				LogoPath:        p.LogoPath,
				DisplayPriority: p.DisplayPriority,
			}
		}
	}

	// Return nil if no providers found
	if len(wp.Flatrate) == 0 && len(wp.Rent) == 0 && len(wp.Buy) == 0 {
		return nil
	}

	return wp
}

func transformMovieCredits(tmdbCredits tmdb.MovieCredits) *model.Credits {
	cast := make([]model.CastMember, 0, len(tmdbCredits.Cast))
	for _, c := range tmdbCredits.Cast {
		cast = append(cast, model.CastMember{
			TmdbID:      c.ID,
			Name:        c.Name,
			Character:   c.Character,
			ProfilePath: c.ProfilePath,
			Order:       c.Order,
		})
	}

	// crew := make([]model.CrewMember, 0, len(tmdbCredits.Crew))
	// for _, c := range tmdbCredits.Crew {
	// 	crew = append(crew, model.CrewMember{
	// 		TmdbID:      c.ID,
	// 		Name:        c.Name,
	// 		Job:         c.Job,
	// 		Department:  c.Department,
	// 		ProfilePath: c.ProfilePath,
	// 	})
	// }

	return &model.Credits{
		Cast: cast,
		// Crew: crew,
	}
}

func transformTVCredits(tmdbCredits tmdb.TVCredits) *model.Credits {
	cast := make([]model.CastMember, 0, len(tmdbCredits.Cast))
	for _, c := range tmdbCredits.Cast {
		cast = append(cast, model.CastMember{
			TmdbID:      c.ID,
			Name:        c.Name,
			Character:   c.Character,
			ProfilePath: c.ProfilePath,
			Order:       c.Order,
		})
	}

	crew := make([]model.CrewMember, 0, len(tmdbCredits.Crew))
	for _, c := range tmdbCredits.Crew {
		crew = append(crew, model.CrewMember{
			TmdbID:      c.ID,
			Name:        c.Name,
			Job:         c.Job,
			Department:  c.Department,
			ProfilePath: c.ProfilePath,
		})
	}

	return &model.Credits{
		Cast: cast,
		Crew: crew,
	}
}

func transformVideos(tmdbVideos tmdb.VideoResults) []model.Video {
	videos := make([]model.Video, 0, len(tmdbVideos.Results))
	for _, v := range tmdbVideos.Results {
		videos = append(videos, model.Video{
			TmdbID:            v.ID,
			Key:               v.Key,
			Name:              v.Name,
			Site:              v.Site,
			Type:              v.Type,
			Size:              v.Size,
			PublishedAt:       v.PublishedAt,
			IsOfficialTrailer: v.Type == "Trailer" && v.Official,
		})
	}
	return videos
}

func (s *MediaService) isInLibrary(ctx context.Context, tmdbID int64, typ model.MediaType) bool {
	_, err := s.repo.GetMediaItemByTmdbIDAndType(ctx, tmdbID, string(typ))
	return err == nil
}

func (s *MediaService) transformMovieRecommendations(ctx context.Context, tmdbRecs tmdb.MovieRecommendations) []model.MovieRail {
	// Sort by popularity (descending) and take top 10
	results := tmdbRecs.Results

	// Sort by popularity descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Popularity > results[j].Popularity
	})

	// Take top 10
	maxResults := 10
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	recommendations := make([]model.MovieRail, 0, len(results))
	for _, movie := range results {
		year := parseYear(movie.ReleaseDate)
		recommendations = append(recommendations, model.MovieRail{
			TmdbID:      movie.ID,
			Title:       movie.Title,
			Overview:    movie.Overview,
			PosterPath:  movie.PosterPath,
			ReleaseDate: movie.ReleaseDate,
			Year:        year,
			Genres:      movie.GenreIDs,
			Tagline:     "",
			IsInLibrary: s.isInLibrary(ctx, movie.ID, model.MediaTypeMovie),
		})
	}
	return recommendations
}

func (s *MediaService) GetMovie(ctx context.Context, id int64) (model.Movie, error) {
	tmdbDetails, err := s.tmdb.GetMovieDetails(ctx, id)
	if err != nil {
		return model.Movie{}, err
	}

	productionCompanies := []model.ProductionCompany{}
	for _, company := range tmdbDetails.ProductionCompanies {
		productionCompanies = append(productionCompanies, model.ProductionCompany{
			TmdbID:        company.ID,
			Name:          company.Name,
			LogoPath:      company.LogoPath,
			OriginCountry: company.OriginCountry,
		})
	}

	productionCountries := []model.ProductionCountry{}
	for _, country := range tmdbDetails.ProductionCountries {
		productionCountries = append(productionCountries, model.ProductionCountry{
			Iso3166_1: country.Iso3166_1,
			Name:      country.Name,
		})
	}

	return model.Movie{
		TmdbID:              tmdbDetails.ID,
		Title:               tmdbDetails.Title,
		Overview:            tmdbDetails.Overview,
		Tagline:             tmdbDetails.Tagline,
		Status:              tmdbDetails.Status,
		ReleaseDate:         tmdbDetails.ReleaseDate,
		Runtime:             tmdbDetails.Runtime,
		OriginalLanguage:    tmdbDetails.OriginalLanguage,
		OriginCountry:       tmdbDetails.OriginCountry,
		ProductionCompanies: productionCompanies,
		ProductionCountries: productionCountries,
		PosterPath:          tmdbDetails.PosterPath,
		BackdropPath:        tmdbDetails.BackdropPath,
	}, nil
}

func (s *MediaService) GetMovieDetail(ctx context.Context, tmdbID int64) (model.MovieDetail, error) {
	// Use extended fetch to get release dates and watch providers in one call
	tmdbDetails, err := s.tmdb.GetMovieDetailsWithExtras(ctx, tmdbID)
	if err != nil {
		return model.MovieDetail{}, err
	}

	var mediaItem model.MediaItem
	local := true
	mediaItem, err = s.repo.GetMediaItemByTmdbIDAndType(ctx, tmdbID, string(model.MediaTypeMovie))
	if err != nil {
		local = false
	}

	var files []model.FileWithState
	if local {
		files, _ = s.repo.ListFilesForItem(ctx, mediaItem.ID)
	}

	fileInfos := buildFileInfos(files)
	genres := make([]model.Genre, 0, len(tmdbDetails.Genres))
	for _, g := range tmdbDetails.Genres {
		genres = append(genres, model.Genre{TmdbID: g.ID, Name: g.Name})
	}
	year := parseYear(tmdbDetails.ReleaseDate)

	// Fetch credits and videos (gracefully handle errors)
	var credits *model.Credits
	tmdbCredits, err := s.tmdb.GetMovieCredits(ctx, tmdbID)
	if err != nil {
		s.logger.Debug().Err(err).Int64("tmdb_id", tmdbID).Msg("Failed to fetch movie credits")
	} else {
		credits = transformMovieCredits(tmdbCredits)
	}

	var videos []model.Video
	tmdbVideos, err := s.tmdb.GetMovieVideos(ctx, tmdbID)
	if err != nil {
		s.logger.Debug().Err(err).Int64("tmdb_id", tmdbID).Msg("Failed to fetch movie videos")
	} else {
		videos = transformVideos(tmdbVideos)
	}

	var recommendations []model.MovieRail
	tmdbRecs, err := s.tmdb.GetMovieRecommendations(ctx, tmdbID)
	if err != nil {
		s.logger.Debug().Err(err).Int64("tmdb_id", tmdbID).Msg("Failed to fetch movie recommendations")
	} else {
		recommendations = s.transformMovieRecommendations(ctx, tmdbRecs)
	}

	// Fetch active download jobs and add them to files
	downloadJobFiles, err := s.buildFileInfosFromDownloadJobs(ctx, tmdbID)
	if err != nil {
		s.logger.Debug().Err(err).Int64("tmdb_id", tmdbID).Msg("Failed to fetch download jobs")
	} else {
		fileInfos = append(fileInfos, downloadJobFiles...)
	}

	// Extract certification from appended release dates
	var certification string
	if tmdbDetails.MovieReleaseDatesAppend != nil && tmdbDetails.ReleaseDates != nil {
		certification = extractMovieCertification(tmdbDetails.ReleaseDates)
	}

	// Extract watch providers for user's region
	region := s.settings.GetUserRegion(ctx)
	var watchProviders *model.WatchProviders
	if tmdbDetails.MovieWatchProvidersAppend != nil {
		watchProviders = extractWatchProviders(tmdbDetails.WatchProviders, region)
	}

	return model.MovieDetail{
		TmdbID:          tmdbDetails.ID,
		Title:           tmdbDetails.Title,
		Year:            year,
		Overview:        tmdbDetails.Overview,
		Tagline:         tmdbDetails.Tagline,
		Status:          tmdbDetails.Status,
		ReleaseDate:     tmdbDetails.ReleaseDate,
		Runtime:         tmdbDetails.Runtime,
		Certification:   certification,
		VoteAverage:     float64(tmdbDetails.VoteAverage),
		VoteCount:       int64(tmdbDetails.VoteCount),
		Genres:          genres,
		PosterPath:      tmdbDetails.PosterPath,
		BackdropPath:    tmdbDetails.BackdropPath,
		Files:           fileInfos,
		Credits:         credits,
		Videos:          videos,
		Recommendations: recommendations,
		WatchProviders:  watchProviders,
	}, nil
}

func (s *MediaService) GetSeries(ctx context.Context, id int64) (model.Series, error) {
	tmdbDetails, err := s.tmdb.GetSeriesDetails(ctx, id)
	if err != nil {
		return model.Series{}, err
	}

	seasons := []model.Season{}
	for _, season := range tmdbDetails.Seasons {
		seasons = append(seasons, model.Season{
			TmdbID:       season.ID,
			SeasonNumber: int64(season.SeasonNumber),
			Title:        season.Name,
			Overview:     season.Overview,
			PosterPath:   season.PosterPath,
			AirDate:      season.AirDate,
		})
	}

	createdBy := []model.Person{}
	for _, person := range tmdbDetails.CreatedBy {
		createdBy = append(createdBy, model.Person{
			TmdbID:      person.ID,
			Name:        person.Name,
			ProfilePath: person.ProfilePath,
		})
	}

	networks := []model.Network{}
	for _, network := range tmdbDetails.Networks {
		networks = append(networks, model.Network{
			TmdbID:   network.ID,
			Name:     network.Name,
			LogoPath: network.LogoPath,
		})
	}

	genres := []model.Genre{}
	for _, genre := range tmdbDetails.Genres {
		genres = append(genres, model.Genre{
			TmdbID: genre.ID,
			Name:   genre.Name,
		})
	}

	return model.Series{
		TmdbID:       tmdbDetails.ID,
		Title:        tmdbDetails.Name,
		Overview:     tmdbDetails.Overview,
		Tagline:      tmdbDetails.Tagline,
		Status:       tmdbDetails.Status,
		Seasons:      seasons,
		PosterPath:   tmdbDetails.PosterPath,
		BackdropPath: tmdbDetails.BackdropPath,
		FirstAirDate: tmdbDetails.FirstAirDate,
		LastAirDate:  tmdbDetails.LastAirDate,
		InProduction: tmdbDetails.InProduction,
		CreatedBy:    createdBy,
		Networks:     networks,
		Genres:       genres,
	}, nil
}

func (s *MediaService) GetSeriesDetail(ctx context.Context, tmdbID int64) (model.SeriesDetail, error) {
	// Use extended fetch to get content ratings and watch providers in one call
	tmdbDetails, err := s.tmdb.GetSeriesDetailsWithExtras(ctx, tmdbID)
	if err != nil {
		return model.SeriesDetail{}, err
	}

	var mediaItem model.MediaItem
	local := true
	mediaItem, err = s.repo.GetMediaItemByTmdbIDAndType(ctx, tmdbID, string(model.MediaTypeSeries))
	if err != nil {
		local = false
	}

	var files []model.FileWithState
	if local {
		files, _ = s.repo.ListFilesForItem(ctx, mediaItem.ID)
	}

	fileInfos, availability := buildFileInfoAndAvailability(files)
	genres := make([]model.Genre, 0, len(tmdbDetails.Genres))
	for _, g := range tmdbDetails.Genres {
		genres = append(genres, model.Genre{TmdbID: g.ID, Name: g.Name})
	}

	// Fetch download jobs for series
	downloadJobFiles, err := s.buildFileInfosFromDownloadJobsForSeries(ctx, tmdbID)
	if err != nil {
		s.logger.Debug().Err(err).Int64("tmdb_id", tmdbID).Msg("Failed to fetch download jobs for series")
	}

	// Map of Season -> Episode -> FileInfo
	fileMap := make(map[int32]map[int32]model.FileInfo)
	for _, f := range fileInfos {
		if f.SeasonNumber != nil && f.EpisodeNumber != nil {
			if _, ok := fileMap[*f.SeasonNumber]; !ok {
				fileMap[*f.SeasonNumber] = make(map[int32]model.FileInfo)
			}
			fileMap[*f.SeasonNumber][*f.EpisodeNumber] = f
		}
	}
	for _, f := range downloadJobFiles {
		if f.SeasonNumber != nil && f.EpisodeNumber != nil {
			if _, ok := fileMap[*f.SeasonNumber]; !ok {
				fileMap[*f.SeasonNumber] = make(map[int32]model.FileInfo)
			}
			// Only overlay if not already present or if this is more "active" (optional logic)
			fileMap[*f.SeasonNumber][*f.EpisodeNumber] = f
		}
	}

	// Fetch full season details from TMDB to get episode metadata
	seasons := make([]model.SeasonDetail, 0, len(tmdbDetails.Seasons))
	for _, sInfo := range tmdbDetails.Seasons {
		fullSeason, err := s.tmdb.GetTVSeasonDetails(ctx, tmdbID, int(sInfo.SeasonNumber))
		if err != nil {
			s.logger.Debug().Err(err).Int64("tmdb_id", tmdbID).Int("season", int(sInfo.SeasonNumber)).Msg("Failed to fetch full season details")
			// Fallback to basic info if full fetch fails
			seasons = append(seasons, model.SeasonDetail{
				SeasonNumber: int32(sInfo.SeasonNumber),
				Overview:     sInfo.Overview,
				PosterPath:   sInfo.PosterPath,
				AirDate:      sInfo.AirDate,
			})
			continue
		}

		eps := make([]model.EpisodeAvailability, 0, len(fullSeason.Episodes))
		for _, eInfo := range fullSeason.Episodes {
			ep := model.EpisodeAvailability{
				SeasonNumber:  int32(eInfo.SeasonNumber),
				EpisodeNumber: int32(eInfo.EpisodeNumber),
				Title:         &eInfo.Name,
				Overview:      eInfo.Overview,
				StillPath:     eInfo.StillPath,
				AirDate:       &eInfo.AirDate,
			}

			if f, ok := fileMap[ep.SeasonNumber][ep.EpisodeNumber]; ok {
				ep.Available = true
				ep.File = &f
			}

			eps = append(eps, ep)
		}

		seasons = append(seasons, model.SeasonDetail{
			SeasonNumber: int32(sInfo.SeasonNumber),
			Overview:     fullSeason.Overview,
			PosterPath:   fullSeason.PosterPath,
			AirDate:      fullSeason.AirDate,
			Episodes:     eps,
		})
	}

	year := parseYear(tmdbDetails.FirstAirDate)

	// Fetch credits and videos (gracefully handle errors)
	var credits *model.Credits
	tmdbCredits, err := s.tmdb.GetTVCredits(ctx, tmdbID)
	if err != nil {
		s.logger.Debug().Err(err).Int64("tmdb_id", tmdbID).Msg("Failed to fetch series credits")
	} else {
		credits = transformTVCredits(tmdbCredits)
	}

	var videos []model.Video
	tmdbVideos, err := s.tmdb.GetTVVideos(ctx, tmdbID)
	if err != nil {
		s.logger.Debug().Err(err).Int64("tmdb_id", tmdbID).Msg("Failed to fetch series videos")
	} else {
		videos = transformVideos(tmdbVideos)
	}

	// Extract certification from appended content ratings
	var certification string
	if tmdbDetails.TVContentRatingsAppend != nil && tmdbDetails.ContentRatings != nil {
		certification = extractTVCertification(tmdbDetails.ContentRatings)
	}

	// Extract episode runtime
	episodeRuntime := extractEpisodeRuntime(tmdbDetails.EpisodeRunTime)

	// Extract watch providers for user's region
	region := s.settings.GetUserRegion(ctx)
	var watchProviders *model.WatchProviders
	if tmdbDetails.TVWatchProvidersAppend != nil {
		watchProviders = extractWatchProviders(tmdbDetails.WatchProviders, region)
	}

	// Map next episode to air (non-zero ID means data is present)
	var nextEpisode *model.NextEpisode
	if tmdbDetails.NextEpisodeToAir.ID != 0 {
		nextEpisode = &model.NextEpisode{
			SeasonNumber:  tmdbDetails.NextEpisodeToAir.SeasonNumber,
			EpisodeNumber: tmdbDetails.NextEpisodeToAir.EpisodeNumber,
			Title:         tmdbDetails.NextEpisodeToAir.Name,
			AirDate:       tmdbDetails.NextEpisodeToAir.AirDate,
			Overview:      tmdbDetails.NextEpisodeToAir.Overview,
			StillPath:     tmdbDetails.NextEpisodeToAir.StillPath,
		}
	}

	return model.SeriesDetail{
		TmdbID:           tmdbDetails.ID,
		Title:            tmdbDetails.Name,
		Year:             year,
		Overview:         tmdbDetails.Overview,
		Tagline:          tmdbDetails.Tagline,
		Status:           tmdbDetails.Status,
		FirstAirDate:     tmdbDetails.FirstAirDate,
		LastAirDate:      tmdbDetails.LastAirDate,
		InProduction:     tmdbDetails.InProduction,
		Certification:    certification,
		EpisodeRuntime:   episodeRuntime,
		VoteAverage:      float64(tmdbDetails.VoteAverage),
		VoteCount:        int64(tmdbDetails.VoteCount),
		Genres:           genres,
		PosterPath:       tmdbDetails.PosterPath,
		BackdropPath:     tmdbDetails.BackdropPath,
		NextEpisodeToAir: nextEpisode,
		Availability:     availability,
		Seasons:          seasons,
		Credits:          credits,
		Videos:           videos,
		WatchProviders:   watchProviders,
	}, nil
}

func (s *MediaService) buildFileInfosFromDownloadJobsForSeries(ctx context.Context, tmdbID int64) ([]model.FileInfo, error) {
	downloadJobs, err := s.repo.ListDownloadJobsByTmdbSeriesID(ctx, tmdbID)
	if err != nil {
		return nil, err
	}

	// Only include active downloads (import is now a separate subsystem)
	activeStatuses := map[string]bool{
		"created":     true,
		"enqueued":    true,
		"downloading": true,
	}

	fileInfos := make([]model.FileInfo, 0)
	for _, job := range downloadJobs {
		if !activeStatuses[job.Status] {
			continue
		}

		jobID := job.ID.String()
		libID := job.LibraryID.String()

		// Use candidate title as path placeholder since actual path is determined at import time
		path := job.CandidateTitle

		fileInfos = append(fileInfos, model.FileInfo{
			ID:            "", // No file row exists yet
			LibraryID:     libID,
			Path:          path,
			Status:        "downloading",
			SeasonNumber:  job.SeasonNumber,
			EpisodeNumber: job.EpisodeNumber,
			DownloadJobID: &jobID,
			Progress:      job.Progress,
		})
	}

	return fileInfos, nil
}

func (s *MediaService) GetPersonDetail(ctx context.Context, tmdbID int64) (model.PersonDetail, error) {
	tmdbDetails, err := s.tmdb.GetPersonDetails(ctx, tmdbID)
	if err != nil {
		return model.PersonDetail{}, err
	}

	// Helper function to convert empty string to nil pointer
	stringPtr := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}

	return model.PersonDetail{
		TmdbID:             tmdbDetails.ID,
		Name:               tmdbDetails.Name,
		Biography:          tmdbDetails.Biography,
		Birthday:           stringPtr(tmdbDetails.Birthday),
		Deathday:           stringPtr(tmdbDetails.Deathday),
		PlaceOfBirth:       stringPtr(tmdbDetails.PlaceOfBirth),
		KnownForDepartment: tmdbDetails.KnownForDepartment,
		ProfilePath:        tmdbDetails.ProfilePath,
		Popularity:         tmdbDetails.Popularity,
		AlsoKnownAs:        tmdbDetails.AlsoKnownAs,
		Homepage:           stringPtr(tmdbDetails.Homepage),
		IMDbID:             stringPtr(tmdbDetails.IMDbID),
	}, nil
}

func buildFileInfos(files []model.FileWithState) []model.FileInfo {
	fileInfos := make([]model.FileInfo, 0, len(files))

	for _, f := range files {
		libID := f.LibraryID.String()
		var seasonNum *int32
		if f.SeasonNumber != nil {
			seasonNum = f.SeasonNumber
		}
		var episodeNum *int32
		if f.EpisodeNumber != nil {
			episodeNum = f.EpisodeNumber
		}
		// Derive status from file_exists field
		status := "available"
		if f.Exists != nil && !*f.Exists {
			status = "missing"
		}
		fileInfos = append(fileInfos, model.FileInfo{
			ID:            f.ID.String(),
			LibraryID:     libID,
			Path:          f.Path,
			Status:        status,
			SeasonNumber:  seasonNum,
			EpisodeNumber: episodeNum,
		})
	}

	return fileInfos
}

func buildFileInfoAndAvailability(files []model.FileWithState) ([]model.FileInfo, model.Availability) {
	fileInfos := make([]model.FileInfo, 0, len(files))
	libAgg := map[string]struct {
		count  int
		status []string
	}{}

	for _, f := range files {
		libID := f.LibraryID.String()
		var seasonNum *int32
		if f.SeasonNumber != nil {
			seasonNum = f.SeasonNumber
		}
		var episodeNum *int32
		if f.EpisodeNumber != nil {
			episodeNum = f.EpisodeNumber
		}
		// Derive status from file_exists field
		status := "available"
		if f.Exists != nil && !*f.Exists {
			status = "missing"
		}
		fileInfos = append(fileInfos, model.FileInfo{
			ID:            f.ID.String(),
			LibraryID:     libID,
			Path:          f.Path,
			Status:        status,
			SeasonNumber:  seasonNum,
			EpisodeNumber: episodeNum,
		})
		entry := libAgg[libID]
		entry.count++
		entry.status = append(entry.status, status)
		libAgg[libID] = entry
	}

	libraries := make([]model.LibraryAvailability, 0, len(libAgg))
	for libID, agg := range libAgg {
		libraries = append(libraries, model.LibraryAvailability{
			LibraryID:    libID,
			FileCount:    agg.count,
			StatusRollup: bestStatus(agg.status),
		})
	}

	return fileInfos, model.Availability{
		IsInLibrary: len(files) > 0,
		Libraries:   libraries,
	}
}

func bestStatus(statuses []string) string {
	if len(statuses) == 0 {
		return "missing"
	}
	priority := map[string]int{
		"available":   5,
		"downloading": 4,
		"importing":   3,
		"missing":     2,
		"failed":      1,
		"deleted":     0,
	}
	best := "missing"
	bestScore := -1
	for _, s := range statuses {
		if score, ok := priority[s]; ok {
			if score > bestScore {
				best = s
				bestScore = score
			}
		}
	}
	return best
}

func parseYear(dateStr string) *int32 {
	if len(dateStr) < 4 {
		return nil
	}
	y, err := strconv.Atoi(dateStr[:4])
	if err != nil {
		return nil
	}
	val := int32(y)
	return &val
}

func (s *MediaService) buildFileInfosFromDownloadJobs(ctx context.Context, tmdbID int64) ([]model.FileInfo, error) {
	downloadJobs, err := s.repo.ListDownloadJobsByTmdbMovieID(ctx, tmdbID)
	if err != nil {
		return nil, err
	}

	// Only include active downloads (import is now a separate subsystem)
	activeStatuses := map[string]bool{
		"created":     true,
		"enqueued":    true,
		"downloading": true,
	}

	fileInfos := make([]model.FileInfo, 0)
	for _, job := range downloadJobs {
		if !activeStatuses[job.Status] {
			continue
		}

		jobID := job.ID.String()
		libID := job.LibraryID.String()

		// Use candidate title as path placeholder since actual path is determined at import time
		path := job.CandidateTitle

		fileInfos = append(fileInfos, model.FileInfo{
			ID:            "", // No file row exists yet
			LibraryID:     libID,
			Path:          path,
			Status:        "downloading",
			DownloadJobID: &jobID,
			Progress:      job.Progress,
		})
	}

	return fileInfos, nil
}

// Search performs a multi-search across movies, series, and people via TMDB
// and enriches results with library status
func (s *MediaService) Search(ctx context.Context, query string, limit int, page int) (model.SearchResponse, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if page <= 0 {
		page = 1
	}

	tmdbResults, err := s.tmdb.MultiSearch(ctx, query, page)
	if err != nil {
		return model.SearchResponse{}, err
	}

	// Collect TMDB IDs for batch library lookup
	var movieIDs, seriesIDs []int64
	for _, r := range tmdbResults.Results {
		switch r.MediaType {
		case "movie":
			movieIDs = append(movieIDs, r.ID)
		case "tv":
			seriesIDs = append(seriesIDs, r.ID)
		}
	}

	// Batch lookup library status
	movieInLibrary := make(map[int64]bool)
	seriesInLibrary := make(map[int64]bool)

	if len(movieIDs) > 0 {
		inLib, err := s.repo.CheckMediaItemsInLibrary(ctx, movieIDs, "movie")
		if err == nil {
			movieInLibrary = inLib
		}
	}
	if len(seriesIDs) > 0 {
		inLib, err := s.repo.CheckMediaItemsInLibrary(ctx, seriesIDs, "series")
		if err == nil {
			seriesInLibrary = inLib
		}
	}

	// Transform results
	results := make([]model.SearchResult, 0, len(tmdbResults.Results))
	for _, r := range tmdbResults.Results {
		if len(results) >= limit {
			break
		}

		result := model.SearchResult{
			ID:        r.ID,
			MediaType: r.MediaType,
		}

		// Set title based on media type
		switch r.MediaType {
		case "movie":
			result.Title = r.Title
			if r.PosterPath != "" {
				result.PosterPath = &r.PosterPath
			}
			if r.Overview != "" {
				result.Overview = &r.Overview
			}
			year := parseYear(r.ReleaseDate)
			if year != nil {
				y := int(*year)
				result.Year = &y
			}
			result.IsInLibrary = movieInLibrary[r.ID]

		case "tv":
			result.Title = r.Name
			if r.PosterPath != "" {
				result.PosterPath = &r.PosterPath
			}
			if r.Overview != "" {
				result.Overview = &r.Overview
			}
			year := parseYear(r.FirstAirDate)
			if year != nil {
				y := int(*year)
				result.Year = &y
			}
			result.IsInLibrary = seriesInLibrary[r.ID]

		case "person":
			result.Title = r.Name
			if r.ProfilePath != "" {
				result.PosterPath = &r.ProfilePath
			}
			// People don't have overview or library status
		}

		results = append(results, result)
	}

	return model.SearchResponse{
		Results:      results,
		TotalResults: int(tmdbResults.TotalResults),
		Query:        query,
	}, nil
}
