---
paths:
  - "backend/internal/http/handlers/**"
---

# HTTP handlers — humachi pattern

`libraries.go` is the canonical example. Every handler in this directory follows the same shape — read it alongside this guide.

## File layout

One file per entity, sectioned by operation. **Do not split into a sibling `_dto.go` file.** Sections are delimited by `// ----- <Op> -----` and ordered as:

1. Handler struct + constructor.
2. Shared body shape (only when ≥2 operations share the same body — see below).
3. Per-operation sections — each section contains the Input struct, the Output struct, and the handler method, in that order.
   - Order: `List`, `Get`, `Create`, `Update`, `Delete`, then any bespoke verbs (`Scan`, `GetDefault`, etc.).
4. `RegisterHumachi(api huma.API)` at the bottom.

A skeleton:

```go
// ----- Handler -----

type Libraries struct{ svc *service.Services }

func NewLibraries(s *service.Services) *Libraries { return &Libraries{svc: s} }

// ----- Shared body shape -----

type libraryWriteBody struct { ... }

// ----- List -----

type LibrariesListInput struct{}
type LibrariesListOutput struct{ Body []model.Library }
func (h *Libraries) List(ctx context.Context, _ *LibrariesListInput) (*LibrariesListOutput, error) { ... }

// ----- Get -----
// ...

// ----- Register -----

func (h *Libraries) RegisterHumachi(api huma.API) { ... }
```

Input is `<Op>Input`, Output is `<Op>Output`. Output is always declared even when empty so the handler signature stays uniform (`func(ctx, *Input) (*Output, error)`).

## Shared body factoring

When Create and Update take the same body, factor it to a package-private `<entity>WriteBody` struct in the "Shared body shape" section near the top. Embed it as `Body <entity>WriteBody` on each Input. Huma names the schema after the Go type, with the first letter uppercased — so `libraryWriteBody` becomes `LibraryWriteBody` in the OpenAPI components.

When an operation has a unique body, declare it inline as the `Body` field's type in the Input. Don't invent a one-shot `<Op>Body` named struct.

## Operation registration

`RegisterHumachi` lives at the bottom of the file and is called from both `internal/http/http.go` (live server) and `cmd/genspec/main.go` (spec generator).

```go
func (h *Libraries) RegisterHumachi(api huma.API) {
    huma.Register(api, huma.Operation{
        OperationID:   "libraries-create",
        Method:        http.MethodPost,
        Path:          "/api/v1/libraries",
        Summary:       "Create library",
        Tags:          []string{"libraries"},
        DefaultStatus: http.StatusCreated,
    }, h.Create)
    // ... one Register per route
}
```

`OperationID` convention: `<resource>-<verb>` in kebab-case (`libraries-list`, `libraries-scan`, `media-search`). It's the stable identifier the TypeScript client uses for function names — don't change it casually.

`Tags` is the OpenAPI grouping (lower-case, plural where natural — `libraries`, `download-jobs`, `users`).

Set `DefaultStatus` whenever it isn't 200 (huma's default for body-bearing outputs) or 204 (its default for empty-body outputs): `201 Created`, `202 Accepted`, etc. Do not use a dynamic `Status` field on the output unless the operation truly returns variable codes.

`Description` is an optional longer text. Use it for non-obvious behavior (async kickoff, conflict semantics). Skip it when `Summary` already says everything.

## Handler signatures

Always `func(ctx context.Context, input *<Op>Input) (*<Op>Output, error)`.

```go
func (h *Libraries) Get(ctx context.Context, input *LibrariesGetInput) (*LibrariesGetOutput, error) {
    lib, err := h.svc.Libraries.Get(ctx, input.ID)
    if err != nil {
        return nil, err
    }
    return &LibrariesGetOutput{Body: lib}, nil
}
```

The handler does three things:

1. Reads parsed inputs from the Input struct (path / query / header / body).
2. Calls a service method, passing typed values (`uuid.UUID`, `model.*`).
3. Wraps the result in `&<Op>Output{Body: ...}`. On error, returns the service error directly — no manual rendering.

When the input ↔ service args translation is more than a couple of lines, factor a small helper.

## UUID path params

Declare path id fields as `uuid.UUID` directly:

```go
type LibrariesGetInput struct {
    ID uuid.UUID `path:"id" format:"uuid" doc:"Library ID"`
}
```

Huma routes path params through `encoding.TextUnmarshaler` when the target type implements it, and `*uuid.UUID` does. On a malformed value huma emits a 422 RFC-9457 response with the bad value at `path.id` — same shape as tag-level validation errors. **Do not write a Resolve method for this.** No `parsedID` field, no `uuid.Parse` call.

The `format:"uuid"` tag is still required: it sets the OpenAPI `format: uuid` annotation on the path-param schema, which the TypeScript client uses for its types.

## Validation split

Three layers; each does the validation it's best at.

| Layer | What goes here | How |
| --- | --- | --- |
| **Tags** | Per-field rules: required, length, enum, format, pattern, numeric bounds | Struct tags on Input fields |
| **Resolve()** | Cross-field rules that don't fit a tag and don't touch state | `Resolve(ctx huma.Context) []error` on the Input |
| **Service** | Stateful rules: filesystem, uniqueness, foreign-key existence, business invariants | `apperrors.Validation(...)` returned from the service method |

**Tag-level — examples from libraries:**

```go
Name     string `json:"name" required:"true" minLength:"1" maxLength:"100"`
Type     string `json:"type" required:"true" enum:"movie,series"`
RootPath string `json:"rootPath" required:"true" minLength:"1"`
```

**Resolve() — only when a tag won't do it:**

