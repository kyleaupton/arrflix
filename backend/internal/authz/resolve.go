package authz

import (
	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/model"
)

// Resource identifies the target a scoped check is about. A nil *Resource is a
// global check ("does the user hold K unconditionally?"). A scoped grant only
// satisfies a check for its exact (Type, ID); a global grant satisfies any
// check.
type Resource struct {
	Type string
	ID   uuid.UUID
}

// grantScope is the resolution-relevant projection of one grant: its resource
// scope (nil resourceType == global) and polarity.
type grantScope struct {
	resourceType *string
	resourceID   *uuid.UUID
	deny         bool
}

// EffectiveSet is a user's grants indexed by permission key for O(1) lookup
// during resolution. Build it once per user (behind the service cache) and
// resolve many keys against it.
type EffectiveSet struct {
	byKey map[string][]grantScope
}

// NewEffectiveSet projects a user's grants into the lookup index.
func NewEffectiveSet(grants []model.Grant) *EffectiveSet {
	byKey := make(map[string][]grantScope, len(grants))
	for _, g := range grants {
		byKey[g.PermissionKey] = append(byKey[g.PermissionKey], grantScope{
			resourceType: g.ResourceType,
			resourceID:   g.ResourceID,
			deny:         g.Effect == model.GrantEffectDeny,
		})
	}
	return &EffectiveSet{byKey: byKey}
}

// Resolve answers whether the set grants key for resource. Only grants whose
// scope matches the check are candidates; among those, any deny wins (returns
// false), else any allow grants it, else it is denied. A nil set denies
// everything.
func Resolve(set *EffectiveSet, key string, resource *Resource) bool {
	if set == nil {
		return false
	}
	allowed := false
	for _, g := range set.byKey[key] {
		if !g.matches(resource) {
			continue
		}
		if g.deny {
			return false
		}
		allowed = true
	}
	return allowed
}

// matches reports whether this grant's scope covers the check. A global grant
// (nil resourceType) covers any check. A scoped grant covers only a check for
// the same (type, id) — it never satisfies a global check.
func (g grantScope) matches(r *Resource) bool {
	if g.resourceType == nil {
		return true
	}
	if r == nil {
		return false
	}
	return *g.resourceType == r.Type && g.resourceID != nil && *g.resourceID == r.ID
}
