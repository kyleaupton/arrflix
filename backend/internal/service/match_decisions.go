package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// MatchDecisionsService owns the Phase-4 decision flows: re-match,
// un-match, detach, match-by-ID, and the evidence-read endpoint.
//
// Auto-match (the scanner's confident-band path) goes through
// MatcherService.MatchBatch directly; this service is the user-driven
// half of the match_decision write surface. Both halves write through
// the same repo (via the insertMatchOutcome helper, and commitMatch for
// the success-path persistence side-effect) so the supersede chain
// remains consistent.
//
// See specs/modules/matching/README.md § "Re-match, un-match, detach"
// for the action / effect contract.
type MatchDecisionsService struct {
	repo       *repo.Repository
	log        *logger.Logger
	tmdb       *TmdbService
	enrichment *EnrichmentService
	provider   metadata.MetadataProvider
	settings   *SettingsService
	// backgroundCtx is used for fire-and-forget side effects (enrichment
	// kick after manual match). Mirrors ScannerService.ctx.
	backgroundCtx context.Context
}

// NewMatchDecisionsService wires the collaborators. The metadata
// provider is the same one MatcherService uses — the identity-validation
// path for match-by-ID flows through it (LookupByID + cross-provider
// resolution), reusing the aggregator's contract rather than
// duplicating the validation logic.
func NewMatchDecisionsService(
	r *repo.Repository,
	l *logger.Logger,
	tmdb *TmdbService,
	enrichment *EnrichmentService,
	provider metadata.MetadataProvider,
	settings *SettingsService,
) *MatchDecisionsService {
	return &MatchDecisionsService{
		repo:          r,
		log:           l,
		tmdb:          tmdb,
		enrichment:    enrichment,
		provider:      provider,
		settings:      settings,
		backgroundCtx: context.Background(),
	}
}

// SetBackgroundContext lets the service.New caller wire a cancellable
// context for fire-and-forget goroutines. Mirrors ScannerService.SetContext.
func (s *MatchDecisionsService) SetBackgroundContext(ctx context.Context) {
	s.backgroundCtx = ctx
}

// ----- Match (re-match + match-by-ID) -----

// FileMatchRequest carries the user-supplied identity for the unified
// match endpoint. The endpoint serves both "match a file from the
// inbox" (file currently in unmatched_file) and "re-match an existing
// match" (file currently in media_file) — the handler reads the file's
// current row and transitions accordingly.
type FileMatchRequest struct {
	Source     string // "tmdb" | "imdb" | "tvdb"
	ExternalID string
	Season     *int
	Episode    *int
	Edition    *string
}

