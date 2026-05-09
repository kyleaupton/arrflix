import type {
  Rule,
  Action,
  FieldDefinition,
  Downloader,
  Library,
  NameTemplate,
} from '@/client/types.gen'
import policyOptions from '@/config/policyOptions.json'

function getOperatorLabel(op: string): string {
  const found = policyOptions.operators.find((o) => o.value === op)
  return found ? found.label.toLowerCase() : op
}

function resolveRightOperand(
  field: FieldDefinition | undefined,
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
  downloaders: Downloader[],
  libraries: Library[],
  nameTemplates: NameTemplate[],
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
  rule: Rule | null | undefined,
  actions: Action[],
  fields: FieldDefinition[],
  downloaders: Downloader[],
  libraries: Library[],
  nameTemplates: NameTemplate[],
): string {
  // Backend always emits a rule struct; treat absent id as "no rule"
  const hasRule = !!(rule && rule.id)
  if (!hasRule && actions.length === 0) return 'Unconfigured'

  let ifClause = ''
  if (hasRule) {
    const field = fields.find((f) => f.path === rule!.leftOperand)
    const fieldLabel = field?.label || rule!.leftOperand
    const opLabel = getOperatorLabel(rule!.operator)
    const valueLabel = resolveRightOperand(field, rule!.rightOperand)
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
