package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type InvitesService struct {
	repo *repo.Repository
}

func NewInvitesService(r *repo.Repository) *InvitesService {
	return &InvitesService{repo: r}
}

// Create adds a new email to the invite list.
func (s *InvitesService) Create(ctx context.Context, email string, invitedBy uuid.UUID) (model.Invite, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return model.Invite{}, apperrors.Validation("invalid invite",
			apperrors.Field("body.email", "required"),
		).Op("InvitesService.Create")
	}
	// Repo translates SQLSTATE 23505 unique violation into Conflict; pass through.
	return s.repo.CreateInvite(ctx, email, invitedBy)
}

// List returns all invites.
func (s *InvitesService) List(ctx context.Context) ([]model.Invite, error) {
	return s.repo.ListInvites(ctx)
}

// Delete removes an invite.
func (s *InvitesService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteInvite(ctx, id)
}

// CheckAndClaim looks up an unclaimed invite by email and marks it as claimed.
// Returns nil if the invite exists and was claimed, an error otherwise.
func (s *InvitesService) CheckAndClaim(ctx context.Context, email string) error {
	invite, err := s.repo.GetInviteByEmail(ctx, email)
	if err != nil {
		// Repo's NotFound carries "invite for ... not found"; the user-facing
		// surface (signup flow) should describe this in invite-claim terms.
		if apperrors.IsNotFound(err) {
			return apperrors.Wrap(err, "no invite found for %q", email)
		}
		return err
	}
	return s.repo.ClaimInvite(ctx, invite.ID)
}