// MatchByID validates the request's identity against the metadata
// provider, writes a match_decision row at confidence 1.0, supersedes
// the prior decision, and applies the side-effect (delete unmatched_file
// + commitMatch for first-match; update identity for re-match).
//
// The unified endpoint serves two spec actions:
//   - match-by-ID (file currently in unmatched_file) — first-match,
//     deletes the unmatched row and creates a media_file via
//     commitMatch.
//   - re-match (file currently in media_file) — re-points the existing
//     media_file at the new identity in place (UpdateMediaFileIdentity),
//     preserving its state snapshot and import history.
//
// On a 404 from the provider the function returns NotFound without
// writing a decision row — the user gave a bad ID; we don't waste an
// audit slot on it.
func (s *MatchDecisionsService) MatchByID(ctx context.Context, fileID uuid.UUID, userID uuid.UUID, req FileMatchRequest) (model.MatchDecision, error) {
	source := metadata.ExternalSource(req.Source)
	switch source {
	case metadata.SourceTMDB, metadata.SourceIMDB, metadata.SourceTVDB:
		// ok
	default:
		return model.MatchDecision{}, apperrors.Validation("invalid match identity",
			apperrors.Field("body.external.source", "must be 'tmdb', 'imdb', or 'tvdb'"),
		).Op("MatchDecisionsService.MatchByID")
	}
	if req.ExternalID == "" {
		return model.MatchDecision{}, apperrors.Validation("invalid match identity",
			apperrors.Field("body.external.externalId", "required"),
		).Op("MatchDecisionsService.MatchByID")
	}

	// Validate the identity against the metadata provider. LookupByID
	// returns (nil, nil) for 404, a redirect-flagged item for merged
	// records, and the canonical item otherwise. Cross-provider
	// (imdb/tvdb → tmdb) rewrite happens inside the TMDB provider's
	// LookupByID — same code path the aggregator uses.
	if s.provider == nil {
		return model.MatchDecision{}, apperrors.BadGatewayf("metadata provider unavailable").
			Op("MatchDecisionsService.MatchByID")
	}
	item, err := s.provider.LookupByID(ctx, source, req.ExternalID)
	if err != nil {
		return model.MatchDecision{}, apperrors.BadGatewayf("metadata lookup failed for %s:%s: %v", source, req.ExternalID, err).
			Op("MatchDecisionsService.MatchByID")
	}
	if item == nil {
		return model.MatchDecision{}, apperrors.NotFoundf("metadata identity %s:%s not found", source, req.ExternalID).
			Op("MatchDecisionsService.MatchByID")
	}

	// At this point item.Source is always TMDB (the provider's contract
	// guarantees post-resolution canonicalization). Defend the
	// invariant — commitMatch enforces it too.
	if item.Source != metadata.SourceTMDB {
		return model.MatchDecision{}, apperrors.Internalf("metadata provider returned non-tmdb source %s for %s:%s", item.Source, source, req.ExternalID).
			Op("MatchDecisionsService.MatchByID").
			NotRetryable()
	}
	tmdbID, perr := strconv.ParseInt(item.ExternalID, 10, 64)
	if perr != nil {
		return model.MatchDecision{}, apperrors.Internalf("metadata provider returned non-numeric tmdb id %q: %v", item.ExternalID, perr).
			Op("MatchDecisionsService.MatchByID").
			NotRetryable()
	}

	// Look up the file's current location — media_file (re-match) or
	// unmatched_file (first match-by-ID). One must exist; otherwise
	// the path id is bogus.
	current, library, relPath, fileSize, location, err := s.lookupFile(ctx, fileID)
	if err != nil {
		return model.MatchDecision{}, err
	}

	episodeRef := episodeRefFromReq(req.Season, req.Episode)
	editionPtr := editionPtrFromReq(req.Edition)

	// Build the outcome record. Manual matches are by definition
	// confident — they bypass resolvers per spec § Killer UX moves.
	rec := matcher.MatchOutcomeRecord{
		FileID:     fileID,
		Outcome:    matcher.OutcomeConfident,
		ChosenRef:  &matcher.ExternalRef{Source: item.Source, ExternalID: item.ExternalID},
		ChosenItem: item,
		// Cross-provider rewrite: if the user supplied IMDb/TVDB, the
		// canonical TMDB ref is in item.ExternalID. Both ends are recorded
		// in evidence below.
		ChosenEpisode:      episodeRef,
		ChosenEdition:      editionPtr,
		Confidence:         1.0,
		ResolversConsulted: []matcher.ResolverAudit{},
		DecidedBy:          "user:" + userID.String(),
	}
	rec.Evidence = manualMatchEvidence(source, req.ExternalID, item)
	_ = current // current decision read for future "preserved chain" affordances

	// Persist match_decision + side-effects.
	mediaFile, decision, err := s.commitManualMatch(ctx, library, fileID, relPath, fileSize, location, tmdbID, item, episodeRef, editionPtr, rec)
	if err != nil {
		return model.MatchDecision{}, err
	}
	_ = mediaFile // exposed via the decision; the wire shape doesn't need both

	return decision, nil
}

