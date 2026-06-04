---
paths:
  - "backend/internal/repo/**"
---

# Repo layer rules

This package is the boundary between SQLC-generated database code and the rest of the backend. Errors crossing this boundary MUST be typed via `internal/errors`.

**Read [`specs/patterns/errors/README.md`](../../specs/patterns/errors/README.md) before adding error-producing methods.** This file is the layer-specific cheat sheet; the spec has the full rationale.

## Rules

1. **Every method that wraps a SQLC query routes the result through `apperrors.FromPg`.**
   - Pass the entity name and the ID the method received as the format string.
   - Never return a naked `pgx` error from this layer.

2. **Never use `errors.New(...)` or `fmt.Errorf(...)` in this package.** Repo errors come from the database; if you have a non-DB error to surface, that's a sign the logic belongs in the service layer.

3. **The format string IS user-facing.** It becomes the `detail` field on the wire (for 4xx kinds). Don't include internal state, query fragments, or anything sensitive.

4. **The repo doesn't know about HTTP status codes.** Don't import `net/http` here. `FromPg` picks the kind; the kind picks the status; that mapping lives in the errors package.

5. **Repo methods return `model.*`, never `dbgen.*`.** `dbgen.*` is a persistence-shaped type and stays strictly inside this package and `internal/db/`. Each repo file owns the translation from `dbgen.Foo` to `model.Foo` (see [Domain types](#domain-types) below). Service signatures take and return `model.*`; pgtype-shaped parameters (`pgtype.UUID`) become idiomatic Go (`uuid.UUID`) at the repo boundary too.

## Domain types

The repo is the single point of translation between persistence shapes (`dbgen.*`, `pgtype.*`) and domain shapes (`model.*`, `uuid.UUID`, `time.Time`, `*string`). Services and workers see only domain shapes.

Each entity gets a small translator helper, kept next to the methods that use it:

```go
// repo/libraries.go

func toModelLibrary(row dbgen.Library) model.Library {
    return model.Library{
        ID:        uuidFromPgtype(row.ID),
        Name:      row.Name,
        Type:      row.Type,
        RootPath:  row.RootPath,
        CreatedAt: row.CreatedAt.Time,
        UpdatedAt: row.UpdatedAt.Time,
    }
}

func (r *Repository) GetLibrary(ctx context.Context, id uuid.UUID) (model.Library, error) {
    row, err := r.Q.GetLibrary(ctx, pgtypeFromUUID(id))
    if err != nil {
        return model.Library{}, apperrors.FromPg(err, "library %s not found", id)
    }
    return toModelLibrary(row), nil
}
```

Conventions:

- **Translator names**: `toModel<Entity>` for `dbgen → model`. For composite shapes (a row joined with derived data), use `toModel<Entity>WithSummary` etc., matching the model type name.
- **Error path returns the zero value** of the model type, not a `dbgen` zero. Don't return `dbgen.Library{}` from a function whose signature is `(model.Library, error)` — that's a different type, and a refactoring footgun.
- **Composite types compose at repo time.** `model.Policy` (rich type with `Condition` and `Actions`) is built by `GetPolicy` from the flat policy row plus the parsed Condition JSON plus the actions array. The composition lives in the repo.
- **Reverse direction (writes)** doesn't need a translator — services pass scalar arguments to repo methods, and the repo builds the `dbgen.*Params` struct internally. Don't define `fromModelLibrary` unless something genuinely needs it.

UUID conversion helpers (`uuidFromPgtype`, `pgtypeFromUUID`) live in this package — define once, reuse everywhere. Same for any other `pgtype ↔ Go` adapters.

## Write method inputs

Read methods return `model.*` types (covered above). Write methods (Create, Update, Upsert, etc.) take input the caller wants to persist — and that input shape often differs from the entity's stored shape (server-managed fields like `ID`, `CreatedAt`, `UpdatedAt` are excluded).

The convention:

- **Trivial writes — 1-2 scalars** (e.g. `DeleteLibrary(id uuid.UUID)`, `SetSystemInitialized(ctx)`): take the scalars directly. No struct.
- **Multi-field writes** (3+ scalar fields, or any field that's a struct / slice / map): define a bespoke `<MethodName>Params` struct next to the repo method.

```go
// UpsertIdentityParams is the domain-shaped input for UpsertIdentity. Mirrors
// the writeable subset of model.Identity (omits server-managed ID and CreatedAt).
type UpsertIdentityParams struct {
    UserID         uuid.UUID
    Provider       model.AuthProvider
    Subject        string
    Username       *string
    AccessToken    *string
    RefreshToken   *string
    TokenExpiresAt *time.Time
    Raw            json.RawMessage
}

func (r *Repository) UpsertIdentity(ctx context.Context, params UpsertIdentityParams) (model.Identity, error)
```

Why a bespoke param struct rather than reusing `model.<Entity>`:

- The model type represents what's *stored*. The param struct represents what a caller is *allowed to send*. They're often shaped similarly but they're different concepts — same distinction as the API layer's `LibrariesCreateInput` vs `model.Library`.
- Reusing the model type for input means server-managed fields (`ID`, `CreatedAt`) get silently ignored. That's a footgun for future callers who don't know.
- Bespoke param structs are explicit: every field in the struct is a field the repo accepts, full stop.

**Service files will import `repo.<MethodName>Params`** to construct the call. That's expected — the param struct is part of the repo's public contract, just like a function's signature. It's not an encapsulation violation; it's the dependency direction working as designed.

The struct lives in the same `repo/<entity>.go` file as the method that consumes it. Use a one-line doc comment pointing to the related model type so the relationship is obvious:

```go
// CreateLibraryParams is the domain-shaped input for CreateLibrary. Mirrors
// the writeable subset of model.Library.
```

## Pattern

For methods that **don't return a row** (deletes, updates that report only success/failure), `FromPg`-as-the-direct-return still works because there's no translation step:

```go
func (r *Repository) DeleteLibrary(ctx context.Context, id uuid.UUID) error {
    return apperrors.FromPg(r.Q.DeleteLibrary(ctx, pgtypeFromUUID(id)),
        "delete library %s", id)
}
```

For methods that **return a row** (gets, lists, creates with `RETURNING *`, etc.), the translation step requires an explicit error check — see [Domain types](#domain-types) above for the canonical shape.

`FromPg` itself is nil-safe: if the SQLC call returns `nil`, `FromPg` returns `nil`. The single-line return shape above is just a convenience for no-row methods.

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

If the user is talking to your method via a different identifier (a slug, a name, a composite key), the **service layer** does the override via `apperrors.Wrap`. Don't try to handle that here — see the service-layer rule (`.claude/rules/backend-service.md`).
