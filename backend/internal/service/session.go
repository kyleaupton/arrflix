package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

const (
	defaultAccessTTL  = 24 * time.Hour
	defaultSessionTTL = 90 * 24 * time.Hour
	// refreshGrace is how long a just-superseded refresh token stays valid, so a
	// benign multi-tab rotation race leapfrogs instead of tripping reuse detection.
	refreshGrace = time.Minute
)

// SessionService owns the session lifecycle: minting the access JWT + refresh
// token at login, rotating on refresh (with reuse detection), and revocation. The
// refresh token is opaque ("<sessionId>.<secret>"); only sha256(secret) is
// persisted, so a database read can never reconstruct a usable token.
type SessionService struct {
	repo       *repo.Repository
	jwtSecret  string
	accessTTL  time.Duration
	sessionTTL time.Duration
}

func NewSessionService(r *repo.Repository, jwtSecret string, accessTTL, sessionTTL time.Duration) *SessionService {
	if accessTTL <= 0 {
		accessTTL = defaultAccessTTL
	}
	if sessionTTL <= 0 {
		sessionTTL = defaultSessionTTL
	}
	return &SessionService{repo: r, jwtSecret: jwtSecret, accessTTL: accessTTL, sessionTTL: sessionTTL}
}

// Issued is the result of creating or refreshing a session: a short-lived access
// token to send as a Bearer, its expiry, and the opaque refresh token the caller
// stores in the HttpOnly cookie. RefreshToken is only ever returned here — the
// server keeps only its hash.
type Issued struct {
	AccessToken  string
	ExpiresAt    time.Time
	RefreshToken string
	Session      model.Session
}

// SessionMeta is the best-effort device context captured at login.
type SessionMeta struct {
	UserAgent *string
	IP        *string
}

// Create opens a new session for an already-authenticated user.
func (s *SessionService) Create(ctx context.Context, user model.User, meta SessionMeta) (Issued, error) {
	secret, hash, err := newRefreshSecret()
	if err != nil {
		return Issued{}, apperrors.Internalf("generate refresh secret: %v", err).Op("SessionService.Create")
	}
	sess, err := s.repo.CreateSession(ctx, repo.CreateSessionParams{
		UserID:           user.ID,
		RefreshHash:      hash,
		RefreshExpiresAt: time.Now().Add(s.sessionTTL),
		UserAgent:        meta.UserAgent,
		IP:               meta.IP,
	})
	if err != nil {
		return Issued{}, err
	}
	return s.issue(sess, user, secret)
}

// Refresh validates the presented refresh token, rotates it, and returns a fresh
// access + refresh token. A token that matches neither the current nor the
// grace-window previous secret is treated as reuse and revokes the session.
func (s *SessionService) Refresh(ctx context.Context, rawToken string) (Issued, error) {
	sid, secret, ok := splitRefreshToken(rawToken)
	if !ok {
		return Issued{}, apperrors.Unauthenticatedf("invalid refresh token").Op("SessionService.Refresh")
	}
	sess, found, err := s.repo.GetSession(ctx, sid)
	if err != nil {
		return Issued{}, err
	}
	if !found || sess.RevokedAt != nil || time.Now().After(sess.RefreshExpiresAt) {
		return Issued{}, apperrors.Unauthenticatedf("invalid refresh token").Op("SessionService.Refresh")
	}

	h := hashSecret(secret)
	switch {
	case subtle.ConstantTimeCompare(h, sess.RefreshHash) == 1:
		// matches the current secret — normal rotation
	case sess.PrevRefreshHash != nil &&
		subtle.ConstantTimeCompare(h, sess.PrevRefreshHash) == 1 &&
		sess.RotatedAt != nil && time.Since(*sess.RotatedAt) < refreshGrace:
		// matches the just-superseded secret within the grace window — a benign
		// multi-tab race; rotate again so the two tabs leapfrog
	default:
		// reuse / theft — burn the whole session so the real user must re-login
		_, _ = s.repo.RevokeSession(ctx, sess.ID)
		return Issued{}, apperrors.Unauthenticatedf("refresh token reuse detected").Op("SessionService.Refresh")
	}

	newSecret, newHash, err := newRefreshSecret()
	if err != nil {
		return Issued{}, apperrors.Internalf("generate refresh secret: %v", err).Op("SessionService.Refresh")
	}
	rotated, updated, err := s.repo.RotateSession(ctx, sess.ID, newHash)
	if err != nil {
		return Issued{}, err
	}
	if !updated {
		// Revoked between the read above and the rotate.
		return Issued{}, apperrors.Unauthenticatedf("invalid refresh token").Op("SessionService.Refresh")
	}

	user, err := s.repo.GetUserByID(ctx, rotated.UserID)
	if err != nil {
		return Issued{}, err
	}
	return s.issue(rotated, user, newSecret)
}

