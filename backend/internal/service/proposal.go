package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/realtime"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/sse"
)

// ProposalService owns the propose rung of acquisition autonomy: it parks a
// propose-segment want's pick as a proposal (held for one-tap approval) and
// resolves it — approve replays the stored grab, decline excludes the release and
// re-arms for a different one, and a strictly-better later pick supersedes the
// open proposal in place. The AcquisitionService calls ProposeOrSupersede on the
// propose branch; the HTTP handlers drive Approve/Decline/ListForTracking.
type ProposalService struct {
	repo    *repo.Repository
	quality *QualityProfileService
	broker  *sse.Broker
	log     *logger.Logger
}

func NewProposalService(r *repo.Repository, quality *QualityProfileService, broker *sse.Broker, log *logger.Logger) *ProposalService {
	return &ProposalService{repo: r, quality: quality, broker: broker, log: log}
}

// ProposeOrSupersede parks a picked release as a proposal for a propose-segment
// want, or supersedes an open proposal in place when this pick is strictly better.
// The already-resolved job carries the grab so approve is a pure DB replay. The
// covered set is the same one the auto pack path would grab; because all covered
// wants are linked at creation, a same-tick sibling that re-searches will find
// this proposal and no-op — at most one redundant search, one proposal per pack.
//
// Held/re-armed want deltas and the proposal event are emitted only after the tx
// commits, so a rolled-back propose emits nothing.
func (s *ProposalService) ProposeOrSupersede(ctx context.Context, want model.Want, job repo.CreateDownloadJobParams, isPack bool, coveredWantIDs []uuid.UUID, bin parsing.BinKey, score int, size int64, seeders int) error {
	create := repo.CreateProposalParams{
		TrackingID:     want.TrackingID,
		MediaItemID:    job.MediaItemID,
		IsPack:         isPack,
		Protocol:       job.Protocol,
		MediaType:      job.MediaType,
		SeasonID:       nilIfZeroUUID(job.SeasonID),
		EpisodeID:      nilIfZeroUUID(job.EpisodeID),
		IndexerID:      job.IndexerID,
		GUID:           job.Guid,
		CandidateTitle: job.CandidateTitle,
		CandidateLink:  job.CandidateLink,
		DownloaderID:   job.DownloaderID,
		LibraryID:      job.LibraryID,
		NameTemplateID: job.NameTemplateID,
		Size:           size,
		Seeders:        seeders,
		Bin:            bin,
		Score:          score,
	}

	var heldWants, rearmedWants []model.Want
	var changed bool
	err := s.repo.InTx(ctx, func(r *repo.Repository) error {
		existing, found, ferr := r.FindOpenProposalForWant(ctx, want.ID)
		if ferr != nil {
			return ferr
		}

		if found {
			better, berr := s.quality.StrictlyBetterQuality(ctx, want.QualityProfileID,
				qualityprofile.FileQuality{Bin: existing.Bin, Score: existing.Score},
				qualityprofile.FileQuality{Bin: bin, Score: score})
			if berr != nil {
				return berr
			}
			if !better {
				return nil // the open proposal already holds an equal-or-better release
			}
			if _, uerr := r.UpdateProposal(ctx, updateFromCreate(existing.ID, create)); uerr != nil {
				return uerr
			}
			held, rearmed, rerr := s.relinkCoverage(ctx, r, existing.ID, coveredWantIDs)
			if rerr != nil {
				return rerr
			}
			heldWants, rearmedWants = held, rearmed
			changed = true
			return nil
		}

		// Hold first: a want that's no longer claimable (already grabbed — e.g. this
		// tick raced an approve, or the segment was just switched) parks nothing, so
		// creating a proposal for it would orphan a row. Only the wants actually
		// parked get a proposal, and it links exactly those.
		held, herr := r.HoldProposedWants(ctx, coveredWantIDs)
		if herr != nil {
			return herr
		}
		if len(held) == 0 {
			return nil // nothing claimable — a clean no-op
		}
		created, cerr := r.CreateProposal(ctx, create)
		if cerr != nil {
			return cerr
		}
		for _, w := range held {
			if lerr := r.LinkProposalWant(ctx, created.ID, w.ID); lerr != nil {
				return lerr
			}
		}
		heldWants = held
		changed = true
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		s.emitWantUpdates(ctx, heldWants, rearmedWants)
		realtime.Emit(ctx, s.broker, realtime.ProposalUpdated(want.TrackingID))
	}
	return nil
}

// relinkCoverage reconciles a superseded proposal's want links to the new pick's
// covered set: it re-arms and unlinks wants dropped from coverage (the newer
// release doesn't carry them) and holds the new set. Returns the held and re-armed
// wants for post-commit emission.
func (s *ProposalService) relinkCoverage(ctx context.Context, r *repo.Repository, proposalID uuid.UUID, coveredWantIDs []uuid.UUID) (held, rearmed []model.Want, err error) {
	oldIDs, err := r.ListWantIDsByProposal(ctx, proposalID)
	if err != nil {
		return nil, nil, err
	}
	newSet := idSet(coveredWantIDs)
	oldSet := idSet(oldIDs)
	for _, id := range oldIDs {
		if newSet[id] {
			continue
		}
		if uerr := r.UnlinkProposalWant(ctx, proposalID, id); uerr != nil {
			return nil, nil, uerr
		}
		rw, ok, rerr := r.RearmProposedWant(ctx, id, "proposal superseded; release no longer covers this want")
		if rerr != nil {
			return nil, nil, rerr
		}
		if ok {
			rearmed = append(rearmed, rw)
		}
	}
	for _, id := range coveredWantIDs {
		if oldSet[id] {
			continue
		}
		if lerr := r.LinkProposalWant(ctx, proposalID, id); lerr != nil {
			return nil, nil, lerr
		}
	}
	held, err = r.HoldProposedWants(ctx, coveredWantIDs)
	if err != nil {
		return nil, nil, err
	}
	return held, rearmed, nil
}

// Approve grabs the proposed release and clears the proposal. Under one tx it
// locks the proposal (404 if already resolved), rebuilds the resolved grab from
// the stored columns, and replays it through the shared grab path (whose CAS
// clears hold='proposed' for free), then deletes the proposal. A pack whose
// covered wants have all advanced grabs nothing → 409; a single whose want is no
// longer grabbable → 409. Returns the grabbed wants for the pill transition.
func (s *ProposalService) Approve(ctx context.Context, proposalID uuid.UUID) ([]model.Want, error) {
	const op = "ProposalService.Approve"
	var grabbed []model.Want
	var trackingID uuid.UUID
	err := s.repo.InTx(ctx, func(r *repo.Repository) error {
		p, gerr := r.GetProposalForUpdate(ctx, proposalID)
		if gerr != nil {
			return gerr // 404 when already resolved
		}
		trackingID = p.TrackingID

		wantIDs, lerr := r.ListWantIDsByProposal(ctx, proposalID)
		if lerr != nil {
			return lerr
		}
		job := jobParamsFromProposal(p)

		if p.IsPack {
			claimed, cerr := grabPack(ctx, r, job, wantIDs)
			if cerr != nil {
				return cerr
			}
			if len(claimed) == 0 {
				return apperrors.Conflictf("proposal %s has no in-flight wants left to grab", proposalID).Op(op)
			}
			grabbed = claimed
		} else {
			if len(wantIDs) == 0 {
				return apperrors.Conflictf("proposal %s covers no want", proposalID).Op(op)
			}
			gw, ok, serr := grabSingle(ctx, r, model.Want{ID: wantIDs[0]}, job)
			if serr != nil {
				return serr
			}
			if !ok {
				return apperrors.Conflictf("proposal %s want is no longer grabbable", proposalID).Op(op)
			}
			grabbed = []model.Want{gw}
		}

		if _, derr := r.DeleteProposal(ctx, proposalID); derr != nil {
			return derr
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.emitWantUpdates(ctx, grabbed, nil)
	realtime.Emit(ctx, s.broker, realtime.ProposalUpdated(trackingID))
	return grabbed, nil
}

// Decline rejects the proposed release for every covered want and clears the
// proposal. Per want it excludes the release (reason 'declined') BEFORE re-arming
// it — the exclude-before-release ordering that stops the re-search from
// re-picking the just-declined release — then re-arms it to 'pending'. The next
// search proposes a different release. Returns the re-armed wants.
func (s *ProposalService) Decline(ctx context.Context, proposalID uuid.UUID) ([]model.Want, error) {
	var rearmed []model.Want
	var trackingID uuid.UUID
	err := s.repo.InTx(ctx, func(r *repo.Repository) error {
		p, gerr := r.GetProposalForUpdate(ctx, proposalID)
		if gerr != nil {
			return gerr // 404 when already resolved
		}
		trackingID = p.TrackingID

		wantIDs, lerr := r.ListWantIDsByProposal(ctx, proposalID)
		if lerr != nil {
			return lerr
		}
		detail := fmt.Sprintf("%s: proposal declined", p.CandidateTitle)
		for _, id := range wantIDs {
			if aerr := r.AddWantReleaseExclusion(ctx, repo.AddWantReleaseExclusionParams{
				WantID:    id,
				IndexerID: p.IndexerID,
				GUID:      p.GUID,
				Reason:    model.ExclusionDeclined,
				Detail:    &detail,
			}); aerr != nil {
				return aerr
			}
			rw, ok, rerr := r.RearmProposedWant(ctx, id, "proposal declined; re-searching for a different release")
			if rerr != nil {
				return rerr
			}
			if ok {
				rearmed = append(rearmed, rw)
			}
		}

		if _, derr := r.DeleteProposal(ctx, proposalID); derr != nil {
			return derr
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.emitWantUpdates(ctx, rearmed, nil)
	realtime.Emit(ctx, s.broker, realtime.ProposalUpdated(trackingID))
	return rearmed, nil
}

// ListForTracking returns a tracking's open proposals as view-models, each
// carrying the episode and want ids it covers so the series grid can map an
// episode to its proposal (a pack maps several episodes to the one proposal).
func (s *ProposalService) ListForTracking(ctx context.Context, trackingID uuid.UUID) ([]model.ProposalView, error) {
	proposals, err := s.repo.ListProposalsForTracking(ctx, trackingID)
	if err != nil {
		return nil, err
	}
	if len(proposals) == 0 {
		return []model.ProposalView{}, nil
	}

	ids := make([]uuid.UUID, len(proposals))
	for i, p := range proposals {
		ids[i] = p.ID
	}
	edges, err := s.repo.ListCoveredEpisodeIDsForProposals(ctx, ids)
	if err != nil {
		return nil, err
	}
	epsByProposal := make(map[uuid.UUID][]uuid.UUID, len(proposals))
	for _, e := range edges {
		epsByProposal[e.ProposalID] = append(epsByProposal[e.ProposalID], e.EpisodeID)
	}

	views := make([]model.ProposalView, 0, len(proposals))
	for _, p := range proposals {
		wantIDs, werr := s.repo.ListWantIDsByProposal(ctx, p.ID)
		if werr != nil {
			return nil, werr
		}
		eps := epsByProposal[p.ID]
		if eps == nil {
			eps = []uuid.UUID{}
		}
		views = append(views, model.ProposalView{
			Proposal:          p,
			CoveredEpisodeIDs: eps,
			CoveredWantIDs:    wantIDs,
		})
	}
	return views, nil
}

// emitWantUpdates fans WantUpdated deltas over the broker for the given want sets
// so the series pills flip (to "Suggested" on hold, back to "Searching" on
// re-arm/grab) without a refetch.
func (s *ProposalService) emitWantUpdates(ctx context.Context, sets ...[]model.Want) {
	for _, set := range sets {
		for _, w := range set {
			realtime.Emit(ctx, s.broker, realtime.WantUpdated(w))
		}
	}
}

// jobParamsFromProposal rebuilds the resolved CreateDownloadJobParams from a
// stored proposal — the inverse of the propose-time capture, so approve replays
// the exact grab with no re-parse.
func jobParamsFromProposal(p model.Proposal) repo.CreateDownloadJobParams {
	return repo.CreateDownloadJobParams{
		Protocol:       p.Protocol,
		MediaType:      p.MediaType,
		MediaItemID:    p.MediaItemID,
		SeasonID:       derefUUID(p.SeasonID),
		EpisodeID:      derefUUID(p.EpisodeID),
		IndexerID:      p.IndexerID,
		Guid:           p.GUID,
		CandidateTitle: p.CandidateTitle,
		CandidateLink:  p.CandidateLink,
		DownloaderID:   p.DownloaderID,
		LibraryID:      p.LibraryID,
		NameTemplateID: p.NameTemplateID,
	}
}

// updateFromCreate projects a CreateProposalParams onto an UpdateProposalParams
// for the supersede write, keyed by the existing proposal's id (tracking_id and
// media_item_id never change, so they're dropped).
func updateFromCreate(id uuid.UUID, c repo.CreateProposalParams) repo.UpdateProposalParams {
	return repo.UpdateProposalParams{
		ID:             id,
		IsPack:         c.IsPack,
		Protocol:       c.Protocol,
		MediaType:      c.MediaType,
		SeasonID:       c.SeasonID,
		EpisodeID:      c.EpisodeID,
		IndexerID:      c.IndexerID,
		GUID:           c.GUID,
		CandidateTitle: c.CandidateTitle,
		CandidateLink:  c.CandidateLink,
		DownloaderID:   c.DownloaderID,
		LibraryID:      c.LibraryID,
		NameTemplateID: c.NameTemplateID,
		Size:           c.Size,
		Seeders:        c.Seeders,
		Bin:            c.Bin,
		Score:          c.Score,
	}
}

// idSet builds a lookup set from a slice of ids.
func idSet(ids []uuid.UUID) map[uuid.UUID]bool {
	m := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// nilIfZeroUUID maps the zero UUID (the "none" sentinel CreateDownloadJobParams
// uses for absent season/episode) to a nil pointer for the proposal's nullable
// FK columns.
func nilIfZeroUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// derefUUID maps a nil pointer back to the zero UUID sentinel
// CreateDownloadJobParams expects for an absent season/episode.
func derefUUID(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}