// commitManualMatch is the per-transition arm of MatchByID. It writes
// the match_decision row, supersedes the prior, then applies the
// media-table side-effect via commitMatch: create the media_file (+
// drop the unmatched row) for first-match, or re-point the existing
// media_file in place for re-match. The in-place re-match path means a
// commit failure leaves the original media_file untouched rather than
// orphaning the file.
//
// commitMatch's transaction and the match_decision write are
// intentionally NOT collapsed into one tx — the read path
// (GetCurrentMatchDecisionForFile) only cares about the latest
// non-superseded row regardless of side-effect ordering, and the
// decision row is the audit anchor that should stand even if the
// media-table write fails. The window where they can drift is small and
// idempotent on retry.
func (s *MatchDecisionsService) commitManualMatch(
	ctx context.Context,
	library model.Library,
	fileID uuid.UUID,
	relPath string,
	fileSize *int64,
	location fileLocation,
	tmdbID int64,
	item *metadata.Item,
	episodeRef *matcher.EpisodeRef,
	editionPtr *string,
	rec matcher.MatchOutcomeRecord,
) (model.MediaFile, model.MatchDecision, error) {
	// Phase 1: write match_decision + supersede prior. This is the
	// audit anchor — even if the side-effect below fails, the decision
	// row stands.
	decisionID, err := insertMatchOutcome(ctx, s.repo, rec)
	if err != nil {
		return model.MediaFile{}, model.MatchDecision{}, err
	}
	if err := s.repo.SupersedeMatchDecision(ctx, fileID, decisionID); err != nil {
		return model.MediaFile{}, model.MatchDecision{}, err
	}

	// Phase 2: side-effect.
	absPath := joinLibraryPath(library, relPath)

	preservedID := fileID
	in := commitMatchInput{
		Library:         library,
		RelPath:         relPath,
		AbsPath:         absPath,
		FileSize:        fileSize,
		TmdbID:          tmdbID,
		Item:            item,
		Edition:         editionPtr,
		Method:          "manual_match",
		PreservedFileID: &preservedID,
	}
	if episodeRef != nil {
		sn := int32(episodeRef.Season)
		en := int32(episodeRef.Episode)
		in.Season = &sn
		in.Episode = &en
	}

	switch location {
	case fileLocationUnmatched:
		// First-match from the inbox: create the media_file (carrying the
		// preserved file_id) via commitMatch, then drop the unmatched row.
	case fileLocationMedia:
		// Re-match: re-point the existing media_file in place rather than
		// delete-and-recreate. UpdateMediaFileIdentity (Recommit) keeps
		// the row, its media_file_state snapshot, and its
		// media_file_import history intact — and, crucially, never leaves
		// the file untracked if the commit fails partway. media_file.id
		// (== file_id) is unchanged, so the match_decision chain keyed on
		// it stays consistent.
		in.Recommit = true
	default:
		return model.MediaFile{}, model.MatchDecision{}, apperrors.NotFoundf("file %s not found in media_file or unmatched_file", fileID).
			Op("MatchDecisionsService.commitManualMatch")
	}

	result, cerr := commitMatch(ctx, commitMatchDeps{repo: s.repo, log: s.log, tmdb: s.tmdb}, in)
	if cerr != nil {
		return model.MediaFile{}, model.MatchDecision{}, cerr
	}
	if result.EpisodeFailed {
		return model.MediaFile{}, model.MatchDecision{}, apperrors.BadGatewayf("tmdb episode-details lookup failed for %s:%d s%de%d", item.Source, tmdbID, *in.Season, *in.Episode).
			Op("MatchDecisionsService.commitManualMatch")
	}
	mediaFile := result.MediaFile

	// First-match only: the unmatched_file row's job is done now that the
	// media_file + match_decision pair is the canonical record. Best-effort
	// — a stale unmatched row is benign and a retry re-runs idempotently.
	if location == fileLocationUnmatched {
		if derr := s.repo.DeleteUnmatchedFile(ctx, fileID); derr != nil {
			s.log.Warn().Err(derr).Str("fileId", fileID.String()).Msg("MatchByID: failed to delete unmatched_file row after commit")
		}
	}

	// Fire enrichment if commitMatch created a new media_item.
	if result.ItemCreated && s.enrichment != nil {
		created := result.MediaItem
		go func() {
			if err := s.enrichment.EnrichMediaItem(s.backgroundCtx, created); err != nil {
				s.log.Warn().Err(err).Str("title", created.Title).Msg("manual-match enrichment failed, worker will retry")
			}
		}()
	}

	// TODO(import): re-match writes the new identity to media_file but the
	// file on disk stays at the old path until the import service grows a
	// re-import entry point to rename per the new identity's name template.

	decision, derr := s.repo.GetCurrentMatchDecisionForFile(ctx, fileID)
	if derr != nil {
		return mediaFile, model.MatchDecision{}, derr
	}
	return mediaFile, decision, nil
}

