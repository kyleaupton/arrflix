package service

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/authz"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// AuthzService answers permission checks for a user, backed by a per-user cache
// of their effective grant set. The cache has no TTL: its lifetime is the
// process, and correctness relies on Bust being called whenever a user's grants
// change (today, only role assignment). Nothing enforces these checks yet —
// this is the primitive; consumers arrive in a later phase.
type AuthzService struct {
	repo  *repo.Repository
	mu    sync.RWMutex
	cache map[uuid.UUID]*authz.EffectiveSet
}

func NewAuthzService(r *repo.Repository) *AuthzService {
	return &AuthzService{
		repo:  r,
		cache: make(map[uuid.UUID]*authz.EffectiveSet),
	}
}

// effectiveSet returns the user's cached grant set, loading it from the repo on
// a miss. A concurrent miss may load twice; the results are identical, so the
// last write wins harmlessly.
func (s *AuthzService) effectiveSet(ctx context.Context, userID uuid.UUID) (*authz.EffectiveSet, error) {
	s.mu.RLock()
	set, ok := s.cache[userID]
	s.mu.RUnlock()
	if ok {
		return set, nil
	}

	grants, err := s.repo.ListUserGrants(ctx, userID)
	if err != nil {
		return nil, err
	}
	set = authz.NewEffectiveSet(grants)

	s.mu.Lock()
	s.cache[userID] = set
	s.mu.Unlock()
	return set, nil
}

// Can reports whether the user holds key for the given resource (nil resource =
// global check). See authz.Resolve for the deny-wins semantics.
func (s *AuthzService) Can(ctx context.Context, userID uuid.UUID, key string, resource *authz.Resource) (bool, error) {
	set, err := s.effectiveSet(ctx, userID)
	if err != nil {
		return false, err
	}
	return authz.Resolve(set, key, resource), nil
}

// Require is Can as a gate: it returns a Forbidden error when the permission is
// absent. The wire detail is deliberately generic — the checked key goes in the
// op/logs, not on the wire, so the keyspace isn't leaked to clients.
func (s *AuthzService) Require(ctx context.Context, userID uuid.UUID, key string, resource *authz.Resource) error {
	ok, err := s.Can(ctx, userID, key, resource)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.Forbiddenf("insufficient permissions").Op("AuthzService.Require:" + key)
	}
	return nil
}

// EffectiveKeys returns every catalog key the user holds at global scope
// (nil resource). It is the capability list /auth/me hands the frontend so the
// UI can gate controls without a second round-trip. Resource-scoped grants are
// intentionally excluded — scoped checks stay server-side.
func (s *AuthzService) EffectiveKeys(ctx context.Context, userID uuid.UUID) ([]string, error) {
	set, err := s.effectiveSet(ctx, userID)
	if err != nil {
		return nil, err
	}
	all := authz.AllKeys()
	out := make([]string, 0, len(all))
	for _, k := range all {
		if authz.Resolve(set, k, nil) {
			out = append(out, k)
		}
	}
	return out, nil
}

// CanOwnAny resolves an own/any permission pair: the user passes if they hold
// base+".any", or they hold base+".own" and own the target. base is the
// dotted stem (e.g. "requests.cancel"); the caller supplies isOwner because
// ownership depends on the target entity, which authz can't see.
func (s *AuthzService) CanOwnAny(ctx context.Context, userID uuid.UUID, base string, isOwner bool) (bool, error) {
	any, err := s.Can(ctx, userID, base+".any", nil)
	if err != nil {
		return false, err
	}
	if any {
		return true, nil
	}
	if !isOwner {
		return false, nil
	}
	return s.Can(ctx, userID, base+".own", nil)
}

// Bust drops the user's cached grant set so the next check reloads from the
// repo. Call it after any mutation to the user's roles or direct grants.
func (s *AuthzService) Bust(userID uuid.UUID) {
	s.mu.Lock()
	delete(s.cache, userID)
	s.mu.Unlock()
}
