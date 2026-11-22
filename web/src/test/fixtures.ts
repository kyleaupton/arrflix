// Builders for API payloads. Each returns a complete, spec-typed value with
// neutral defaults, so a test states only the fields its scenario turns on.

import { HttpResponse } from 'msw'
import type {
  DownloaderPathCheck,
  PathTestResult,
  ProblemDetails,
  Request,
  Role,
  TitleStatus,
  User,
  Volume,
} from '@/client/types.gen'
import { requester } from './render'

export function request(overrides: Partial<Request> = {}): Request {
  return {
    id: '10000000-0000-0000-0000-000000000001',
    type: 'movie',
    tmdbId: 693134,
    tier: 'HD',
    scopeRule: '',
    status: 'spawned',
    requestedBy: requester.sub,
    decidedAt: null,
    decidedBy: '',
    decisionAuto: true,
    deniedReason: null,
    spawnedTrackingId: '20000000-0000-0000-0000-000000000001',
    createdAt: '2026-11-01T12:00:00Z',
    updatedAt: '2026-11-01T12:00:00Z',
    ...overrides,
  }
}

export function role(name: string, overrides: Partial<Role> = {}): Role {
  return {
    id: `role-${name}`,
    name,
    description: null,
    builtIn: true,
    createdAt: '2026-11-01T12:00:00Z',
    ...overrides,
  }
}

export function user(overrides: Partial<User> = {}): User {
  return {
    id: '30000000-0000-0000-0000-000000000001',
    email: 'member@example.com',
    username: 'member',
    isActive: true,
    roles: [role('requester')],
    autoApprove: false,
    allow4k: false,
    createdAt: '2026-11-01T12:00:00Z',
    updatedAt: '2026-11-01T12:00:00Z',
    ...overrides,
  }
}

export function titleStatus(overrides: Partial<TitleStatus> = {}): TitleStatus {
  return {
    mediaType: 'movie',
    tmdbId: 693134,
    state: 'searching',
    active: true,
    actions: [],
    counts: { available: 0, total: 1, working: 1 },
    library: { fileCount: 0, hasFiles: false },
    viewer: { isRequester: false },
    ...overrides,
  }
}

export function pathCheck(overrides: Partial<DownloaderPathCheck> = {}): DownloaderPathCheck {
  return {
    downloaderId: '40000000-0000-0000-0000-000000000001',
    downloaderName: 'qBittorrent',
    status: 'no_mapping_needed',
    savePath: '/data/torrents',
    resolvedPath: '/data/torrents',
    ...overrides,
  }
}

export function volume(overrides: Partial<Volume> = {}): Volume {
  return {
    id: '50000000-0000-0000-0000-000000000001',
    name: 'media',
    arrflixBase: '/data',
    aliases: [],
    createdAt: '2026-11-01T12:00:00Z',
    updatedAt: '2026-11-01T12:00:00Z',
    ...overrides,
  }
}

export function pathTestResult(overrides: Partial<PathTestResult> = {}): PathTestResult {
  return {
    downloaderId: '40000000-0000-0000-0000-000000000001',
    path: '/downloads',
    resolvedPath: '/data/torrents',
    exists: true,
    libraries: [],
    ...overrides,
  }
}

/**
 * A resolver answering with an RFC 9457 problem, for reads a test needs to
 * settle but doesn't care about (e.g. `handleMediaGetMovie(notFound)`).
 */
export function notFound() {
  const body: ProblemDetails = { type: 'about:blank', status: 404, title: 'Not Found' }
  return HttpResponse.json(body, { status: 404 })
}