// ----- Unmatch -----

// Unmatch transitions a media_file row back to unmatched_file with no
// suggestions ("I don't know what this is"). Writes a no_match
// match_decision row and supersedes the prior.
//
// Idempotent: if the file is already in unmatched_file, returns the
// current decision without writing a new row. The file stays on disk
// regardless — un-match has no filesystem effect per spec § Re-match,
// un-match, detach.
func (s *MatchDecisionsService) Unmatch(ctx context.Context, fileID uuid.UUID, userID uuid.UUID) (model.MatchDecision, error) {
	_, library, relPath, fileSize, location, err := s.lookupFile(ctx, fileID)
	if err != nil {
		return model.MatchDecision{}, err
	}

	if location == fileLocationUnmatched {
		// Already unmatched — surface the current decision unchanged.
		return s.repo.GetCurrentMatchDecisionForFile(ctx, fileID)
	}

	rec := matcher.MatchOutcomeRecord{
		FileID:             fileID,
		Outcome:            matcher.OutcomeNoMatch,
		Confidence:         0.0,
		ResolversConsulted: []matcher.ResolverAudit{},
		Evidence:           json.RawMessage(`{}`),
		DecidedBy:          "user:" + userID.String(),
	}

	decisionID, err := insertMatchOutcome(ctx, s.repo, rec)
	if err != nil {
		return model.MatchDecision{}, err
	}
	if err := s.repo.SupersedeMatchDecision(ctx, fileID, decisionID); err != nil {
		return model.MatchDecision{}, err
	}

	// Delete media_file (media_file_state cascades; media_file_import
	// sets media_file_id NULL but preserves the history row), then
	// create unmatched_file at the same path with empty suggestions.
	// The reuse of fileID as the unmatched_file row id is NOT possible
	// — unmatched_file.id has its own DEFAULT uuid_generate_v4(). We
	// accept that the file's stable id changes on un-match; the
	// match_decision row still ties the audit chain together via
	// file_id. A future seam would let unmatched_file accept a provided
	// id to preserve the stable join key.
	if err := s.repo.DeleteMediaFile(ctx, fileID); err != nil {
		return model.MatchDecision{}, err
	}
	if _, err := s.repo.UpsertUnmatchedFile(ctx, repo.UpsertUnmatchedFileParams{
		LibraryID:        library.ID,
		Path:             relPath,
		FileSize:         fileSize,
		SuggestedMatches: nil, // user said "I don't know" → no suggestions
		PartialSeries:    false,
	}); err != nil {
		return model.MatchDecision{}, err
	}

	return s.repo.GetCurrentMatchDecisionForFile(ctx, fileID)
}

// ----- Detach -----

// DetachRequest configures whether the file is quarantined on disk. The
// quarantine path is read from the settings table (if configured); when
// absent or the flag is false, the file stays in place and detach is a
// pure audit operation.
type DetachRequest struct {
	Quarantine bool
}

// DetachResult is returned by Detach: the new (detached) decision plus
// the destination path when quarantine moved the file (nil otherwise).
type DetachResult struct {
	Decision        model.MatchDecision
	QuarantinedPath *string
}

