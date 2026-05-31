package model

// Pagination contains metadata for paginated responses
type Pagination struct {
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	PageSize   int   `json:"pageSize"`
	TotalPages int   `json:"totalPages"`
}

// Page is the generic envelope for paginated list responses. The wire shape
// matches the prior PaginatedLibraryResponse: a `data` array plus a
// `pagination` block, so existing clients continue to work unchanged.
type Page[T any] struct {
	Data       []T        `json:"data"`
	Pagination Pagination `json:"pagination"`
}

// PaginatedLibraryResponse is the concrete instantiation used by the library
// listing endpoint. Kept as a named alias so swag picks up a stable schema
// name (Go generics produce mangled names like Page-model_LibraryItem in the
// OpenAPI spec, which would change every client type name on regeneration).
type PaginatedLibraryResponse = Page[LibraryItem]

// InboxItem is one row of the matcher inbox: a file whose current
// (non-superseded) match_decision has an outcome other than confident /
// detached. Title/Year/Type are the display fields, COALESCEd from the
// identified media_item (when one is set) over the decision's parsed
// snapshot, so a flagged-but-identified file (confident_review,
// partial_series) renders its real title and an unidentified one
// (low_confidence, ambiguous, no_match) renders what the parser saw.
type InboxItem struct {
	ID            string  `json:"id"`
	LibraryID     string  `json:"libraryId"`
	Path          string  `json:"path"`
	FileSize      *int64  `json:"fileSize,omitempty"`
	DiscoveredAt  string  `json:"discoveredAt"`
	Outcome       string  `json:"outcome"`
	Confidence    float64 `json:"confidence"`
	Title         string  `json:"title,omitempty"`
	Year          *int    `json:"year,omitempty"`
	Type          string  `json:"type,omitempty"`
	PartialSeries bool    `json:"partialSeries,omitempty"`
}

// InboxPage is the matcher-inbox list envelope: the same {data, pagination}
// shape the library endpoint uses, plus countsByOutcome — the per-band
// totals (group headers + the dashboard badge). It's a concrete struct
// rather than Page[InboxItem] so the extra field fits and the OpenAPI
// schema name stays stable (generics mangle to Page-model_InboxItem).
type InboxPage struct {
	Data            []InboxItem      `json:"data"`
	Pagination      Pagination       `json:"pagination"`
	CountsByOutcome map[string]int64 `json:"countsByOutcome"`
}

// LibraryItem is the enriched media item returned by the library endpoint
type LibraryItem struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	Year       *int32 `json:"year,omitempty"`
	TmdbID     *int64 `json:"tmdbId,omitempty"`
	PosterPath string `json:"posterPath,omitempty"`
	CreatedAt  string `json:"createdAt"`
}
