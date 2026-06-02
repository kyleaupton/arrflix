// Display labels for the canonical media status set. The backend maps TMDB's
// status wording down to these tokens at the provider boundary (see
// model.CanonicalizeStatus); the UI labels decouple from TMDB's strings here.
// `unknown` (or a missing status) renders no label.
export const MEDIA_STATUS_LABELS: Record<string, string> = {
  upcoming: 'Upcoming',
  released: 'Released',
  continuing: 'Continuing',
  ended: 'Ended',
  canceled: 'Canceled',
}

// statusLabel returns the display label for a canonical status token, or an
// empty string for `unknown`/missing — callers gate the chip on a truthy result.
export function statusLabel(status: string | null | undefined): string {
  if (!status) return ''
  return MEDIA_STATUS_LABELS[status] ?? ''
}
