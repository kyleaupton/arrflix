import type {
  RoutingRule,
  FieldDefinition,
  Downloader,
  Library,
  NameTemplate,
} from '@/client/types.gen'
import { describeTree } from '@/lib/conditions'

export function buildRoutingSummary(
  rule: RoutingRule,
  fields: FieldDefinition[],
  downloaders: Downloader[],
  libraries: Library[],
  nameTemplates: NameTemplate[],
): string {
  const ifClause = `IF ${describeTree(rule.conditions, fields)}`

  const parts: string[] = []
  if (rule.downloaderId) {
    const d = downloaders.find((x) => x.id === rule.downloaderId)
    parts.push(`use ${d?.name ?? rule.downloaderId}`)
  }
  if (rule.libraryId) {
    const l = libraries.find((x) => x.id === rule.libraryId)
    parts.push(`save to ${l?.name ?? rule.libraryId}`)
  }
  if (rule.nameTemplateId) {
    const nt = nameTemplates.find((x) => x.id === rule.nameTemplateId)
    parts.push(`name with ${nt?.name ?? rule.nameTemplateId}`)
  }
  if (rule.continue) {
    parts.push('continue')
  }

  const thenClause = parts.length > 0 ? `THEN ${parts.join(', ')}` : 'No actions configured'
  return `${ifClause} → ${thenClause}`
}
