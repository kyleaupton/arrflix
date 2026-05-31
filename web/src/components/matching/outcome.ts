import type { BadgeVariants } from '@/components/ui/badge'

// Outcome bands that can appear in the matcher inbox. `confident` (auto-matched)
// and `detached` (user-rejected) never surface here — the backend excludes them
// from /unmatched-files — so they are absent from this union.
export type InboxOutcome =
  | 'confident_review'
  | 'low_confidence'
  | 'ambiguous'
  | 'no_match'
  | 'partial_series'

interface OutcomeMeta {
  label: string
  variant: NonNullable<BadgeVariants['variant']>
}

// Triage order: bands that already carry an identity (a one-click confirm) come
// first, descending to the ones that need the most work to identify.
export const INBOX_OUTCOMES: InboxOutcome[] = [
  'confident_review',
  'partial_series',
  'ambiguous',
  'low_confidence',
  'no_match',
]

const OUTCOME_META: Record<InboxOutcome, OutcomeMeta> = {
  confident_review: { label: 'Needs review', variant: 'secondary' },
  partial_series: { label: 'Partial series', variant: 'secondary' },
  ambiguous: { label: 'Ambiguous', variant: 'outline' },
  low_confidence: { label: 'Low confidence', variant: 'outline' },
  no_match: { label: 'No match', variant: 'destructive' },
}

// Tolerant of the raw `string` outcome carried on InboxItem/MatchDecision, which
// is wider than InboxOutcome (it can also be confident/detached in other surfaces).
export function outcomeLabel(outcome: string): string {
  return OUTCOME_META[outcome as InboxOutcome]?.label ?? outcome
}

export function outcomeVariant(outcome: string): OutcomeMeta['variant'] {
  return OUTCOME_META[outcome as InboxOutcome]?.variant ?? 'outline'
}
