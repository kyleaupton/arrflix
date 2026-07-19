# Arrflix task runner. All recipes execute inside the dev container so the
# environment matches what runs in CI and what s6-overlay manages day-to-day.
#
# Quick start:
#   just up         # start the dev container
#   just check      # mirror CI: lint + type-check + unit tests (read-only)
#   just fix        # autofix everything that can be: fmt, lint --fix, regen
#   just preflight  # fix then check — run before declaring work done

# Load .env so recipes can read secrets/config (e.g. DOWNLOADER_PASSWORD) the
# same way the app does. Missing .env is not an error.
set dotenv-load := true

container := "arrflix-dev"
backend-exec := "docker compose exec -T -w /app/backend " + container
web-exec := "docker compose exec -T -w /app/web " + container

# The container runs as root, so files it writes into the bind mount land
# root-owned on the host and break git ops. Recipes that WRITE (formatters,
# generators) exec as the host uid/gid instead, so output is owned by whoever
# ran `just`. HOME points at a per-uid temp dir so go/npm caches resolve for an
# uid with no /etc/passwd entry (e.g. CI). Read-only recipes stay root to reuse
# root's warm caches.
host-uid := `id -u`
host-gid := `id -g`
exec-rw := "docker compose exec -T -u " + host-uid + ":" + host-gid + " -e HOME=/tmp/arrflix-dev-home-" + host-uid
backend-exec-rw := exec-rw + " -w /app/backend " + container
web-exec-rw := exec-rw + " -w /app/web " + container

# In-container connection string (matches config.go's default). Postgres listens
# on localhost inside the dev container with trust auth.
db-url := "postgres://arrflix:arrflixpw@127.0.0.1:5432/arrflix?sslmode=disable"

# Default recipe: list everything organized by group.
default:
    @just --list --unsorted

# --- container ---------------------------------------------------------------

# Start the dev container in the background.
[group('container')]
up:
    docker compose up -d {{container}}

# Stop the dev container.
[group('container')]
down:
    docker compose down

# Tail container logs (optionally pass a service name).
[group('container')]
logs *services:
    docker compose logs -f --tail=100 {{services}}

# Open an interactive bash shell inside the dev container.
[group('container')]
shell:
    docker compose exec -w /app {{container}} bash

# Rebuild the dev image (use after Dockerfile.dev changes).
[group('container')]
rebuild:
    docker compose up -d --build {{container}}

# --- prod-like ---------------------------------------------------------------
# Build and run the published-image experience locally. docker/Dockerfile.prod
# built for the host arch only (no QEMU) — minutes, not the ~15 the multi-arch
# CI build takes. One all-in-one container, APP_ENV=prod, fresh onboarding, a
# postgres volume isolated from dev. Host port defaults to 8484; override with
# ARRFLIX_PROD_PORT in .env.

prod-compose := "docker compose -f docker-compose.prod.yml"

# Build the prod image locally (host arch only).
[group('prod')]
prod-build:
    {{prod-compose}} build

# Build (if needed) and start the prod-like container in the background.
[group('prod')]
prod-up:
    {{prod-compose}} up -d --build

# Stop and remove the prod-like container.
[group('prod')]
prod-down:
    {{prod-compose}} down

# Tail prod-like container logs.
[group('prod')]
prod-logs:
    {{prod-compose}} logs -f

# --- backend (in-container) --------------------------------------------------

# Format Go code (gofmt -w).
[group('backend')]
backend-fmt: _ensure-up
    {{backend-exec-rw}} gofmt -w .

# Lint Go code with golangci-lint (autofix where possible).
[group('backend')]
backend-lint: _ensure-up
    {{backend-exec-rw}} golangci-lint run --fix

# Run go vet.
[group('backend')]
backend-vet: _ensure-up
    {{backend-exec}} go vet ./...

# Run backend unit tests (excludes integration suite).
[group('backend')]
backend-test: _ensure-up
    {{backend-exec}} go test -race ./...

# Run backend integration tests (spins up postgres testcontainers).
[group('backend')]
backend-test-integration: _ensure-up
    {{backend-exec}} go test -race -tags=integration ./internal/test/integration/... ./internal/jobs/...

# Regenerate the parser parity goldens from live pinned Sonarr/Radarr
# containers (Tier 2 — slow, spins up two containers via the docker socket).
# On-demand only; the fast Tier-1 parity test reads the committed goldens.
[group('backend')]
parity-regen: _ensure-up
    {{backend-exec}} go test -tags=parity -timeout=20m -run TestRegenerateGoldens -v ./internal/test/parity/...

# Regenerate the OpenAPI spec from huma operation declarations.
[group('backend')]
backend-genspec: _ensure-up
    {{backend-exec-rw}} go run ./cmd/genspec

# Regenerate sqlc Go code from queries + migrations.
[group('backend')]
backend-sqlc: _ensure-up
    {{backend-exec-rw}} sqlc generate

