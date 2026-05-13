# Arrflix task runner. All recipes execute inside the dev container so the
# environment matches what runs in CI and what s6-overlay manages day-to-day.
#
# Quick start:
#   just up         # start the dev container
#   just check      # mirror CI: lint + type-check + unit tests (read-only)
#   just fix        # autofix everything that can be: fmt, lint --fix, regen
#   just preflight  # fix then check — run before declaring work done

container := "arrflix-dev"
backend-exec := "docker compose exec -T -w /app/backend " + container
web-exec := "docker compose exec -T -w /app/web " + container

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

# --- backend (in-container) --------------------------------------------------

# Format Go code (gofmt -w).
[group('backend')]
backend-fmt: _ensure-up
    {{backend-exec}} gofmt -w .

# Lint Go code with golangci-lint (autofix where possible).
[group('backend')]
backend-lint: _ensure-up
    {{backend-exec}} golangci-lint run --fix

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
    {{backend-exec}} go test -race -tags=integration ./internal/test/integration/...

# Regenerate the OpenAPI spec from huma operation declarations.
[group('backend')]
backend-genspec: _ensure-up
    {{backend-exec}} go run ./cmd/genspec

# Regenerate sqlc Go code from queries + migrations.
[group('backend')]
backend-sqlc: _ensure-up
    {{backend-exec}} sqlc generate

# --- frontend (in-container) -------------------------------------------------

# Format frontend code with prettier (writes).
[group('frontend')]
web-fmt: _ensure-up
    {{web-exec}} npm run format

# Lint frontend code with eslint (autofix).
[group('frontend')]
web-lint: _ensure-up
    {{web-exec}} npm run lint

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
    {{web-exec}} npm run openapi-ts

# --- aggregates --------------------------------------------------------------

# Format both backend and frontend.
[group('aggregate')]
fmt: backend-fmt web-fmt

# Regenerate everything (sqlc, openapi spec, ts client). Order matters —
# the TS client reads the spec produced by genspec.
[group('aggregate')]
gen: backend-sqlc backend-genspec web-genclient

# Autofix everything: format, lint --fix, regenerate. Best-effort; lint
# errors that can't be autofixed are left for `check` to surface.
[group('aggregate')]
fix: _ensure-up
    @echo "→ formatting"
    {{backend-exec}} gofmt -w .
    {{web-exec}} npm run format
    @echo "→ lint --fix"
    -{{backend-exec}} golangci-lint run --fix
    -{{web-exec}} npm run lint
    @echo "→ regenerating"
    {{backend-exec}} sqlc generate
    {{backend-exec}} go run ./cmd/genspec
    {{web-exec}} npm run openapi-ts
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
    {{backend-exec}} go test -race -tags=integration ./internal/test/integration/...
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
