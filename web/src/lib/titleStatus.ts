// The presentation vocabulary for the acquisition read model — the one place a
// TitleStatus.state becomes a label, an icon, and a badge variant.
//
// The state itself is derived server-side (internal/titlestatus). This file is
// deliberately the *only* thing the frontend adds on top: a rendering choice per
// state, made once. Every acquisition surface reads from here, which is what
// makes the poster chip, the hero control, and the status card unable to
// disagree — they are not three derivations agreeing, they are one lookup.
//
// See specs/modules/title-status/README.md.

import {
  CalendarClock,
  Check,
  CircleAlert,
  CircleSlash,
  Clock,
  Download,
  Hand,
  Loader,
  Search,
  Sparkles,
  ThumbsDown,
  type LucideIcon,
} from 'lucide-vue-next'
import type { BadgeVariants } from '@/components/ui/badge'

// TitleState is the projection's headline vocabulary. It mirrors the State
// constants in internal/titlestatus — the OpenAPI schema types `state` as a bare
// string, so this is where the frontend pins the closed set. A value outside it
// falls through to the neutral presentation below rather than rendering raw.
export type TitleState =
  | 'not_requested'
  | 'unreleased'
  | 'awaiting_approval'
  | 'denied'
  | 'searching'
  | 'needs_pick'
  | 'proposed'
  | 'downloading'
  | 'importing'
  | 'available'
  | 'partially_available'
  | 'unavailable'
  | 'canceled'

// Presentation is everything a surface needs to render a state without knowing
// what the state means.
//
// `label` is the second-person phrasing used by the chip and the pill, where
// space is tight. `headline` is the sentence form the status card leads with;
// where the two would be identical the state omits it and callers fall back to
// `label`. Splitting them here rather than at each call site is what keeps the
// card and the chip from drifting into different wordings for one state.
export interface Presentation {
  label: string
  headline?: string
  icon: LucideIcon
  variant: BadgeVariants['variant']
  // attention marks states waiting on a person rather than on the pipeline.
  // Surfaces use it to decide what to surface first; it is not a color.
  attention?: boolean
}

const PRESENTATION: Record<TitleState, Presentation> = {
  not_requested: {
    label: 'Not in your library',
    icon: CircleSlash,
    variant: 'outline',
  },
  unreleased: {
    label: 'Not out yet',
    headline: 'Not available yet',
    icon: CalendarClock,
    variant: 'outline',
  },
  awaiting_approval: {
    label: 'Awaiting approval',
    headline: 'Your request is awaiting approval',
    icon: Clock,
    variant: 'secondary',
    attention: true,
  },
  denied: {
    label: 'Declined',
    headline: 'Your request was declined',
    icon: ThumbsDown,
    variant: 'outline',
  },
  searching: {
    label: 'Searching',
    headline: 'Searching for a release',
    icon: Search,
    variant: 'secondary',
  },
  needs_pick: {
    label: 'Needs your pick',
    headline: 'Waiting for a release to be chosen',
    icon: Hand,
    variant: 'secondary',
    attention: true,
  },
  proposed: {
    label: 'Suggested',
    headline: 'A download suggestion is waiting for review',
    icon: Sparkles,
    variant: 'default',
    attention: true,
  },
  downloading: {
    label: 'Downloading',
    icon: Download,
    variant: 'default',
  },
  importing: {
    label: 'Importing',
    headline: 'Adding to your library',
    icon: Loader,
    variant: 'default',
  },
  available: {
    label: 'In your library',
    icon: Check,
    variant: 'default',
  },
  partially_available: {
    label: 'Partly in your library',
    icon: Check,
    variant: 'secondary',
  },
  unavailable: {
    label: "Couldn't find it",
    headline: 'No release could be found',
    icon: CircleAlert,
    variant: 'destructive',
  },
  canceled: {
    label: 'Canceled',
    icon: CircleSlash,
    variant: 'outline',
  },
}

// A state the frontend doesn't recognize is a backend that added one. Render it
// neutrally rather than blank: an unlabeled chip is a bug report nobody files.
const UNKNOWN: Presentation = {
  label: 'Unknown',
  icon: CircleSlash,
  variant: 'outline',
}

// present maps a projection state onto how to render it. Total — any string in,
// a usable Presentation out.
export function present(state: string | null | undefined): Presentation {
  if (!state) return UNKNOWN
  return PRESENTATION[state as TitleState] ?? UNKNOWN
}

// headlineOf is the card's sentence form, falling back to the chip label for
// states where one phrasing serves both.
export function headlineOf(state: string | null | undefined): string {
  const p = present(state)
  return p.headline ?? p.label
}

// isOnDisk reports whether the title has something watchable, which is not the
// same as "finished" — a partly-available series is watchable now and still
// acquiring. Surfaces that gate playback read this; surfaces that gate the
// request affordance read actions[] instead.
export function isOnDisk(state: string | null | undefined): boolean {
  return state === 'available' || state === 'partially_available'
}
