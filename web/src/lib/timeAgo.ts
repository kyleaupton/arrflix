// A tiny relative-time formatter for chrome that shows "when" without pulling in
// a date library. Renders the coarsest sensible unit ("just now", "5m", "3h",
// "2d", "3w") and falls back to a localized date past ~4 weeks, where "n weeks
// ago" stops being more useful than the date itself. Pure and synchronous, so it
// works inside a v-for without a composable per row; callers re-invoke it on the
// cadence they care about (the bell re-renders when its query refetches).
export function timeAgo(input: string | number | Date, now: number = Date.now()): string {
  const then = input instanceof Date ? input.getTime() : new Date(input).getTime()
  if (Number.isNaN(then)) return ''

  const diffMs = now - then
  const sec = Math.round(diffMs / 1000)

  // Clamp small negatives (clock skew between server and client) to "just now"
  // rather than rendering a nonsensical future time.
  if (sec < 45) return 'just now'

  const min = Math.round(sec / 60)
  if (min < 60) return `${min}m ago`

  const hr = Math.round(min / 60)
  if (hr < 24) return `${hr}h ago`

  const day = Math.round(hr / 24)
  if (day < 7) return `${day}d ago`

  const wk = Math.round(day / 7)
  if (wk < 4) return `${wk}w ago`

  return new Date(then).toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  })
}
