---
paths:
  - "backend/internal/versioninfo/**"
  - "backend/internal/github/**"
  - "backend/internal/semver/**"
  - "backend/internal/service/version.go"
  - "web/src/components/settings/VersionCard.vue"
---

# Version & update system

Arrflix has a built-in version tracking and update check system.

## Build metadata

- Version information is injected at Docker build time via build args.
- Environment variables: `ARRFLIX_VERSION`, `ARRFLIX_COMMIT`, `ARRFLIX_BUILD_DATE`, `PROWLARR_VERSION`.
- Dev builds default to version `dev` with no update checks.
- Edge builds (from main branch) compare commit SHAs.
- Stable releases use semantic versioning and compare against GitHub releases.

## API

- `GET /api/v1/version` — returns build information and update status (cached 15 minutes).

## Implementation

- `backend/internal/versioninfo/` — reads environment variables.
- `backend/internal/github/` — GitHub API client.
- `backend/internal/semver/` — semantic version comparison.
- `backend/internal/service/version.go` — update check logic with caching.
- `web/src/components/settings/VersionCard.vue` — UI component.

## Update logic

- Dev builds: always show status "unknown".
- Edge builds: compare commit SHA with GitHub main HEAD.
- Stable releases: compare semver with latest GitHub release, show release notes.
- Prereleases: always show status "unknown".
- GitHub API responses cached for 15 minutes using the existing `api_cache` table.
