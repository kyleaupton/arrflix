import type { ClassValue } from 'clsx'
import { clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Formats runtime in minutes to human-readable string (e.g., "2h 32m")
 */
export function formatRuntime(minutes: number | undefined | null): string {
  if (!minutes || minutes <= 0) return ''
  const hours = Math.floor(minutes / 60)
  const mins = minutes % 60
  if (hours === 0) return `${mins}m`
  if (mins === 0) return `${hours}h`
  return `${hours}h ${mins}m`
}

/**
 * Builds metadata subtitle with bullet separators (e.g., "Movie • 2008 • PG-13 • 2h 32m")
 */
export function buildMetadataSubtitle(parts: {
  mediaType?: 'movie' | 'series'
  year?: string | number
  certification?: string
  runtime?: number | null
}): string {
  const segments: string[] = []
  if (parts.mediaType) segments.push(parts.mediaType === 'series' ? 'Series' : 'Movie')
  if (parts.year) segments.push(String(parts.year))
  if (parts.certification) segments.push(parts.certification)
  if (parts.runtime) {
    const formatted = formatRuntime(parts.runtime)
    if (formatted) segments.push(formatted)
  }
  return segments.join(' \u2022 ')
}
