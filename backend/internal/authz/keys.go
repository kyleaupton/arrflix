// Package authz is the pure permission-resolution core of arrflix's RBAC. It
// holds the frozen permission catalog as code (the keys below), builds a
// per-user effective grant set, and resolves "does this user hold key K on
// resource R?" with deny-wins semantics.
//
// The package is pure in the domain-module sense: it imports only model,
// qualityprofile, and stdlib — no repo, sqlc, or pgtype. Persistence (loading a
// user's grants) and the caching layer live on AuthzService in
// internal/service; the .own/.any ownership fallback lives there too, because
// "own" depends on the target entity's owner, known only at the call site.
//
// AllKeys is the single source of truth for the concrete keyspace. An
// integration drift test binds it to the seeded permission table, so a key that
// exists in code but not the DB (or vice versa) fails the build.
package authz

import (
	"strings"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
)

// Flat permission keys — the catalog's non-parameterized entries.
const (
	LibraryRead  = "library.read"
	LibraryWrite = "library.write"
	LibraryScan  = "library.scan"

	MediaWrite = "media.write"

	RequestCancelOwn = "requests.cancel.own"
	RequestCancelAny = "requests.cancel.any"
	RequestViewOwn   = "requests.view.own"
	RequestViewAny   = "requests.view.any"

	TrackingCreateOwn = "tracking.create.own"
	TrackingCreateAny = "tracking.create.any"
	TrackingCancelOwn = "tracking.cancel.own"
	TrackingCancelAny = "tracking.cancel.any"

	JobsRead   = "jobs.read"
	JobsManage = "jobs.manage"

	HygieneRead    = "hygiene.read"
	HygieneResolve = "hygiene.resolve"

	AdminUsersManage       = "admin.users.manage"
	AdminSettingsRead      = "admin.settings.read"
	AdminSettingsWrite     = "admin.settings.write"
	AdminIndexersManage    = "admin.indexers.manage"
	AdminDownloadersManage = "admin.downloaders.manage"
)

// flatKeys is every non-parameterized key, in catalog order. AllKeys appends
// the parameterized families to this.
var flatKeys = []string{
	LibraryRead, LibraryWrite, LibraryScan,
	MediaWrite,
	RequestCancelOwn, RequestCancelAny, RequestViewOwn, RequestViewAny,
	TrackingCreateOwn, TrackingCreateAny, TrackingCancelOwn, TrackingCancelAny,
	JobsRead, JobsManage,
	HygieneRead, HygieneResolve,
	AdminUsersManage, AdminSettingsRead, AdminSettingsWrite,
	AdminIndexersManage, AdminDownloadersManage,
}

// allMediaTypes is the concrete media-type keyspace the parameterized families
// enumerate over. Kept local so the catalog owns its enumeration rather than
// leaning on a model-level "all types" list that other code might reshape.
var allMediaTypes = []model.MediaType{model.MediaTypeMovie, model.MediaTypeSeries}

// RequestCreate builds the key gating a request at a given media type and tier,
// e.g. RequestCreate(MediaTypeMovie, "HD") == "requests.create:movie:hd". The
// tier is lowercased because catalog tiers are "HD"/"4K" but keys use hd/4k.
func RequestCreate(t model.MediaType, tier string) string {
	return "requests.create:" + string(t) + ":" + strings.ToLower(tier)
}

// RequestAutoApprove builds the key that lets a request at (type, tier)
// auto-spawn past the approval queue, e.g. "requests.auto_approve:series:4k".
func RequestAutoApprove(t model.MediaType, tier string) string {
	return "requests.auto_approve:" + string(t) + ":" + strings.ToLower(tier)
}

// RequestApprove builds the per-media-type approve key, e.g.
// "requests.approve:series".
func RequestApprove(t model.MediaType) string {
	return "requests.approve:" + string(t)
}

// RequestDeny builds the per-media-type deny key, e.g. "requests.deny:movie".
func RequestDeny(t model.MediaType) string {
	return "requests.deny:" + string(t)
}

// AllKeys enumerates the full concrete keyspace: the flat consts plus the four
// parameterized families expanded over {movie, series} and, where tiered, over
// qualityprofile.AllTiers (lowercased). It is the source of truth the drift
// test locks the seeded permission table against.
func AllKeys() []string {
	out := make([]string, 0, len(flatKeys)+len(allMediaTypes)*(2*len(qualityprofile.AllTiers)+2))
	out = append(out, flatKeys...)
	for _, t := range allMediaTypes {
		for _, tier := range qualityprofile.AllTiers {
			out = append(out, RequestCreate(t, string(tier)))
			out = append(out, RequestAutoApprove(t, string(tier)))
		}
		out = append(out, RequestApprove(t))
		out = append(out, RequestDeny(t))
	}
	return out
}
