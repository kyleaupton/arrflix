package authz

import (
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
)

// RequestSettingsKeys is every key the per-user request settings own as
// user-subject grants: 4K create plus auto-approve at every tier, for both media
// types. Saving the settings replaces exactly this set; user-subject grants on
// any other key are left alone.
func RequestSettingsKeys() []string {
	out := make([]string, 0, len(allMediaTypes)*(1+len(qualityprofile.AllTiers)))
	for _, t := range allMediaTypes {
		out = append(out, RequestCreate(t, string(qualityprofile.Tier4K)))
		for _, tier := range qualityprofile.AllTiers {
			out = append(out, RequestAutoApprove(t, string(tier)))
		}
	}
	return out
}

// RequestSettingsGrants derives the user-subject grants for a user's request
// settings. HD create is not among them: it comes from the requester role.
// Auto-approve covers every tier the user may request, so it reaches 4K only
// while 4K is allowed — turning 4K off drops its auto-approve with it, and
// turning it back on restores it.
func RequestSettingsGrants(s model.RequestSettings) []string {
	var out []string
	for _, t := range allMediaTypes {
		if s.Allow4k {
			out = append(out, RequestCreate(t, string(qualityprofile.Tier4K)))
		}
		if s.AutoApprove {
			out = append(out, RequestAutoApprove(t, string(qualityprofile.TierHD)))
			if s.Allow4k {
				out = append(out, RequestAutoApprove(t, string(qualityprofile.Tier4K)))
			}
		}
	}
	return out
}
