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

