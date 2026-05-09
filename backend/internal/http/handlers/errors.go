package handlers

import "net/http"

// Standard error-status sets for huma.Operation.Errors.
//
// Universal statuses are intentionally omitted: 401 (auth middleware emits
// it outside huma's view) and 500 (universal across all operations). Both
// are handled globally on the frontend, not branched per-operation.
//
// Use the bare set when an operation's error surface matches one shape.
// Compose with errs(...) when an operation combines concerns (e.g., a DB
// read that also hits an upstream API).
var (
	// errsRead applies to GET-by-id endpoints.
	errsRead = []int{http.StatusNotFound}

	// errsWrite applies to POST/PUT/PATCH endpoints that don't address an
	// existing row by ID (e.g., create).
	errsWrite = []int{
		http.StatusBadRequest,
		http.StatusConflict,
		http.StatusUnprocessableEntity,
	}

	// errsUpsert applies to update/upsert endpoints addressed by ID — combines
	// errsRead's "row may not exist" with errsWrite's validation/conflict surface.
	errsUpsert = []int{
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusUnprocessableEntity,
	}

	// errsDelete applies to delete-by-id. 409 covers FK violations.
	errsDelete = []int{
		http.StatusNotFound,
		http.StatusConflict,
	}

	// errsUpstream applies to operations that talk to external services
	// (TMDB, Prowlarr, qBittorrent). Compose with another set:
	//   Errors: errs(errsRead, errsUpstream)
	errsUpstream = []int{http.StatusBadGateway}

	// errsForbidden applies to operations behind an explicit permission check
	// beyond authentication. Compose: errs(errsUpsert, errsForbidden).
	errsForbidden = []int{http.StatusForbidden}
)

// errs composes multiple status sets into one, deduplicating along the way.
// Use for operations that combine concerns — e.g., errs(errsRead, errsUpstream)
// for a refresh endpoint that hits TMDB.
func errs(sets ...[]int) []int {
	seen := make(map[int]struct{})
	var out []int
	for _, s := range sets {
		for _, code := range s {
			if _, dup := seen[code]; dup {
				continue
			}
			seen[code] = struct{}{}
			out = append(out, code)
		}
	}
	return out
}
