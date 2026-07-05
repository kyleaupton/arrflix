package repo

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
)

// CreateProposalParams is the domain-shaped input for CreateProposal. Mirrors the
// writeable subset of model.Proposal (omits server-managed ID/timestamps). The
// resolved-grab fields match repo.CreateDownloadJobParams so Approve can rebuild
// that struct from the stored row.
type CreateProposalParams struct {
	TrackingID     uuid.UUID
	MediaItemID    uuid.UUID
	IsPack         bool
	Protocol       string
	MediaType      string
	SeasonID       *uuid.UUID
	EpisodeID      *uuid.UUID
	IndexerID      int64
	GUID           string
	CandidateTitle string
	CandidateLink  string
	DownloaderID   uuid.UUID
	LibraryID      uuid.UUID
	NameTemplateID uuid.UUID
	Size           int64
	Seeders        int
	Bin            parsing.BinKey
	Score          int
}

// UpdateProposalParams is the domain-shaped input for UpdateProposal — the
// supersede write. It replaces every mutable column (release/display/bin/score)
// in place, keyed by ID; tracking_id/media_item_id never change.
type UpdateProposalParams struct {
	ID             uuid.UUID
	IsPack         bool
	Protocol       string
	MediaType      string
	SeasonID       *uuid.UUID
	EpisodeID      *uuid.UUID
	IndexerID      int64
	GUID           string
	CandidateTitle string
	CandidateLink  string
	DownloaderID   uuid.UUID
	LibraryID      uuid.UUID
	NameTemplateID uuid.UUID
	Size           int64
	Seeders        int
	Bin            parsing.BinKey
	Score          int
}

