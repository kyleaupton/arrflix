package resolvers

import (
	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/metadata"
)

// DefaultRegistry builds the v1 matcher resolver catalog: PathEmbed
// (Tier 1) and NameParse (Tier 3). It is the single source of truth for
// "what resolvers ship in v1"; service.New consumes it.
//
// Lives in `resolvers` (rather than `matcher`) because the matcher
// package can't import its own resolver implementations without an
// import cycle (resolvers depends on matcher's types). The helper is
// here so the construction site stays one line.
//
// Future resolvers (osdb-hash, embedded-tags, episode-numbering)
// register here when they ship; the per-library toggle (v2) filters
// the registered set rather than rebuilding it.
func DefaultRegistry(provider metadata.MetadataProvider) *matcher.Registry {
	r := matcher.NewRegistry()
	r.Register(NewPathEmbed())
	r.Register(NewNameParse(provider))
	return r
}