// Detach removes the file from the library's scope entirely. Writes a
// detached match_decision row (the new 7th outcome added in migration
// 0016) and supersedes the prior. The file is then deleted from the
// tracking tables (media_file OR unmatched_file). If Quarantine is true
// and a quarantine path is configured, the file is also moved on disk.
func (s *MatchDecisionsService) Detach(ctx context.Context, fileID uuid.UUID, userID uuid.UUID, req DetachRequest) (DetachResult, error) {
	_, library, relPath, _, location, err := s.lookupFile(ctx, fileID)
	if err != nil {
		return DetachResult{}, err
	}

	rec := matcher.MatchOutcomeRecord{
		FileID:             fileID,
		Outcome:            matcher.OutcomeDetached,
		Confidence:         0.0,
		ResolversConsulted: []matcher.ResolverAudit{},
		Evidence:           json.RawMessage(`{}`),
		DecidedBy:          "user:" + userID.String(),
	}

	decisionID, err := insertMatchOutcome(ctx, s.repo, rec)
	if err != nil {
		return DetachResult{}, err
	}
	if err := s.repo.SupersedeMatchDecision(ctx, fileID, decisionID); err != nil {
		return DetachResult{}, err
	}

	// Drop the file from whichever table holds it. media_file_state
	// cascades; media_file_import rows have their media_file_id nulled
	// (so the audit row stays around).
	switch location {
	case fileLocationMedia:
		if err := s.repo.DeleteMediaFile(ctx, fileID); err != nil {
			return DetachResult{}, err
		}
	case fileLocationUnmatched:
		if err := s.repo.DeleteUnmatchedFile(ctx, fileID); err != nil {
			return DetachResult{}, err
		}
	}

	var quarantinedPath *string
	if req.Quarantine {
		absSrc := joinLibraryPath(library, relPath)
		// TODO(quarantine-settings): the quarantine root isn't a
		// committed setting yet. Phase-4 leaves the seam — when a
		// settings key like "matcher.quarantine_path" lands, read it
		// here. For now, attempt to read the key and skip silently if
		// unset.
		var qroot string
		if s.settings != nil {
			if raw, gerr := s.settings.GetRaw(ctx, "matcher.quarantine_path"); gerr == nil {
				if str, ok := raw.(string); ok {
					qroot = str
				}
			}
		}
		if qroot != "" {
			dst := filepath.Join(qroot, filepath.Base(relPath))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				s.log.Warn().Err(err).Str("dst", dst).Msg("detach: failed to create quarantine dir; skipping move")
			} else if err := os.Rename(absSrc, dst); err != nil {
				s.log.Warn().Err(err).Str("src", absSrc).Str("dst", dst).Msg("detach: failed to quarantine file")
			} else {
				quarantinedPath = &dst
			}
		}
	}

	decision, derr := s.repo.GetCurrentMatchDecisionForFile(ctx, fileID)
	if derr != nil {
		return DetachResult{}, derr
	}
	return DetachResult{Decision: decision, QuarantinedPath: quarantinedPath}, nil
}

// ----- Evidence read -----

// MatchDecisionView is the read-side composite: current decision plus
// the validated metadata.Item for display, plus the optional history
// (the supersede chain) when the caller asks for it.
type MatchDecisionView struct {
	Current model.MatchDecision
	// ChosenItem is the validated metadata.Item for the current
	// decision's chosen_external_id, when available. Nil when the
	// outcome carries no winning candidate (no_match, ambiguous,
	// detached) or when LookupByID failed.
	ChosenItem *metadata.Item
	// History carries every prior decision newest-first, when the
	// caller passed includeHistory=true. Empty otherwise. History
	// entries don't carry ChosenItem — they're audit, not display.
	History []model.MatchDecision
}

// GetForFile returns the file's current match decision plus optional
// supersede-chain history. Powers the spec's drill-down / "why didn't
// this match" surface.
func (s *MatchDecisionsService) GetForFile(ctx context.Context, fileID uuid.UUID, includeHistory bool) (MatchDecisionView, error) {
	current, err := s.repo.GetCurrentMatchDecisionForFile(ctx, fileID)
	if err != nil {
		return MatchDecisionView{}, err
	}

	view := MatchDecisionView{Current: current}

	// Attach the validated item for display — only when the row carries
	// a chosen identity AND we have a provider to ask. Lookup errors
	// degrade gracefully: the audit data still surfaces.
	if current.ChosenSource != nil && current.ChosenExternalID != nil && s.provider != nil {
		item, lerr := s.provider.LookupByID(ctx, metadata.ExternalSource(*current.ChosenSource), *current.ChosenExternalID)
		if lerr == nil {
			view.ChosenItem = item
		}
	}

	if includeHistory {
		history, herr := s.repo.ListMatchDecisionHistoryForFile(ctx, fileID)
		if herr != nil {
			return MatchDecisionView{}, herr
		}
		// Filter the current row out of the history slice — the spec's
		// shape is Current + (superseded chain), not Current + (Current
		// + chain).
		view.History = make([]model.MatchDecision, 0, len(history))
		for _, h := range history {
			if h.ID != current.ID {
				view.History = append(view.History, h)
			}
		}
	}

	return view, nil
}

// ----- File-location lookup -----

