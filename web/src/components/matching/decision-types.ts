// The match-decision endpoint types `resolversConsulted` and `evidence` as
// `unknown` (open-ended, resolver-specific JSON). `resolversConsulted` has a
// stable, useful shape though — the per-resolver score summary the decide pane
// surfaces — so we narrow it here. `evidence` stays unknown and is rendered raw.
export interface ResolverConsulted {
  name: string
  tier: number
  topConfidence: number
  candidateCount: number
}

export function asResolverList(value: unknown): ResolverConsulted[] {
  return Array.isArray(value) ? (value as ResolverConsulted[]) : []
}
