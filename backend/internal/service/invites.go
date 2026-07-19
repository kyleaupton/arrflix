package service

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/token"
)

// inviteTTL is how long a magic link stays valid. A constant for now; a
// configurable setting is a possible later refinement. The invite email copy
// ("expires in 7 days") is written to match — keep them in step.
const inviteTTL = 7 * 24 * time.Hour

type InvitesService struct {
	repo          *repo.Repository
	notifications *NotificationService
	settings      *SettingsService
	log           *logger.Logger
}

func NewInvitesService(r *repo.Repository, notifications *NotificationService, settings *SettingsService, log *logger.Logger) *InvitesService {
	return &InvitesService{repo: r, notifications: notifications, settings: settings, log: log}
}

// Create issues (or re-issues) an invite for email with the given target role and
// returns the invite, the raw token (returned exactly once, for the caller to build
// the accept link), and whether the magic link was also emailed. An empty role
// defaults to "requester". Re-inviting an existing email regenerates the link
// ("resend"); inviting an address that already belongs to a user is a Conflict, so
// we never mint a link that can't be accepted.
//
// originHint is the admin request's Origin (the browser the invite was created
// from); it's the fallback public base URL when the site.base_url setting is unset.
// Emailing is best-effort and never fails the create: the copyable link is the
// source of truth, so a delivery-enqueue problem or unconfigured SMTP just yields
// emailed=false.
func (s *InvitesService) Create(ctx context.Context, email, role string, invitedBy uuid.UUID, originHint string) (model.Invite, string, bool, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return model.Invite{}, "", false, apperrors.Validation("invalid invite",
			apperrors.Field("body.email", "required"),
		).Op("InvitesService.Create")
	}
	if role == "" {
		role = "requester"
	}

	// Guard: an address that's already a user can't accept an invite (Users.Create
	// would 409). Fail here so the admin gets a clear signal, not a dead link.
	if _, err := s.repo.GetUserByEmail(ctx, email); err == nil {
		return model.Invite{}, "", false, apperrors.Conflictf("a user with email %q already exists", email).Op("InvitesService.Create")
	} else if !apperrors.IsNotFound(err) {
		return model.Invite{}, "", false, err
	}

	raw, hash, err := token.Generate()
	if err != nil {
		return model.Invite{}, "", false, apperrors.Internalf("generate invite token: %v", err).Op("InvitesService.Create")
	}

	invite, err := s.repo.UpsertInvite(ctx, repo.CreateInviteParams{
		Email:     email,
		InvitedBy: invitedBy,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(inviteTTL),
		Role:      role,
	})
	if err != nil {
		return model.Invite{}, "", false, err
	}

	emailed := s.tryEmailInvite(ctx, email, raw, originHint)
	return invite, raw, emailed, nil
}

// tryEmailInvite best-effort delivers the invite magic link. It resolves the public
// base URL, builds the accept link, and enqueues a transactional email — returning
// whether it was enqueued. Any failure (no base URL, SMTP unconfigured, enqueue
// error) yields false and is non-fatal: the caller still returns the copyable link.
func (s *InvitesService) tryEmailInvite(ctx context.Context, email, rawToken, originHint string) bool {
	base := s.resolveBaseURL(ctx, originHint)
	if base == "" {
		return false // No public URL to build a link from — copy-link only.
	}
	acceptURL := base + "/accept?token=" + url.QueryEscape(rawToken)

	emailed, err := s.notifications.EnqueueTransactionalEmail(ctx, notifications.EventInviteCreated, email,
		notifications.InviteCreatedPayload{AcceptURL: acceptURL})
	if err != nil {
		// The invite itself succeeded; the link is returned regardless. Log and move on.
		s.log.Warn().Err(err).Str("email", email).Msg("enqueue invite email failed; magic link still returned")
		return false
	}
	return emailed
}

// resolveBaseURL returns the public base URL for building emailed links, trimmed of
// any trailing slash: the site.base_url setting when set, else the admin request's
// Origin, else empty. site.base_url is authoritative (stable across admins, and the
// only trustworthy source for the unauthenticated flows to come); the request Origin
// is a zero-config fallback that's safe here because an admin creating an invite
// from their own browser can't spoof their own origin.
func (s *InvitesService) resolveBaseURL(ctx context.Context, originHint string) string {
	if raw, err := s.settings.GetRaw(ctx, "site.base_url"); err == nil {
		if configured, ok := raw.(string); ok && strings.TrimSpace(configured) != "" {
			return strings.TrimRight(strings.TrimSpace(configured), "/")
		}
	}
	return strings.TrimRight(strings.TrimSpace(originHint), "/")
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