// fileLocation names where a fileID currently lives: media_file,
// unmatched_file, or neither. Each handler reads it to decide which
// transition to apply.
type fileLocation int

const (
	fileLocationNone fileLocation = iota
	fileLocationMedia
	fileLocationUnmatched
)

// lookupFile probes media_file then unmatched_file, returning the
// resolved row's library + path + size. The current decision (when
// any) is also returned so callers can inspect prior identity without
// a second query. Returns NotFound when the file_id matches neither
// table.
func (s *MatchDecisionsService) lookupFile(ctx context.Context, fileID uuid.UUID) (*model.MatchDecision, model.Library, string, *int64, fileLocation, error) {
	if mf, err := s.repo.GetMediaFile(ctx, fileID); err == nil {
		library, lerr := s.repo.GetLibrary(ctx, mf.LibraryID)
		if lerr != nil {
			return nil, model.Library{}, "", nil, fileLocationNone, lerr
		}
		state, _ := s.repo.GetMediaFileState(ctx, mf.ID)
		current, cerr := s.repo.GetCurrentMatchDecisionForFile(ctx, fileID)
		var currentPtr *model.MatchDecision
		if cerr == nil {
			tmp := current
			currentPtr = &tmp
		}
		var sizePtr *int64
		if state.FileSize != nil {
			sizePtr = state.FileSize
		}
		return currentPtr, library, mf.Path, sizePtr, fileLocationMedia, nil
	} else if !apperrors.IsNotFound(err) {
		return nil, model.Library{}, "", nil, fileLocationNone, err
	}

	uf, err := s.repo.GetUnmatchedFile(ctx, fileID)
	if err == nil {
		library, lerr := s.repo.GetLibrary(ctx, uf.LibraryID)
		if lerr != nil {
			return nil, model.Library{}, "", nil, fileLocationNone, lerr
		}
		current, cerr := s.repo.GetCurrentMatchDecisionForFile(ctx, fileID)
		var currentPtr *model.MatchDecision
		if cerr == nil {
			tmp := current
			currentPtr = &tmp
		}
		return currentPtr, library, uf.Path, uf.FileSize, fileLocationUnmatched, nil
	}
	if !apperrors.IsNotFound(err) {
		return nil, model.Library{}, "", nil, fileLocationNone, err
	}

	return nil, model.Library{}, "", nil, fileLocationNone, apperrors.NotFoundf("file %s not found in media_file or unmatched_file", fileID).
		Op("MatchDecisionsService.lookupFile")
}

// ----- helpers -----

// episodeRefFromReq converts the request's nullable season/episode
// into a matcher.EpisodeRef pointer. Returns nil when either is absent
// — partial episode info isn't carried in v1.
func episodeRefFromReq(season, episode *int) *matcher.EpisodeRef {
	if season == nil || episode == nil {
		return nil
	}
	return &matcher.EpisodeRef{Season: *season, Episode: *episode}
}

// editionPtrFromReq normalizes the request's edition value to a
// non-empty pointer.
func editionPtrFromReq(edition *string) *string {
	if edition == nil || *edition == "" {
		return nil
	}
	v := *edition
	return &v
}

// manualMatchEvidence builds the evidence JSONB payload for a
// match_decision row written by the manual handler. It records what
// the user supplied AND what the provider resolved it to — so the
// audit trail captures cross-provider rewrites the same way the
// aggregator's validation_cross_provider slot does.
func manualMatchEvidence(reqSource metadata.ExternalSource, reqExternalID string, item *metadata.Item) json.RawMessage {
	type submitted struct {
		Source     string `json:"source"`
		ExternalID string `json:"externalId"`
	}
	type resolved struct {
		Source     string `json:"source"`
		ExternalID string `json:"externalId"`
		Title      string `json:"title,omitempty"`
		Year       int    `json:"year,omitempty"`
		Redirected bool   `json:"redirected,omitempty"`
	}
	payload := map[string]any{
		"submitted": submitted{Source: string(reqSource), ExternalID: reqExternalID},
		"resolved": resolved{
			Source:     string(item.Source),
			ExternalID: item.ExternalID,
			Title:      item.Title,
			Year:       item.Year,
			Redirected: item.Redirected,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