// RevokeByRefreshToken logs out the session the cookie belongs to. It verifies the
// presented secret matches the session before revoking, so a guessed session id
// cannot force-log-out someone else. Idempotent — an absent/foreign/garbage token
// is a no-op.
func (s *SessionService) RevokeByRefreshToken(ctx context.Context, rawToken string) error {
	sid, secret, ok := splitRefreshToken(rawToken)
	if !ok {
		return nil
	}
	sess, found, err := s.repo.GetSession(ctx, sid)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	h := hashSecret(secret)
	matches := subtle.ConstantTimeCompare(h, sess.RefreshHash) == 1 ||
		(sess.PrevRefreshHash != nil && subtle.ConstantTimeCompare(h, sess.PrevRefreshHash) == 1)
	if !matches {
		return nil
	}
	_, err = s.repo.RevokeSession(ctx, sid)
	return err
}

// Revoke ends one session by id (owner enforcement is the caller's responsibility).
func (s *SessionService) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.repo.RevokeSession(ctx, sessionID)
	return err
}

// RevokeAllForUser ends every active session for a user ("log out everywhere").
func (s *SessionService) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	_, err := s.repo.RevokeAllSessionsForUser(ctx, userID)
	return err
}

// ListForUser returns the user's active sessions for the devices UI.
func (s *SessionService) ListForUser(ctx context.Context, userID uuid.UUID) ([]model.Session, error) {
	return s.repo.ListActiveSessionsByUser(ctx, userID)
}

// ListDevicesForUser returns the user's active sessions each joined to its push
// subscription — the unified devices list.
func (s *SessionService) ListDevicesForUser(ctx context.Context, userID uuid.UUID) ([]model.SessionDevice, error) {
	return s.repo.ListActiveSessionDevicesByUser(ctx, userID)
}

// RevokeForUser revokes one of the caller's own sessions. A session that isn't the
// caller's (or doesn't exist) affects 0 rows and is a 404 — it never reveals that
// the id belongs to someone else.
func (s *SessionService) RevokeForUser(ctx context.Context, userID, sessionID uuid.UUID) error {
	n, err := s.repo.RevokeSessionForUser(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	if n == 0 {
		return apperrors.NotFoundf("session %s not found", sessionID).Op("SessionService.RevokeForUser")
	}
	return nil
}

// RevokeOthersForUser revokes every active session for the caller except the one
// they're currently on ("log out other devices").
func (s *SessionService) RevokeOthersForUser(ctx context.Context, userID, keepSessionID uuid.UUID) (int64, error) {
	return s.repo.RevokeOtherSessionsForUser(ctx, userID, keepSessionID)
}

func (s *SessionService) issue(sess model.Session, user model.User, secret string) (Issued, error) {
	sid := sess.ID
	access, exp, err := signAccessToken(s.jwtSecret, user.ID, user.Email, user.Username, &sid, s.accessTTL)
	if err != nil {
		return Issued{}, apperrors.Internalf("sign access token: %v", err).Op("SessionService.issue")
	}
	return Issued{
		AccessToken:  access,
		ExpiresAt:    exp,
		RefreshToken: sess.ID.String() + "." + secret,
		Session:      sess,
	}, nil
}

// signAccessToken builds a signed HS256 access token. sid is optional (nil for the
// legacy identity-only token the test harness mints); when present it binds the
// token to a session so later phases can scope revocation.
func signAccessToken(secret string, userID uuid.UUID, email *string, username string, sid *uuid.UUID, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	claims := jwt.MapClaims{
		"sub":   userID.String(),
		"email": email,
		"name":  username,
		"exp":   exp.Unix(),
		"iat":   now.Unix(),
	}
	if sid != nil {
		claims["sid"] = sid.String()
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp, nil
}

// newRefreshSecret returns a fresh high-entropy refresh secret and its sha256 hash.
func newRefreshSecret() (secret string, hash []byte, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	secret = base64.RawURLEncoding.EncodeToString(buf)
	return secret, hashSecret(secret), nil
}

func hashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// splitRefreshToken parses the "<sessionId>.<secret>" wire form. A UUID contains
// no '.' and base64url no '.', so a single split is unambiguous.
func splitRefreshToken(raw string) (uuid.UUID, string, bool) {
	i := strings.IndexByte(raw, '.')
	if i <= 0 || i >= len(raw)-1 {
		return uuid.Nil, "", false
	}
	id, err := uuid.Parse(raw[:i])
	if err != nil {
		return uuid.Nil, "", false
	}
	return id, raw[i+1:], true
}