Reach for Resolve when you have a cross-field rule like "exactly one of `a` or `b` must be set" that no struct tag can express. Emit `apperrors.Validation(detail, apperrors.Field("body.<fieldName>", msg))` so the wire shape matches a tag-emitted or service-emitted validation error. There's no need to declare a `var _ huma.Resolver = ...` assertion — the registration call surfaces type mismatches.

**Service — stateful checks:**

The libraries service's `os.Stat(rootPath)` check is the canonical example. It stays in the service because:

1. The handler binding path shouldn't read the filesystem.
2. The same check belongs on any code path that creates a library (CLI, future bulk import, etc.).

The error it produces (`apperrors.Validation` with `apperrors.Field("body.rootPath", ...)`) flows through to the wire with the same shape as a tag-level rejection.

## Output rule

Default to wrapping the service's return type:

- Single resource → `Body model.<Entity>`.
- Paginated list → `Body model.Page[model.<Entity>]`.
- Flat list (no pagination) → `Body []model.<Entity>`.

Define a bespoke output type only when the API shape genuinely differs from any model — e.g. `ScanResponse` for the scan-kickoff endpoint, where the wire is just `{ scanId: string }` and there's no domain model for "a kicked-off scan".

When the operation has no body (204 No Content), declare an empty output struct anyway:

```go
type LibrariesDeleteOutput struct{}
```

The handler returns `&LibrariesDeleteOutput{}` on success; huma writes no body because there are no body-bearing fields. This keeps the signature uniform.

## Auth claims

JWT and setup-mode middleware run at the chi layer (see `internal/http/http.go`), so every route registered on humachi is implicitly protected. The handler only needs to read claims when it actually uses them. Public routes (login, signup, bootstrap, etc.) bypass the JWT middleware via the `publicPathSet` allowlist in `middlewares/chi.go`; adding a new public route requires updating that allowlist.

```go
import "github.com/kyleaupton/arrflix/internal/http/middlewares"

claims, ok := middlewares.ClaimsFromContext(ctx)
if !ok {
    return nil, apperrors.Unauthenticatedf("missing credentials").Op("LibrariesHandler.X")
}
```

## Error rule

Service code already produces typed `*apperrors.Error` values. The handler just returns them. `internal/http/humaerr` overrides `huma.NewError` so any error in the chain that matches `*apperrors.Error` renders as RFC 9457 problem-details with the typed kind, op, and field details preserved.

What this means concretely:

- **Don't construct typed errors in the handler.** The service builds them; the handler hands them back.
- For the very rare handler-level invariant (e.g. "this code path is unreachable"), use the typed constructor with `.Op("<Resource>Handler.<Method>")`.

Tag-level validation, path-param binding, and Resolve-emitted errors are all aggregated by huma into a single 422 response. The wire shape for these binding-time errors is huma's default `{status, title, errors: []string}` — the `path.id` / `body.<field>` location appears in the stringified message. Service-emitted `apperrors.Validation` produces the richer `ProblemDetails` shape (`{type, title, status, detail, errors: []FieldError}`). The two shapes are not byte-identical, but both communicate the same information.

## Operation `Errors` enumeration

Per-op `Errors` should only enumerate **semantically meaningful** statuses — codes the frontend actually branches on. Universal statuses are dropped: 401 lives in the auth middleware (outside huma's view) and 500 is universal across every operation. Both are handled globally by the FE's interceptor + error boundary, not per-operation.

Use the predefined sets in `errors.go` rather than inline literals:

| Set | Codes | Use for |
| --- | --- | --- |
| `errsRead` | `404` | GET-by-id, and any op whose only meaningful error is "row gone" |
| `errsWrite` | `400, 409, 422` | Create/POST endpoints (no path id; body validation + uniqueness) |
| `errsUpsert` | `400, 404, 409, 422` | Update/PUT/PATCH addressed by id — read-shape + write-shape |
| `errsDelete` | `404, 409` | Delete-by-id where 409 covers FK violations |
| `errsUpstream` | `502` | Compose with another set when the op talks to TMDB / Prowlarr / qBittorrent |
| `errsForbidden` | `403` | Compose when the op has an explicit permission check beyond auth |

Compose with `errs(...)` when an op combines concerns:

```go
Errors: errs(errsRead, errsUpstream),    // refresh-by-id endpoint
Errors: errs(errsWrite, errsUpstream),   // test-from-config endpoint
Errors: errs(errsUpsert, errsForbidden), // permission-gated update
```

**Escape hatch — literal slice with comment.** Use this when an op has a genuinely one-off shape. The canonical case is a password-change endpoint that takes a current-password and emits 401 from the handler for "wrong current password" — that 401 IS semantically meaningful (it's not the middleware's), so it stays enumerated:

```go
// 401 here is handler-emitted ("wrong current password"), not middleware.
Errors: []int{http.StatusUnauthorized, http.StatusBadRequest, http.StatusUnprocessableEntity},
```

**Omit `Errors` entirely** when the op has no semantic error surface — list endpoints, simple GETs that can only fail with 500, etc. Huma emits a single `default` response, which correctly says "no per-op error branches beyond universals."

## Adding a new handler

1. Create `<entity>.go` in this directory following the layout above.
2. Add `RegisterHumachi(api)` at the bottom.
3. Wire it into `internal/http/http.go::NewServer` and `cmd/genspec/main.go::main`.
4. Run `go run ./cmd/genspec` to regenerate the OpenAPI spec, then regenerate the TS client (`npm run openapi-ts` from `web/`) — or use the `arrflix_gen_api` MCP tool which does both.
