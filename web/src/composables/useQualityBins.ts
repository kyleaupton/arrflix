import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { qualityListBinsOptions } from '@/client/@tanstack/vue-query.gen'
import type { BinKey, QualityBinOption } from '@/client/types.gen'

/** Two bins share identity when their (Source, Resolution, Modifier) triple
 * matches; the rendered Name is display-only and never participates. */
export function binKeyEquals(a: BinKey, b: BinKey): boolean {
  return a.Source === b.Source && a.Resolution === b.Resolution && a.Modifier === b.Modifier
}

/** A bin's display name from its identity, falling back to a joined triple when
 * the vocabulary hasn't loaded or doesn't carry the bin. */
export function renderBinKey(bin: BinKey, vocabulary: QualityBinOption[]): string {
  const found = vocabulary.find((o) => binKeyEquals(o.bin, bin))
  if (found) return found.name
  return [bin.Source, bin.Resolution, bin.Modifier].filter(Boolean).join(' ') || 'Unknown'
}

/**
 * useQualityBins wraps the per-domain bin vocabulary endpoint and exposes the
 * two lookups the profile editor needs: label(bin) renders a bin's identity to
 * its display name, and available(current) returns the vocabulary minus the
 * already-ranked bins (both keyed on the identity triple, never the name).
 */
export function useQualityBins(domain: MaybeRefOrGetter<'movie' | 'series'>) {
  const query = useQuery(
    computed(() => qualityListBinsOptions({ query: { domain: toValue(domain) } })),
  )

  const vocabulary = computed<QualityBinOption[]>(() => query.data.value ?? [])

  const label = (bin: BinKey): string => renderBinKey(bin, vocabulary.value)

  const available = (current: BinKey[]): QualityBinOption[] =>
    vocabulary.value.filter((o) => !current.some((c) => binKeyEquals(c, o.bin)))

  return {
    vocabulary,
    isLoading: query.isLoading,
    label,
    available,
  }
}
