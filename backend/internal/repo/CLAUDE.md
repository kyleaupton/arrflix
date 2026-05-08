# Repo layer rules

This package is the boundary between SQLC-generated database code and the rest of the backend. Errors crossing this boundary MUST be typed via `internal/errors`.

**Read [`specs/errors/README.md`](../../../specs/errors/README.md) before adding error-producing methods.** This file is the layer-specific cheat sheet; the spec has the full rationale.

## Rules

1. **Every method that wraps a SQLC query routes the result through `apperrors.FromPg`.**
   - Pass the entity name and the ID the method received as the format string.
   - Never return a naked `pgx` error from this layer.

2. **Never use `errors.New(...)` or `fmt.Errorf(...)` in this package.** Repo errors come from the database; if you have a non-DB error to surface, that's a sign the logic belongs in the service layer.

3. **The format string IS user-facing.** It becomes the `detail` field on the wire (for 4xx kinds). Don't include internal state, query fragments, or anything sensitive.

4. **The repo doesn't know about HTTP status codes.** Don't import `net/http` here. `FromPg` picks the kind; the kind picks the status; that mapping lives in the errors package.

## Pattern

```go
import (
    apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

func (r *Repository) GetLibrary(ctx context.Context, id pgtype.UUID) (dbgen.Library, error) {
    lib, err := r.Q.GetLibrary(ctx, id)
    return lib, apperrors.FromPg(err, "library %s not found", id)
}

func (r *Repository) CreateLibrary(ctx context.Context, name, typ, rootPath string, ...) (dbgen.Library, error) {
    lib, err := r.Q.CreateLibrary(ctx, dbgen.CreateLibraryParams{...})
    return lib, apperrors.FromPg(err, "create library %q", name)
}
```

`FromPg` is nil-safe: if the SQLC call returns `nil`, `FromPg` returns `nil`. No `if err != nil` boilerplate.

## Format strings

The `FromPg` format string becomes the wire `detail` for 4xx kinds (NotFound, Conflict, Validation). Pick the verb based on what's being interpolated:

- **`%q` for arbitrary strings** — paths, names, slugs, external identifiers, IdP subjects, titles, search keys. The quotes make whitespace and empty strings visible and prevent the value from blending into surrounding prose.
- **`%s` for UUIDs and enum-shaped values** — `pgtype.UUID`, kinds, types, protocols, statuses. These are already opaque tokens; quoting adds noise.
- **`%d` for numeric IDs** — TMDB IDs, indexer IDs, season/episode numbers.

Good:

```go
return lib, apperrors.FromPg(err, "library %s not found", id)              // UUID → %s
return inv, apperrors.FromPg(err, "invite for %q not found", email)        // arbitrary string → %q
return ident, apperrors.FromPg(err, "identity %s/%s not found",            // enum + arbitrary
    provider, subject)                                                     // ↑ enum %s, but subject is arbitrary — see note
return mf, apperrors.FromPg(err, "media file at %q in library %s ...",     // path %q, UUID %s
    path, libraryID)
return item, apperrors.FromPg(err, "media item with tmdb id %d ...", tmdbID) // numeric → %d
```

Bad:

```go
return lib, apperrors.FromPg(err, "library %q not found", id)              // pgtype.UUID isn't arbitrary text
return inv, apperrors.FromPg(err, "invite for %s not found", email)        // bare email blends into prose
```

Note on IdP subjects: although `subject` is an external token, it's still arbitrary user-controlled text and should be `%q`. See `repo/auth.go::GetIdentityByProviderSubject`.

## Create methods

Create methods always include a natural identifier in the format string. A bare `"create X"` makes "X already exists" conflicts impossible to localize without reading logs. Pick the most distinguishing field the method received:

- **Named entities** (libraries, downloaders, name templates, policies) → use the name: `"create library %q"`, `"create downloader %q"`.
- **Relational rows** (download job events, import task events, rules tied to a policy) → use the parent ID with a label: `"create download job %s event"`, `"create rule for policy %s"`.
- **Tasks/jobs** without an obvious name → use the most distinguishing attribute the row carries (media item ID, indexer+guid, etc.): `"create download job for media %s"`, not `"create download job"`.

Good:

```go
return lib, apperrors.FromPg(err, "create library %q", name)
return ev, apperrors.FromPg(err, "create download job %s event", arg.DownloadJobID)
return rule, apperrors.FromPg(err, "create rule for policy %s", policyID)
```

Bad:

```go
return job, apperrors.FromPg(err, "create download job")        // no identifier — which one collided?
return task, apperrors.FromPg(err, "create import task")        // ditto
```

## What `FromPg` translates

- `pgx.ErrNoRows` → `KindNotFound` (404)
- SQLSTATE 23505 unique violation → `KindConflict` (409)
- SQLSTATE 23503 foreign-key violation → `KindConflict` (409)
- SQLSTATE 23502 not-null violation → `KindValidation` (422)
- SQLSTATE 23514 check violation → `KindValidation` (422)
- anything else → wrapped without a kind (surfaces as 500, message hidden from the wire)

If a postgres error code matters and isn't in this list, extend `internal/errors/adapters.go` rather than special-casing at the callsite.

## Identifier choice

The repo only knows the identifier the method received. If callers consistently pass the user-facing identifier (UUID from a URL like `/libraries/:id`), the repo's error message is already correct.

If the user is talking to your method via a different identifier (a slug, a name, a composite key), the **service layer** does the override via `apperrors.Wrap`. Don't try to handle that here — see `internal/service/CLAUDE.md`.
