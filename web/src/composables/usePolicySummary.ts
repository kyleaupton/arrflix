import type {
  DbgenRule,
  DbgenAction,
  ModelFieldDefinition,
  DbgenDownloader,
  HandlersLibrarySwagger,
  HandlersNameTemplateSwagger,
} from '@/client/types.gen'
import policyOptions from '@/config/policyOptions.json'

function getOperatorLabel(op: string): string {
  const found = policyOptions.operators.find((o) => o.value === op)
  return found ? found.label.toLowerCase() : op
}

function resolveRightOperand(
  field: ModelFieldDefinition | undefined,
  value: string,
): string {
  if (!field) return value

  if ((field.type === 'enum' || field.type === 'boolean') && field.enumValues) {
    const found = field.enumValues.find((ev) => ev.value === value)
    if (found) return found.label
  }

  return value
}

function resolveActionValue(
  actionType: string,
  value: string,
  downloaders: DbgenDownloader[],
  libraries: HandlersLibrarySwagger[],
  nameTemplates: HandlersNameTemplateSwagger[],
): string {
  switch (actionType) {
    case 'set_downloader': {
      const d = downloaders.find((x) => x.id === value)
      return d ? d.name : value
    }
    case 'set_library': {
      const l = libraries.find((x) => x.id === value)
      return l ? l.name : value
    }
    case 'set_name_template': {
      const nt = nameTemplates.find((x) => x.id === value)
      return nt ? nt.name : value
    }
    default:
      return value
  }
}

const actionVerbs: Record<string, string> = {
  set_downloader: 'use',
  set_library: 'save to',
  set_name_template: 'name with',
  stop_processing: 'stop processing',
}

export function buildPolicySummary(
  rule: DbgenRule | null | undefined,
  actions: DbgenAction[],
  fields: ModelFieldDefinition[],
  downloaders: DbgenDownloader[],
  libraries: HandlersLibrarySwagger[],
  nameTemplates: HandlersNameTemplateSwagger[],
): string {
  if (!rule && actions.length === 0) return 'Unconfigured'

  let ifClause = ''
  if (rule) {
    const field = fields.find((f) => f.path === rule.left_operand)
    const fieldLabel = field?.label || rule.left_operand
    const opLabel = getOperatorLabel(rule.operator)
    const valueLabel = resolveRightOperand(field, rule.right_operand)
    ifClause = `IF ${fieldLabel} ${opLabel} ${valueLabel}`
  } else {
    ifClause = 'No condition configured'
  }

  let thenClause = ''
  if (actions.length > 0) {
    const parts = actions.map((a) => {
      const verb = actionVerbs[a.type] || a.type
      if (a.type === 'stop_processing') return verb
      const valueName = resolveActionValue(a.type, a.value, downloaders, libraries, nameTemplates)
      return `${verb} ${valueName}`
    })
    thenClause = `THEN ${parts.join(', ')}`
  } else {
    thenClause = 'No actions configured'
  }

  return `${ifClause} → ${thenClause}`
}