# Wipe the app DB, re-apply migrations, and load the dev seed. The seeded
# downloader password comes from DOWNLOADER_PASSWORD in .env (kept out of git),
# defaulting to 'admin'. The TMDB api key is seeded from TMDB_API_KEY when set
# (omitted otherwise). Only touches the arrflix DB — Prowlarr's DBs are left
# alone.
#
# Finishes by restarting the backend: the default quality profiles and tier
# bindings are seeded in Go on startup (QualityProfileService.SeedDefaults),
# not by SQL, so the drop-schema step wipes them and only a fresh process
# re-creates them. SeedDefaults is idempotent, so the restart is safe.
[group('backend')]
db-reseed: _ensure-up
    #!/usr/bin/env bash
    set -euo pipefail
    pw="${DOWNLOADER_PASSWORD:-admin}"
    seed_vars=(-v downloader_password="$pw")
    if [ -n "${TMDB_API_KEY:-}" ]; then
      seed_vars+=(-v tmdb_api_key="$TMDB_API_KEY")
    fi
    echo "→ resetting schema"
    {{backend-exec}} psql '{{db-url}}' -v ON_ERROR_STOP=1 -c 'drop schema public cascade; create schema public;'
    echo "→ applying migrations"
    {{backend-exec}} go run ./cmd/migrate
    echo "→ seeding"
    {{backend-exec}} psql '{{db-url}}' -v ON_ERROR_STOP=1 "${seed_vars[@]}" -f internal/db/seed/seed_dev.sql
    echo "→ restarting backend (re-seeds quality profiles)"
    {{backend-exec}} s6-svc -r /run/service/backend
    echo "✓ db reseeded"

# --- frontend (in-container) -------------------------------------------------

# Format frontend code with prettier (writes).
[group('frontend')]
web-fmt: _ensure-up
    {{web-exec-rw}} npm run format

# Lint frontend code with eslint (autofix).
[group('frontend')]
web-lint: _ensure-up
    {{web-exec-rw}} npm run lint

# Run vue-tsc type-check.
[group('frontend')]
web-type-check: _ensure-up
    {{web-exec}} npm run type-check

# Build the production bundle.
[group('frontend')]
web-build: _ensure-up
    {{web-exec}} npm run build

# Regenerate the TypeScript API client from the OpenAPI spec.
[group('frontend')]
web-genclient: _ensure-up
    {{web-exec-rw}} npm run openapi-ts

# Compile MJML email sources (backend/internal/notifications/emailsrc/) into the
# Go-embedded template tree. Node/MJML live in the web toolchain; the compiled
# .html.tmpl output is generated — edit the .mjml source, never the output.
[group('frontend')]
gen-email: _ensure-up
    {{web-exec-rw}} npm run gen-email

# --- aggregates --------------------------------------------------------------

# Format both backend and frontend.
[group('aggregate')]
fmt: backend-fmt web-fmt

# Regenerate everything (sqlc, openapi spec, email templates, ts client). Order
# matters for the API chain — the TS client reads the spec produced by genspec.
# gen-email is independent (compiles MJML → embedded HTML) and runs alongside.
[group('aggregate')]
gen: backend-sqlc backend-genspec gen-email web-genclient

# Autofix everything: format, lint --fix, regenerate. Best-effort; lint
# errors that can't be autofixed are left for `check` to surface.
[group('aggregate')]
fix: _ensure-up
    @echo "→ formatting"
    {{backend-exec-rw}} gofmt -w .
    {{web-exec-rw}} npm run format
    @echo "→ lint --fix"
    -{{backend-exec-rw}} golangci-lint run --fix
    -{{web-exec-rw}} npm run lint
    @echo "→ regenerating"
    {{backend-exec-rw}} sqlc generate
    {{backend-exec-rw}} go run ./cmd/genspec
    {{web-exec-rw}} npm run gen-email
    {{web-exec-rw}} npm run openapi-ts
    @echo "Done. Run 'just check' to verify."

# Read-only mirror of CI: lint + type-check + unit tests. Fails fast on the
# first issue. Does NOT regenerate generated code (CI catches drift).
[group('aggregate')]
check: _ensure-up
    @echo "→ backend vet"
    {{backend-exec}} go vet ./...
    @echo "→ backend lint"
    {{backend-exec}} golangci-lint run
    @echo "→ frontend lint"
    {{web-exec}} npx eslint .
    @echo "→ frontend prettier check"
    {{web-exec}} npx prettier --check src/
    @echo "→ frontend type-check"
    {{web-exec}} npm run type-check
    @echo "→ backend unit tests"
    {{backend-exec}} go test -race ./...
    @echo "All checks passed."

# `check` plus the integration suite (slow — spins up postgres testcontainers).
[group('aggregate')]
check-all: check
    @echo "→ backend integration tests"
    {{backend-exec}} go test -race -tags=integration ./internal/test/integration/... ./internal/jobs/...
    @echo "All checks (incl. integration) passed."

# Agent's pre-push entry point: fix then check. If this is green, work is
# ready for human review. The user commits selectively from the resulting
# tree.
[group('aggregate')]
preflight: fix check

# --- internal helpers --------------------------------------------------------

[private]
_ensure-up:
    @docker compose ps --services --filter "status=running" 2>/dev/null | grep -q "^{{container}}$" || { echo "Error: {{container}} container is not running. Run 'just up' first."; exit 1; }
