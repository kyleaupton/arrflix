import prettyBytes from 'pretty-bytes'
import prettyMilliseconds from 'pretty-ms'

/** Format bytes to human-readable string, e.g. "4.2 GB" */
export function formatBytes(bytes: number | null | undefined): string {
  if (bytes == null || bytes <= 0) return ''
  return prettyBytes(bytes)
}

/** Format bytes/sec to human-readable speed, e.g. "2.5 MB/s" */
export function formatSpeed(bytesPerSec: number | null | undefined): string {
  if (bytesPerSec == null || bytesPerSec <= 0) return ''
  return prettyBytes(bytesPerSec) + '/s'
}

// qBittorrent uses 8640000 as a sentinel for "unknown ETA"
const QBIT_ETA_UNKNOWN = 8640000

/** Format seconds to human-readable duration, e.g. "12m 34s" */
export function formatEta(seconds: number | null | undefined): string {
  if (seconds == null || seconds <= 0 || seconds >= QBIT_ETA_UNKNOWN) return ''
  return prettyMilliseconds(seconds * 1000, { compact: true })
}