// toModelProposal translates a persistence-shaped dbgen.Proposal into the
// domain-shaped model.Proposal, reconstructing the BinKey from its three columns.
func toModelProposal(row dbgen.Proposal) model.Proposal {
	return model.Proposal{
		ID:             uuidFromPgtype(row.ID),
		TrackingID:     uuidFromPgtype(row.TrackingID),
		MediaItemID:    uuidFromPgtype(row.MediaItemID),
		IsPack:         row.IsPack,
		Protocol:       row.Protocol,
		MediaType:      row.MediaType,
		SeasonID:       uuidPtrFromPgtype(row.SeasonID),
		EpisodeID:      uuidPtrFromPgtype(row.EpisodeID),
		IndexerID:      row.IndexerID,
		GUID:           row.Guid,
		CandidateTitle: row.CandidateTitle,
		CandidateLink:  row.CandidateLink,
		DownloaderID:   uuidFromPgtype(row.DownloaderID),
		LibraryID:      uuidFromPgtype(row.LibraryID),
		NameTemplateID: uuidFromPgtype(row.NameTemplateID),
		Size:           row.Size,
		Seeders:        int(row.Seeders),
		Bin: parsing.BinKey{
			Source:     parsing.Source(row.BinSource),
			Resolution: parsing.Resolution(row.BinResolution),
			Modifier:   parsing.Modifier(row.BinModifier),
		},
		Score:     int(row.Score),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

func (r *Repository) CreateProposal(ctx context.Context, params CreateProposalParams) (model.Proposal, error) {
	row, err := r.Q.CreateProposal(ctx, dbgen.CreateProposalParams{
		TrackingID:     pgtypeFromUUID(params.TrackingID),
		MediaItemID:    pgtypeFromUUID(params.MediaItemID),
		IsPack:         params.IsPack,
		Protocol:       params.Protocol,
		MediaType:      params.MediaType,
		SeasonID:       pgtypeFromUUIDPtr(params.SeasonID),
		EpisodeID:      pgtypeFromUUIDPtr(params.EpisodeID),
		IndexerID:      params.IndexerID,
		Guid:           params.GUID,
		CandidateTitle: params.CandidateTitle,
		CandidateLink:  params.CandidateLink,
		DownloaderID:   pgtypeFromUUID(params.DownloaderID),
		LibraryID:      pgtypeFromUUID(params.LibraryID),
		NameTemplateID: pgtypeFromUUID(params.NameTemplateID),
		Size:           params.Size,
		Seeders:        int32(params.Seeders),
		BinSource:      string(params.Bin.Source),
		BinResolution:  string(params.Bin.Resolution),
		BinModifier:    string(params.Bin.Modifier),
		Score:          int32(params.Score),
	})
	if err != nil {
		return model.Proposal{}, apperrors.FromPg(err, "create proposal for tracking %s", params.TrackingID)
	}
	return toModelProposal(row), nil
}

func (r *Repository) GetProposal(ctx context.Context, id uuid.UUID) (model.Proposal, error) {
	row, err := r.Q.GetProposal(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.Proposal{}, apperrors.FromPg(err, "proposal %s not found", id)
	}
	return toModelProposal(row), nil
}

// GetProposalForUpdate reads a proposal under a row lock (FOR UPDATE), the guard
// approve/decline/supersede take so concurrent resolutions serialize. A
// pgx.ErrNoRows flows through FromPg as NotFound — the proposal was already
// resolved.
func (r *Repository) GetProposalForUpdate(ctx context.Context, id uuid.UUID) (model.Proposal, error) {
	row, err := r.Q.GetProposalForUpdate(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.Proposal{}, apperrors.FromPg(err, "proposal %s not found", id)
	}
	return toModelProposal(row), nil
}

// FindOpenProposalForWant returns the proposal covering a want. The bool reports
// existence: false (a 0-row match, surfaced as pgx.ErrNoRows) means no open
// proposal — the propose path creates one. A proposal_want edge existing is the
// same as an open proposal, since a resolved proposal is deleted.
func (r *Repository) FindOpenProposalForWant(ctx context.Context, wantID uuid.UUID) (model.Proposal, bool, error) {
	row, err := r.Q.FindOpenProposalForWant(ctx, pgtypeFromUUID(wantID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Proposal{}, false, nil
		}
		return model.Proposal{}, false, apperrors.FromPg(err, "find open proposal for want %s", wantID)
	}
	return toModelProposal(row), true, nil
}

func (r *Repository) ListProposalsForTracking(ctx context.Context, trackingID uuid.UUID) ([]model.Proposal, error) {
	rows, err := r.Q.ListProposalsForTracking(ctx, pgtypeFromUUID(trackingID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list proposals for tracking %s", trackingID)
	}
	out := make([]model.Proposal, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelProposal(row))
	}
	return out, nil
}

// UpdateProposal replaces a proposal's release/display/bin/score in place — the
// supersede write, keeping the same id.
func (r *Repository) UpdateProposal(ctx context.Context, params UpdateProposalParams) (model.Proposal, error) {
	row, err := r.Q.UpdateProposal(ctx, dbgen.UpdateProposalParams{
		ID:             pgtypeFromUUID(params.ID),
		IsPack:         params.IsPack,
		Protocol:       params.Protocol,
		MediaType:      params.MediaType,
		SeasonID:       pgtypeFromUUIDPtr(params.SeasonID),
		EpisodeID:      pgtypeFromUUIDPtr(params.EpisodeID),
		IndexerID:      params.IndexerID,
		Guid:           params.GUID,
		CandidateTitle: params.CandidateTitle,
		CandidateLink:  params.CandidateLink,
		DownloaderID:   pgtypeFromUUID(params.DownloaderID),
		LibraryID:      pgtypeFromUUID(params.LibraryID),
		NameTemplateID: pgtypeFromUUID(params.NameTemplateID),
		Size:           params.Size,
		Seeders:        int32(params.Seeders),
		BinSource:      string(params.Bin.Source),
		BinResolution:  string(params.Bin.Resolution),
		BinModifier:    string(params.Bin.Modifier),
		Score:          int32(params.Score),
	})
	if err != nil {
		return model.Proposal{}, apperrors.FromPg(err, "update proposal %s", params.ID)
	}
	return toModelProposal(row), nil
}

// DeleteProposal removes a resolved proposal (its proposal_want edges cascade).
// The bool reports whether this call deleted it: false (a 0-row DELETE, surfaced
// as pgx.ErrNoRows) means a concurrent resolve already removed it — a clean
// conflict the caller reads as "already resolved", not an error.
func (r *Repository) DeleteProposal(ctx context.Context, id uuid.UUID) (bool, error) {
	_, err := r.Q.DeleteProposal(ctx, pgtypeFromUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, apperrors.FromPg(err, "delete proposal %s", id)
	}
	return true, nil
}

// LinkProposalWant records that a proposal would advance a want. Idempotent (ON
// CONFLICT DO NOTHING).
func (r *Repository) LinkProposalWant(ctx context.Context, proposalID, wantID uuid.UUID) error {
	return apperrors.FromPg(r.Q.LinkProposalWant(ctx, dbgen.LinkProposalWantParams{
		ProposalID: pgtypeFromUUID(proposalID),
		WantID:     pgtypeFromUUID(wantID),
	}), "link proposal %s to want %s", proposalID, wantID)
}

// UnlinkProposalWant removes one proposal↔want edge — used on supersede when a
// want drops out of the newer pick's coverage.
func (r *Repository) UnlinkProposalWant(ctx context.Context, proposalID, wantID uuid.UUID) error {
	return apperrors.FromPg(r.Q.UnlinkProposalWant(ctx, dbgen.UnlinkProposalWantParams{
		ProposalID: pgtypeFromUUID(proposalID),
		WantID:     pgtypeFromUUID(wantID),
	}), "unlink proposal %s from want %s", proposalID, wantID)
}

// ListWantsByProposal returns the wants a proposal covers.
func (r *Repository) ListWantsByProposal(ctx context.Context, proposalID uuid.UUID) ([]model.Want, error) {
	rows, err := r.Q.ListWantsByProposal(ctx, pgtypeFromUUID(proposalID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list wants for proposal %s", proposalID)
	}
	out := make([]model.Want, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelWant(row))
	}
	return out, nil
}

// ListWantIDsByProposal returns just the covered want ids — the set approve/
// decline iterate over.
func (r *Repository) ListWantIDsByProposal(ctx context.Context, proposalID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.Q.ListWantIDsByProposal(ctx, pgtypeFromUUID(proposalID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list want ids for proposal %s", proposalID)
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, uuidFromPgtype(row))
	}
	return out, nil
}

// ProposalCoveredEpisode is one (proposal, episode) coverage edge — the read
// join-model the series grid maps an episode to its proposal with.
type ProposalCoveredEpisode struct {
	ProposalID uuid.UUID
	EpisodeID  uuid.UUID
}

// ListCoveredEpisodeIDsForProposals returns the covered-episode edges for a set
// of proposals in one round trip, backing the ProposalView read model. Movie
// wants (NULL episode_id) are excluded by the query.
func (r *Repository) ListCoveredEpisodeIDsForProposals(ctx context.Context, proposalIDs []uuid.UUID) ([]ProposalCoveredEpisode, error) {
	pgIDs := make([]pgtype.UUID, len(proposalIDs))
	for i, id := range proposalIDs {
		pgIDs[i] = pgtypeFromUUID(id)
	}
	rows, err := r.Q.ListCoveredEpisodeIDsForProposals(ctx, pgIDs)
	if err != nil {
		return nil, apperrors.FromPg(err, "list covered episodes for proposals")
	}
	out := make([]ProposalCoveredEpisode, 0, len(rows))
	for _, row := range rows {
		out = append(out, ProposalCoveredEpisode{
			ProposalID: uuidFromPgtype(row.ProposalID),
			EpisodeID:  uuidFromPgtype(row.EpisodeID),
		})
	}
	return out, nil
}
