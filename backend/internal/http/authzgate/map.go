package authzgate

import (
	"github.com/kyleaupton/arrflix/internal/authz"
	"github.com/kyleaupton/arrflix/internal/model"
)

// operationPerms maps every huma operation ID to its authorization requirement.
// It is the single source of truth the fail-closed gate keys on: an operation
// absent from this map is denied (403), and the coverage test (map_test.go)
// fails CI naming any unclassified — or stale — entry.
//
// Grouped by requirement. A few assignments are judgment calls worth stating:
//   - The acquisition-operator surface (proposals-*, download-candidates-*,
//     tracking-{set-autonomy,retry}, wants-cancel) maps to jobs.manage: admin
//     and co_admin hold it, requester and viewer don't, so access is correct
//     without a dedicated acquisition.manage key (a possible later addition).
//   - filesystem-browse maps to library.write (operator-only server-FS browse).
var operationPerms = map[string]requirement{
	// ----- Public (gate skips; must match middlewares.publicPathSet) -----
	"auth-login":         authPublic(),
	"auth-signup":        authPublic(),
	"auth-plex-exchange": authPublic(),
	"health-check":       authPublic(),
	"bootstrap-get":      authPublic(),
	"version-get":        authPublic(),
	"setup-status":       authPublic(),
	"setup-initialize":   authPublic(),
	"setup-tmdb":         authPublic(),

	// ----- Authenticated (valid login, no permission key) -----
	"auth-me":                       authUser(),
	"users-get-profile":             authUser(),
	"users-update-profile":          authUser(),
	"users-update-profile-password": authUser(),
	"events-stream":                 authUser(),
	"events-subscriptions-list":     authUser(),
	"events-subscriptions-add":      authUser(),
	"events-subscriptions-remove":   authUser(),
	"notifications-list":            authUser(),
	"notifications-unread-count":    authUser(),
	"notifications-mark-read":       authUser(),
	"notifications-mark-all-read":   authUser(),
	"notification-preferences-get":  authUser(),
	"notification-preferences-set":  authUser(),

	// ----- admin.users.manage -----
	"users-list":            perm(authz.AdminUsersManage),
	"users-get":             perm(authz.AdminUsersManage),
	"users-update":          perm(authz.AdminUsersManage),
	"users-delete":          perm(authz.AdminUsersManage),
	"users-update-password": perm(authz.AdminUsersManage),
	"users-assign-role":     perm(authz.AdminUsersManage),
	"invites-list":          perm(authz.AdminUsersManage),
	"invites-create":        perm(authz.AdminUsersManage),
	"invites-delete":        perm(authz.AdminUsersManage),
	"roles-list":            perm(authz.AdminUsersManage),

	// ----- library.read (shared library state, not private) -----
	"libraries-list":       perm(authz.LibraryRead),
	"libraries-get":        perm(authz.LibraryRead),
	"library-list":         perm(authz.LibraryRead),
	"media-search":         perm(authz.LibraryRead),
	"media-get-movie":      perm(authz.LibraryRead),
	"media-get-series":     perm(authz.LibraryRead),
	"media-get-person":     perm(authz.LibraryRead),
	"feed-get":             perm(authz.LibraryRead),
	"files-match-decision": perm(authz.LibraryRead),
	"tracking-list":        perm(authz.LibraryRead),
	"tracking-get":         perm(authz.LibraryRead),
	"tracking-wants":       perm(authz.LibraryRead),
	"tracking-by-tmdb":     perm(authz.LibraryRead),
	"tracking-requesters":  perm(authz.LibraryRead),
	"tracking-proposals":   perm(authz.LibraryRead),

	// ----- library.write -----
	"libraries-create":  perm(authz.LibraryWrite),
	"libraries-update":  perm(authz.LibraryWrite),
	"libraries-delete":  perm(authz.LibraryWrite),
	"filesystem-browse": perm(authz.LibraryWrite),

	// ----- library.scan -----
	"libraries-scan": perm(authz.LibraryScan),

	// ----- media.write -----
	"files-match":   perm(authz.MediaWrite),
	"files-unmatch": perm(authz.MediaWrite),
	"files-detach":  perm(authz.MediaWrite),

	// ----- hygiene.read -----
	"unmatched-files-list": perm(authz.HygieneRead),
	"unmatched-files-get":  perm(authz.HygieneRead),

	// ----- jobs.read -----
	"download-jobs-list":              perm(authz.JobsRead),
	"download-jobs-get":               perm(authz.JobsRead),
	"download-jobs-timeline":          perm(authz.JobsRead),
	"download-jobs-list-import-tasks": perm(authz.JobsRead),
	"download-jobs-history":           perm(authz.JobsRead),
	"download-jobs-list-for-movie":    perm(authz.JobsRead),
	"download-jobs-list-for-series":   perm(authz.JobsRead),
	"import-tasks-list":               perm(authz.JobsRead),
	"import-tasks-counts":             perm(authz.JobsRead),
	"import-tasks-get":                perm(authz.JobsRead),
	"import-tasks-timeline":           perm(authz.JobsRead),
	"import-tasks-history":            perm(authz.JobsRead),

	// ----- jobs.manage (incl. acquisition-operator surface, see header) -----
	"download-jobs-reimport":              perm(authz.JobsManage),
	"download-jobs-retry":                 perm(authz.JobsManage),
	"download-jobs-cancel":                perm(authz.JobsManage),
	"import-tasks-reimport":               perm(authz.JobsManage),
	"import-tasks-cancel":                 perm(authz.JobsManage),
	"download-candidates-list-movie":      perm(authz.JobsManage),
	"download-candidates-list-series":     perm(authz.JobsManage),
	"download-candidates-preview-movie":   perm(authz.JobsManage),
	"download-candidates-preview-series":  perm(authz.JobsManage),
	"download-candidates-download-movie":  perm(authz.JobsManage),
	"download-candidates-download-series": perm(authz.JobsManage),
	"proposals-approve":                   perm(authz.JobsManage),
	"proposals-decline":                   perm(authz.JobsManage),
	"tracking-set-autonomy":               perm(authz.JobsManage),
	"tracking-retry":                      perm(authz.JobsManage),
	"wants-cancel":                        perm(authz.JobsManage),

	// ----- admin.settings.read -----
	"settings-list":               perm(authz.AdminSettingsRead),
	"quality-list-profiles":       perm(authz.AdminSettingsRead),
	"quality-get-profile":         perm(authz.AdminSettingsRead),
	"quality-list-custom-formats": perm(authz.AdminSettingsRead),
	"quality-get-custom-format":   perm(authz.AdminSettingsRead),
	"quality-list-tiers":          perm(authz.AdminSettingsRead),
	"quality-list-bins":           perm(authz.AdminSettingsRead),
	"quality-list-size-defaults":  perm(authz.AdminSettingsRead),
	"routing-list-rules":          perm(authz.AdminSettingsRead),
	"routing-get-rule":            perm(authz.AdminSettingsRead),
	"routing-get-fields":          perm(authz.AdminSettingsRead),
	"name-templates-list":         perm(authz.AdminSettingsRead),
	"name-templates-get":          perm(authz.AdminSettingsRead),
	"name-templates-get-default":  perm(authz.AdminSettingsRead),
	"email-provider-get":          perm(authz.AdminSettingsRead),

	// ----- admin.settings.write -----
	"settings-patch":               perm(authz.AdminSettingsWrite),
	"quality-create-profile":       perm(authz.AdminSettingsWrite),
	"quality-update-profile":       perm(authz.AdminSettingsWrite),
	"quality-delete-profile":       perm(authz.AdminSettingsWrite),
	"quality-test-profile":         perm(authz.AdminSettingsWrite),
	"quality-create-custom-format": perm(authz.AdminSettingsWrite),
	"quality-update-custom-format": perm(authz.AdminSettingsWrite),
	"quality-delete-custom-format": perm(authz.AdminSettingsWrite),
	"quality-bind-tier":            perm(authz.AdminSettingsWrite),
	"routing-create-rule":          perm(authz.AdminSettingsWrite),
	"routing-update-rule":          perm(authz.AdminSettingsWrite),
	"routing-delete-rule":          perm(authz.AdminSettingsWrite),
	"routing-reorder-rules":        perm(authz.AdminSettingsWrite),
	"routing-evaluate":             perm(authz.AdminSettingsWrite),
	"name-templates-create":        perm(authz.AdminSettingsWrite),
	"name-templates-update":        perm(authz.AdminSettingsWrite),
	"name-templates-delete":        perm(authz.AdminSettingsWrite),
	"email-provider-save":          perm(authz.AdminSettingsWrite),
	"email-provider-test":          perm(authz.AdminSettingsWrite),
	"email-provider-test-config":   perm(authz.AdminSettingsWrite),

	// ----- admin.indexers.manage -----
	"indexers-list-configured": perm(authz.AdminIndexersManage),
	"indexers-get":             perm(authz.AdminIndexersManage),
	"indexers-get-schema":      perm(authz.AdminIndexersManage),
	"indexers-save":            perm(authz.AdminIndexersManage),
	"indexers-delete":          perm(authz.AdminIndexersManage),
	"indexers-toggle":          perm(authz.AdminIndexersManage),
	"indexers-action":          perm(authz.AdminIndexersManage),
	"indexers-test-all":        perm(authz.AdminIndexersManage),
	"indexers-test-saved":      perm(authz.AdminIndexersManage),
	"indexers-test-unsaved":    perm(authz.AdminIndexersManage),

	// ----- admin.downloaders.manage -----
	"downloaders-list":        perm(authz.AdminDownloadersManage),
	"downloaders-get":         perm(authz.AdminDownloadersManage),
	"downloaders-get-default": perm(authz.AdminDownloadersManage),
	"downloaders-create":      perm(authz.AdminDownloadersManage),
	"downloaders-update":      perm(authz.AdminDownloadersManage),
	"downloaders-delete":      perm(authz.AdminDownloadersManage),
	"downloaders-test":        perm(authz.AdminDownloadersManage),
	"downloaders-test-config": perm(authz.AdminDownloadersManage),

	// ----- AnyOf floor + service refinement -----
	// requests read = own/any: the gate passes a holder of either key; the
	// service filters (List) or checks ownership (Get).
	"requests-list": anyOf(authz.RequestViewOwn, authz.RequestViewAny),
	"requests-get":  anyOf(authz.RequestViewOwn, authz.RequestViewAny),
	// requests-create floor: holding ANY create key clears the gate; the service
	// enforces the exact requests.create:<type>:<tier> from the parsed body,
	// which the middleware can't see (it runs before body parse).
	"requests-create": anyOf(
		authz.RequestCreate(model.MediaTypeMovie, "HD"),
		authz.RequestCreate(model.MediaTypeMovie, "4K"),
		authz.RequestCreate(model.MediaTypeSeries, "HD"),
		authz.RequestCreate(model.MediaTypeSeries, "4K"),
	),
	// requests-approve/deny floor: holding the key for EITHER media type clears
	// the gate; the service refines to the exact approve/deny:<type> once the
	// pending row (and its type) is loaded.
	"requests-approve": anyOf(
		authz.RequestApprove(model.MediaTypeMovie),
		authz.RequestApprove(model.MediaTypeSeries),
	),
	"requests-deny": anyOf(
		authz.RequestDeny(model.MediaTypeMovie),
		authz.RequestDeny(model.MediaTypeSeries),
	),
	// requests-cancel = own/any: the service computes ownership from the request's
	// requested_by.
	"requests-cancel": anyOf(authz.RequestCancelOwn, authz.RequestCancelAny),
	// tracking-cancel = own/any: the service computes ownership from the
	// tracking's requester set.
	"tracking-cancel": anyOf(authz.TrackingCancelOwn, authz.TrackingCancelAny),
}
