package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/token"
)

// inviteTTL is how long a magic link stays valid. A constant for now; a
// configurable setting is a possible later refinement.
const inviteTTL = 7 * 24 * time.Hour

type InvitesService struct {
	repo *repo.Repository
}

func NewInvitesService(r *repo.Repository) *InvitesService {
	return &InvitesService{repo: r}
}

// Create issues (or re-issues) an invite for email with the given target role and
// returns the invite plus the raw token — returned exactly once, for the caller to
// build the accept link. An empty role defaults to "requester". Re-inviting an
// existing email regenerates the link ("resend"); inviting an address that already
// belongs to a user is a Conflict, so we never mint a link that can't be accepted.
func (s *InvitesService) Create(ctx context.Context, email, role string, invitedBy uuid.UUID) (model.Invite, string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return model.Invite{}, "", apperrors.Validation("invalid invite",
			apperrors.Field("body.email", "required"),
		).Op("InvitesService.Create")
	}
	if role == "" {
		role = "requester"
	}

	// Guard: an address that's already a user can't accept an invite (Users.Create
	// would 409). Fail here so the admin gets a clear signal, not a dead link.
	if _, err := s.repo.GetUserByEmail(ctx, email); err == nil {
		return model.Invite{}, "", apperrors.Conflictf("a user with email %q already exists", email).Op("InvitesService.Create")
	} else if !apperrors.IsNotFound(err) {
		return model.Invite{}, "", err
	}

	raw, hash, err := token.Generate()
	if err != nil {
		return model.Invite{}, "", apperrors.Internalf("generate invite token: %v", err).Op("InvitesService.Create")
	}

	invite, err := s.repo.UpsertInvite(ctx, repo.CreateInviteParams{
		Email:     email,
		InvitedBy: invitedBy,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(inviteTTL),
		Role:      role,
	})
	if err != nil {
		return model.Invite{}, "", err
	}
	return invite, raw, nil
}

// List returns all invites.
func (s *InvitesService) List(ctx context.Context) ([]model.Invite, error) {
	return s.repo.ListInvites(ctx)
}

// Delete removes an invite.
func (s *InvitesService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteInvite(ctx, id)
}

// ValidateToken resolves a raw invite token to its unclaimed, unexpired invite. It
// does NOT consume the invite — the caller claims it only after account creation
// succeeds, so a failed signup (weak password, taken email) leaves the link live.
// A missing/expired/consumed token yields a generic NotFound that never reveals
// which of those it was.
func (s *InvitesService) ValidateToken(ctx context.Context, rawToken string) (model.Invite, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return model.Invite{}, apperrors.NotFoundf("invite link is invalid or has expired").Op("InvitesService.ValidateToken")
	}
	invite, err := s.repo.GetInviteByTokenHash(ctx, token.Hash(rawToken))
	if err != nil {
		if apperrors.IsNotFound(err) {
			return model.Invite{}, apperrors.NotFoundf("invite link is invalid or has expired").Op("InvitesService.ValidateToken")
		}
		return model.Invite{}, err
	}
	return invite, nil
}

// Claim marks an invite consumed. Idempotent at the SQL level (claims only when
// still unclaimed).
func (s *InvitesService) Claim(ctx context.Context, id uuid.UUID) error {
	return s.repo.ClaimInvite(ctx, id)
}

// CheckAndClaim looks up an unclaimed invite by email and marks it claimed. This is
// the Plex SSO onboarding path: Plex verifies the email, so matching an invite by
// email (rather than a token link) is sound there. The link-acceptance flow uses
// ValidateToken + Claim instead.
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
