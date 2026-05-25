# Name templates — turning metadata into paths

**Status:** Draft, iteration 1

A name template is a string that renders against a media file's metadata to produce its on-disk path (folder names + filename). It is one of the three things [routing](../routing/README.md) dispatches to (alongside libraries and downloaders) and the substrate the [import worker](../acquisition/README.md) uses to lay hardlinked files into a library at the right place with the right name.

The model is implemented and stable: Go `text/template` over a typed evaluation context, with per-type entities (movie + series), three template components per series template (show folder / season folder / file), and a small set of custom render funcs. This spec captures what exists and locks in the v1 additions: **create-time syntax validation**, a **preview/dry-run endpoint**, and a clearer story on **missing-variable handling**.

## TL;DR

- A name template is a `(type, template, optional folder templates, default flag)` row. Each renders to a single path component; the import worker joins them via `filepath.Join`.
- **Two types**: `movie` and `series`. Movies have a folder template + file template. Series have a show-folder + season-folder + file template.
- **DSL is Go `text/template`** with two custom funcs: `sanitize` (strip filesystem-illegal chars) and `clean` (drop `"unknown"` values, then sanitize).
- **Variables come from a unified `EvaluationContext`** with five namespaces: `Candidate` (the release), `Quality` (the quality bin — **asserted-reconciled at render time**), `Release` (group, edition), `Media` (TMDB), `MediaInfo` (post-download `ffprobe` analysis).
- **Name from truth, not claims.** `ffprobe`-verifiable tags (codec/audio/HDR/resolution) render from `MediaInfo`; non-verifiable ones (source/group/edition) from the parse. The shipped default uses identity + quality bin only — it can't be wrong.
- **Sonarr/Radarr parity** is read-parity (matcher, for trials) + write-parity (token-vocabulary + format-string import; byte-identical output is a harness-tested aspiration). `{{.Quality.Full}}` intentionally reflects asserted truth — identical when the release was honest, truthful when it wasn't.
- Per-type default; routing falls back to the default for the media type when no template is explicitly chosen.
- v1 adds: **syntax validation at create / update**, **preview/dry-run endpoint** against sample data, and a clearer **missing-variable contract** (today: silent empty string; lean for v1: still silent, but lint-warn at save time).
- v1 does **not** add: template versioning, range-based multi-episode syntax, includes/snippets — all deferred to [open questions](#open-questions).

## What a name template owns

A row encodes:

- **Name** — display name, unique case-insensitive.
- **Type** — `movie` or `series`. Drives which template components are required and which variables are guaranteed present.
- **`template`** (required) — the **file** template. For movies, the movie filename. For series, the episode filename. Extension is **not** included; the import worker appends the source extension automatically.
- **`movie_dir_template`** (required when `type='movie'`) — the movie folder name.
- **`series_show_template`** (required by convention when `type='series'`) — the show folder name.
- **`series_season_template`** (required by convention when `type='series'`) — the season folder name.
- **Default flag** — at most one default per type, enforced by a partial unique index.
- **Created / updated timestamps**.

That is the full surface today. No tags, no description, no per-quality variants, no library scoping.

## The DSL

Templates are **Go `text/template`** strings. Variables use dotted-path access with capitalized field names (`{{.Media.Title}}`, `{{.MediaInfo.AudioCodec}}`). Standard Go template control flow is available — `{{if}}`, `{{range}}`, `{{with}}`, pipes — though most real-world templates only use `{{if}}` for optional segments.

### Custom render funcs

| Func       | Behavior                                                                                                                                |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `sanitize` | Replaces filesystem-illegal chars (`/\:*?"<>|`) with `-`, trims whitespace. Use anywhere a value might contain user-supplied text.       |
| `clean`    | If the value is `"unknown"` (case-insensitive), returns empty string; otherwise sanitizes. Use for optional MediaInfo / Release fields. |

### Example templates

From the dev seed data (real templates that work today):

**Movie file:**

```
{{.Media.CleanTitle}} ({{.Media.Year}}){{if .Release.Edition}} {edition-{{.Release.Edition}}}{{end}} [{{.Quality.Full}}]{{if .MediaInfo.AudioCodec}}[{{.MediaInfo.AudioCodec}} {{.MediaInfo.AudioChannels}}]{{end}}{{if .MediaInfo.HDR}}[{{.MediaInfo.HDR}}]{{end}}{{if .MediaInfo.VideoCodec}}[{{.MediaInfo.VideoCodec}}]{{end}}{{if .Release.ReleaseGroup}}-{{.Release.ReleaseGroup}}{{end}}
```

**Movie folder:**

```
{{.Media.CleanTitle}} ({{.Media.Year}}) {tmdb-{{.Media.TmdbID}}}
```

**Series file:**

```
{{.Media.Title}} - S{{.Media.Season}}E{{.Media.Episode}} - {{clean .Media.EpisodeTitle}} [{{.Quality.Full}}]{{if .MediaInfo.AudioCodec}}[{{.MediaInfo.AudioCodec}} {{.MediaInfo.AudioChannels}}]{{end}}{{if .MediaInfo.HDR}}[{{.MediaInfo.HDR}}]{{end}}{{if .MediaInfo.VideoCodec}}[{{.MediaInfo.VideoCodec}}]{{end}}{{if .Release.ReleaseGroup}}-{{.Release.ReleaseGroup}}{{end}}
```

These are intentionally Sonarr/Radarr-shaped — the goal is that someone migrating in can paste their existing naming scheme with minimal translation.

## Variable catalog

Five namespaces, each a typed struct on the unified `EvaluationContext`. The catalog is **the contract** between the rest of the system and template authors. Adding a variable is additive (templates that don't use it are unaffected); removing or renaming one is a breaking change.

The complete field-by-field list lives with the struct definitions in code (`backend/internal/model/context.go`). This spec captures the **shape** and **guarantees** of each namespace; the per-field reference is generated docs, not spec material.

### `Candidate` — the release we picked

Available **pre-download**. Source: the indexer search result. Includes title, size, indexer, indexer ID, categories, protocol (`torrent` / `usenet`), seeders, peers, age (seconds + hours), grabs, publish date, link, GUID.

Use case: occasionally useful for the file template (e.g., embedding the release group in the filename), but most templates draw from `Quality` / `Release` instead, which are parsed *from* the candidate title and are more structured.

### `Quality` — quality bin

Source: the [name-parser](../parsing/README.md) over the candidate title (the **advertised** bin). Fields: `Full` (human-readable string like `"Bluray-2160p Remux"`), `Resolution` (enumerated), `Source` (enumerated), `IsRemux`, `IsRepack`, `Version`.

**At render time the values are asserted-reconciled.** Templates run at import, after `ffprobe`. The [re-gate](../quality-profiles/README.md#import-time-re-gate) reconciles `Quality` against the real file before rendering: **`Resolution` from `MediaInfo`** (verifiable), **`Source` from the parse** (`ffprobe` can't see it). So `{{.Quality.Full}}` writes what the file _is_. The routing engine reads the same namespace _pre-download_ and sees the advertised values — same struct, different phase.

The enumerations are stable; new values are additive. `Resolution` covers `Unknown / SD / 480p / 576p / 720p / 1080p / 1440p / 2160p / 4320p`. `Source` covers the standard set (`SDTV`, `CAM`, `Telesync`, `Telecine`, `Screener`, `DVD`, `DVD-Rip`, `HDTV`, `WEBRip`, `WEB-DL`, `BluRay`, `REMUX`, `Raw-HD`, `Unknown`).

### `Release` — non-quality release metadata

Available **pre-download**. Source: name-parser. Fields: `ReleaseGroup`, `Edition`. Both optional; both renderable with `clean` to drop empty/`unknown`.

### `Media` — TMDB / catalog data

Available **pre-download**. Source: TMDB + media-item record. Fields: `Type` (`movie` / `series`), `Title`, `CleanTitle` (auto-sanitized), `Year`, `TmdbID`.

**Series-only fields:** `Season`, `Episode`, `EpisodeTitle`. Note: `Season` and `Episode` render as **zero-padded 2-digit strings** (`"01"`, `"05"`) — this is so `S{{.Media.Season}}E{{.Media.Episode}}` produces `S01E05`. The underlying model stores them as `*int`; the renderer formats them. Templates that need the integer value (e.g., for arithmetic) have no clean path today.

### `MediaInfo` — file analysis

Available **post-download only**. Source: media-info probe of the actual file. Populated by the import worker before template rendering, so it's always available in the rendering context.

Three sub-groups:

- **Video** — `VideoCodec`, `VideoBitDepth`, `VideoProfile`, `Width`, `Height`, `VideoFps`, `HDR` (enumerated: `None / HDR10 / HDR10+ / Dolby Vision / HLG`), `ScanType` (`Progressive / Interlaced / Unknown`).
- **Audio** — `AudioCodec`, `AudioChannels` (string like `"5.1"`), `AudioProfile`, `AudioStreamCount`, `AudioLanguages` (list).
- **Container / general** — `Container` (`MKV / MP4 / AVI / TS / Unknown`), `Duration`, `FileSize`, `Subtitles` (list).

When the probe didn't produce a value, fields default to their zero values (empty string, zero, empty list). The `clean` func is the conventional way to skip these gracefully — `[{{clean .MediaInfo.HDR}}]` renders nothing when HDR is `None` or `unknown`.

### When variables aren't available

Templates run only at import time, when **all** five namespaces are populated. There is no pre-import rendering path; the routing-time policy engine reads the same context for its own purposes but does not invoke the template renderer.

That means: every template can assume the full context exists. The only "missing" case is **optional fields** within a namespace (e.g., `EpisodeTitle` on a season pack, `HDR` on an SDR file). See [Missing-variable behavior](#missing-variable-behavior).

## Naming from truth, not from claims

A filename should reflect what the file **is**, not what its release **claimed** — otherwise the library accrues self-inflicted entropy (a filename tagged `Atmos`/`DV` that the file doesn't contain, which the matcher and hygiene then re-read as fact). The rule:

- **`ffprobe`-verifiable attributes render from `MediaInfo` (asserted)** — codec, audio, channels, HDR/DV, resolution. Facts about the bytes.
- **Non-verifiable attributes render from `Release`/`Quality` (name-derived)** — release group, edition, and the **source** half of the quality bin. `ffprobe` can't see these, so the parse is the only source; they aren't "lies" (see the [parsing taxonomy](../parsing/README.md#what-ffprobe-can-and-cannot-verify)).
- **`Quality` is asserted-reconciled at render time** — resolution from `MediaInfo`, source from the parse. This is the [re-gate](../quality-profiles/README.md#import-time-re-gate)'s output, not the raw advertised parse.

The seed templates already follow this — `{{.MediaInfo.AudioCodec}}`, `{{.MediaInfo.HDR}}`, `{{.MediaInfo.VideoCodec}}` for granular tags, `{{.Quality.Full}}` for the bin, `{{.Release.ReleaseGroup}}` / `{{.Release.Edition}}` for provenance. The principle just makes it a contract.

**Default-safe, advanced-expressive.** The shipped default template uses **identity + quality bin only** — it can never be wrong because it makes no granular claims. Granular `MediaInfo` tags are opt-in for power users who want `[x265][Atmos]` in filenames. Same three-tier philosophy as [quality profiles](../quality-profiles/README.md): great defaults, advanced surfaces hidden.

## Sonarr / Radarr parity

The goal is that a migrating user can paste their existing naming scheme — but parity splits into two halves with very different weight:

- **Read parity** (we understand Sonarr/Radarr-named libraries) is owned by [matching](../matching/README.md) and matters most for _trials_: nobody lets a new tool rename 5,000 files on day one; they point it at the existing library and watch it identify everything. Templates aren't involved.
- **Write parity** (we render identical paths) is a committed-migration nicety — payoff is "no mass-rename diff when I switch." It's where the work lives.

The committed write-parity surface is **token-vocabulary parity + format-string import** (paste your Sonarr/Radarr format string, get familiar output). **Byte-identical rendered output is a harness-tested aspiration, not a contract** — the real effort is reproducing libmediainfo's field conventions through `ffprobe` (AVC vs h264 vs x264, channel rendering, DV labels), measurable the same way the [parser parity harness](../parsing/README.md#testing-strategy--parity-as-a-ci-gate) measures quality parity.

**One intentional divergence — loud and rare.** Our `{{.Quality.Full}}` reflects **asserted** truth (post-re-gate). When a release was honest, it matches Sonarr's grabbed quality. When the release **lied** (advertised `Bluray-1080p`, the stream is a web encode), ours writes the measured value while Sonarr writes what it grabbed — deliberately: _identical when the release was honest; truthful when it wasn't._

## Per-type structure

Templates are organized **per media type**, not per file class. Within a type, the template entity carries multiple components:

| Type     | Required components               | Renders to (in order)                              |
| -------- | --------------------------------- | -------------------------------------------------- |
| `movie`  | `movie_dir_template`, `template`  | `<movie folder>/<movie file>`                      |
| `series` | `series_show_template`, `series_season_template`, `template` | `<show folder>/<season folder>/<episode file>` |

Each component renders to a **single path component** — no embedded `/` or `\`. The import worker joins them via `filepath.Join`, which is OS-aware.

The library's `root_path` is prepended at apply time. Templates never reference the library root; that decoupling is intentional — the same template works against any library.

## Default selection

When routing's action set doesn't carry an explicit `name_template_id`, the import worker looks up the per-type default via `GetDefault(type)`. The partial unique index `(type) WHERE default = true` enforces at-most-one per type.

If no default exists for a type and the routing action didn't specify, **import will fail**. There is no system-baked fallback template today. The first-install seed sets defaults for both types so this is rarely a real-world issue; v1 might harden this with a minimal hardcoded fallback (see [open questions #6](#open-questions)).

## Path safety

Three layers:

1. **The `sanitize` and `clean` funcs** strip illegal chars (`/\:*?"<>|`) and trim whitespace. Template authors call these explicitly; nothing is auto-sanitized.
2. **`filepath.Join`** handles OS-specific separators on join.
3. **Extension appending** is done by the import worker (`importer.EnsureExt`), not the template. Templates render basenames without extensions; the source file's extension is appended after render.

Notably **not** handled:

- **Length limits** — Windows `MAX_PATH` (260) and per-component limits. A long-titled movie at full quality detail can overflow. See [open questions #4](#open-questions).
- **Unicode normalization** — NFC vs NFD differences between filesystems. Files may round-trip through scan with different paths than they were imported with. See [open questions #8](#open-questions).
- **Reserved Windows filenames** — `CON`, `PRN`, `AUX`, etc. Same open question as length limits.

For self-hosted Linux installs (the primary target), these gaps are mostly theoretical. For Windows / NAS deployments they matter.

## Missing-variable behavior

Today: **silent empty string**. Go's `text/template` renders missing or zero-value fields as `<no value>` by default, but the renderer initializes the context to safe zero values (empty struct pointers, empty strings) so missing fields render as empty strings instead.

This is the right behavior for **optional MediaInfo / Release fields** — `{{if .MediaInfo.HDR}}[{{.MediaInfo.HDR}}]{{end}}` correctly emits nothing when HDR is empty.

It is the wrong behavior for **typos**. `{{.Media.Titel}}` silently renders nothing; the operator sees the resulting filename and may not notice the dropped title until much later.

**V1 contract:**

- Runtime behavior unchanged — silent empty string, because changing this breaks every existing template that uses `{{if}}` guards.
- **Save-time lint** — the validator parses the template, walks the AST, and warns on any dotted-path access that doesn't resolve against the known variable catalog. Save still succeeds (admins may have reasons to use experimental paths), but the response carries a `warnings: [...]` field.
- **Strict mode flag** — opt-in per template: `strict_mode: true` upgrades render-time missing-variable behavior to a hard error, surfacing as a job failure with a clear message rather than a silently-wrong file.

Strict mode is the safety net for serious operators; lint is the safety net for casual ones.

## Lifecycle and operations

CRUD only. No verbs.

| OperationID                       | Method | Path                                            | Notes                                                                                  |
| --------------------------------- | ------ | ----------------------------------------------- | -------------------------------------------------------------------------------------- |
| `name-templates-list`             | GET    | `/api/v1/name-templates`                        |                                                                                        |
| `name-templates-get`              | GET    | `/api/v1/name-templates/{id}`                   |                                                                                        |
| `name-templates-get-default`      | GET    | `/api/v1/name-templates/default/{type}`         | Per-type default lookup.                                                               |
| `name-templates-create`           | POST   | `/api/v1/name-templates`                        | v1: validates syntax + lints variables.                                                |
| `name-templates-update`           | PUT    | `/api/v1/name-templates/{id}`                   | Same validation as create.                                                             |
| `name-templates-delete`           | DELETE | `/api/v1/name-templates/{id}`                   |                                                                                        |
| `name-templates-preview` *(v1)*   | POST   | `/api/v1/name-templates/preview`                | Renders one or more templates against sample data; returns the resulting paths. See [Preview / dry-run](#preview--dry-run). |

Permissions (defined in [users](../users/README.md)):

- `name_templates.read` — list / get / get-default / preview
- `name_templates.write` — create / update / delete

## Validation

Today the service validates: name non-empty, type in enum, `template` non-empty, `movie_dir_template` non-empty when `type='movie'`. **It does not parse the template strings.** Syntax errors only surface at import time, failing the job.

**V1 additions, at create and update:**

1. **Parse each template component** with `text/template`. Syntax errors return `apperrors.Validation` with a structured pointer to the offending component (e.g., `"series_season_template: unexpected }} at byte 24"`).
2. **Walk the parsed AST** for variable references. Emit lint warnings for paths that don't resolve against the known catalog (e.g., `Media.Titel`).
3. **Dry-render against a fixture** — the validator includes a built-in fixture per type and confirms the template at least produces a non-empty string. Catches issues like "every component is wrapped in `{{if}}` and the fixture happens to render nothing."

Validation does **not** verify the rendered output is filesystem-legal — `sanitize` does that at render time, and a template that doesn't call `sanitize` on user-supplied fields is the template author's responsibility (the lint can recommend it, but doesn't require it).

## Preview / dry-run

**New in v1.** A `POST /name-templates/preview` endpoint that accepts:

- A template body (full or partial — any subset of `template`, `movie_dir_template`, `series_show_template`, `series_season_template`)
- A type
- An optional sample data override (otherwise built-in fixtures per type are used)

Returns: each component's rendered string, the joined path, and any lint warnings. The frontend uses this to drive a live preview in the template-editor dialog — the killer UX feature missing today.

The preview endpoint shares the validator's lint + parse code; it's the read-only counterpart.

## Multi-episode handling

Today: **one template render per episode file.**

A season-pack download is matched per-file to its episodes by the import matcher; each match spawns its own `ImportTask`, each task renders the template independently with that episode's metadata. The result for a 5-episode pack is five separate files, each named `S01E01.ext`, `S01E02.ext`, …, `S01E05.ext`.

**Range-based templates** (`S01E01-E05.mkv` for a multi-episode file) are **not supported**. The matcher always splits multi-episode files into single-episode `ImportTask` rows, so the template engine never sees the range case. Adding support means changing the matcher first, then surfacing range variables to the template context (`.Media.SeasonRange`, `.Media.EpisodeRange`, etc.). Tracked in [open questions #2](#open-questions).

## Integration points

| Consumer                                          | How it uses name templates                                                                                                              |
| ------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| **[Routing](../routing/README.md)**               | Rules pick a template by ID. The action set on a fired rule fills `download_job.name_template_id`, which propagates to import tasks.    |
| **Import (existing)**                             | Loads the template row, renders each component against the import-task's `EvaluationContext`, joins with `filepath.Join`, appends extension. |
| **[Parsing](../parsing/README.md)** | Produces the advertised `Quality`/`Release` the templates read.                                                                       |
| **[Quality profiles](../quality-profiles/README.md)** | The re-gate reconciles `Quality` to asserted truth before render; `{{.Quality.Full}}` is the file's quality of record, not the advertised claim. |
| **[Acquisition](../acquisition/README.md)**       | The pipeline assembles the `EvaluationContext` for both routing and import — the template just reads it.                                |
| **[Users](../users/README.md)**                   | `name_templates.*` permissions gate the API.                                                                                            |
| **Frontend (`NameTemplateSettings.vue`)**         | CRUD + (v1) live preview using the new endpoint.                                                                                        |

## What name templates does NOT own

- The routing decision that picks a template ([routing](../routing/README.md))
- The variable catalog itself — that's defined by `EvaluationContext` and lives in code adjacent to whoever owns that struct (matching / metadata / mediainfo) ([metadata](../metadata/README.md), [matching](../matching/README.md))
- The name-parser that produces `Quality` and `Release` (lives in [parsing](../parsing/README.md))
- The **re-gate** that reconciles `Quality` to asserted truth before render ([quality profiles](../quality-profiles/README.md#import-time-re-gate) logic, run by the [importer](../importer/README.md))
- The mediainfo probe (lives with import / scan)
- The library root path that gets prepended ([libraries](../libraries/README.md))
- The file extension (appended by the import worker, source-derived)
- Hardlink and atomic-move mechanics (import)

## Open questions

1. **Syntax-vs-lint strictness levels.** V1 has parse-as-error + lint-as-warning. Worth a per-template `strict_mode` flag (escalates lint to error and missing-variable to error)? Lean: yes, opt-in. Already in the spec but pin the data shape in iteration 2.
2. **Range-based multi-episode rendering.** "When the file actually contains S01E01-E05, name it `S01E01-E05`." Requires matcher changes upstream; not a template-spec-only fix. Worth doing eventually for users with multi-episode releases (common in older shows). Track but defer.
3. **Anime numbering.** Anime has dual numbering (per-season + absolute). The catalog has `Episode` (season-relative); no `AbsoluteEpisode`. Adding it is straightforward (one field on `Media`), but it should be added once anime support is more cohesively planned — same call-out in [tracking](../tracking/README.md).
4. **Path length and Windows reserved names.** No protection today. Worth a render-time length check + reserved-name check, configurable per library or globally? Lean: best-effort warning at preview, no hard enforcement (users self-hosting on Windows should pick shorter templates).
5. **Unicode normalization.** Filesystems vary in how they store NFC vs NFD. A template emitting NFC may round-trip as NFD on macOS. Worth normalizing at render? Lean: NFC at render, document the choice. Low priority.
6. **Hardcoded fallback template.** If no per-type default exists, today import fails. Worth a minimal hardcoded fallback (`{{.Media.CleanTitle}} ({{.Media.Year}})` for movies, `{{.Media.Title}} - S{{.Media.Season}}E{{.Media.Episode}}` for series) so the system never bricks itself? Lean: yes, but only as a last-resort fallback, with a loud [notification](../notifications/README.md).
7. **Per-quality templates.** "Use this template for 4K, that one for HD." Expressible today via `{{if eq .Quality.Resolution "2160p"}}` inside one template, but UX-clunky. Alternative: routing rules already choose the template, so admins can just author multiple templates and route accordingly. Lean: don't add a new abstraction; doc the routing-based pattern.
8. **Sanitization customization.** Some operators want underscores instead of dashes for replaced chars, or want to preserve `+` and `&`. Worth a per-template sanitization config? Lean: not in v1 — the defaults are sensible, customization is a long tail.
9. **Includes / snippets / partials.** Repeating the same `[audio][hdr][video]` suffix across templates is annoying. Go templates support `{{template "name"}}` partials but they require a multi-template parse. Worth a "shared snippets" feature? Lean: defer; if it becomes a real pain, add later as a `text_snippet` resource referenced by name.
10. **Template versioning / snapshotting at job-create time.** Today, editing a template affects all future imports immediately, but in-flight imports use the version they loaded at task-create. Should a template carry a `version` and the import task snapshot the version-id? Lean: defer — the current behavior is fine for self-hosted scale.
11. **Self-documenting variable catalog endpoint.** `GET /name-templates/variables` returns the full catalog (namespaces, fields, types, availability windows). Useful for the frontend's template editor (autocomplete) and for keeping documentation in sync. Lean: ship in v1 if cheap, otherwise iteration 2.
12. **Strict typing for `Season` / `Episode`.** Currently exposed as zero-padded 2-digit strings, hiding the underlying int. Templates that need arithmetic (`{{add .Media.Season 1}}`) can't. Worth surfacing both `Season` (string for display) and `SeasonNum` (int)? Lean: low priority.
13. **Template testing harness.** A way for operators to commit a small fixture file (sample release + sample episode) alongside their template, run a CI-style check, and catch regressions. Probably overkill for self-hosted; flag and forget unless real demand.

## What we're explicitly not deciding here

- Exact JSON shape of `preview` response
- The lint catalog (which variable paths exist) — that comes from the `EvaluationContext` struct, single source of truth
- Frontend UX for the template editor (live preview, syntax highlighting, autocomplete)
- The matcher changes required to support range-based multi-episode rendering
- Migration plan for the new `strict_mode` column
- Backfill of lint warnings against existing saved templates
- Whether the variable-catalog endpoint is paginated, cached, etc.

## Doc neighbors

- [Routing](../routing/README.md) — picks the template that import applies
- [Libraries](../libraries/README.md) — provides the root path that gets prepended
- [Downloaders](../downloaders/README.md) — sibling routing-action
- [Acquisition](../acquisition/README.md) — assembles the `EvaluationContext` consumed by both routing and import
- [Quality profiles](../quality-profiles/README.md) — produces `Quality.*` via the same parser the templates read
- [Metadata](../metadata/README.md) — produces `Media.*`
- [Matching](../matching/README.md) — determines which template runs per file (movie vs episode, season pack split)
- [Users](../users/README.md) — `name_templates.*` permissions
