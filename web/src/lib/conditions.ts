// Helpers for the routing rule editor's AND-list <-> canonical condition tree
// translation. The v1 editor is a flat list of field/operator/value rows that
// serializes as a single `and`-rooted tree (one row -> that condition as the
// root). Trees authored elsewhere (nested or/not) won't fit the row model;
// treeToRows returns null for those and the editor falls back to raw JSON.
import type { FieldDefinition } from '@/client/types.gen'

export interface ConditionRow {
  field: string
  operator: string
  value: string
}

interface Literal {
  type: 'string' | 'int' | 'float' | 'bool' | 'enum' | 'list'
  str?: string
  int?: number
  float?: number
  bool?: boolean
  list?: string[]
}

interface Operand {
  kind: 'field' | 'literal'
  path?: string
  literal?: Literal
}

interface ConditionNode {
  op: string
  left?: Operand
  right?: Operand
  children?: ConditionNode[]
}

const COMPARISON_OPS = new Set(['==', '!=', '>', '>=', '<', '<=', 'contains', 'in', 'not in'])

function buildLiteral(
  field: FieldDefinition | undefined,
  operator: string,
  value: string,
): Literal {
  if (operator === 'in' || operator === 'not in') {
    return {
      type: 'list',
      list: value
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
    }
  }
  switch (field?.type) {
    case 'number':
      if (field.valueType === 'float64') return { type: 'float', float: Number(value) }
      return { type: 'int', int: Number(value) }
    case 'boolean':
      return { type: 'bool', bool: value === 'true' }
    case 'enum':
      return { type: 'enum', str: value }
    default:
      return { type: 'string', str: value }
  }
}

function literalToString(lit: Literal): string {
  switch (lit.type) {
    case 'int':
      return String(lit.int ?? 0)
    case 'float':
      return String(lit.float ?? 0)
    case 'bool':
      return lit.bool ? 'true' : 'false'
    case 'list':
      return (lit.list ?? []).join(', ')
    default:
      return lit.str ?? ''
  }
}

/** Serialize editor rows into the canonical tree: one row is the root leaf,
 * multiple rows are children of a single `and`. */
export function rowsToTree(rows: ConditionRow[], fields: FieldDefinition[]): ConditionNode | null {
  const leaves = rows.map((row): ConditionNode => {
    const field = fields.find((f) => f.path === row.field)
    return {
      op: row.operator,
      left: { kind: 'field', path: row.field },
      right: { kind: 'literal', literal: buildLiteral(field, row.operator, row.value) },
    }
  })
  if (leaves.length === 0) return null
  if (leaves.length === 1) return leaves[0]!
  return { op: 'and', children: leaves }
}

function leafToRow(node: ConditionNode): ConditionRow | null {
  if (!COMPARISON_OPS.has(node.op)) return null
  if (node.left?.kind !== 'field' || !node.left.path) return null
  if (node.right?.kind !== 'literal' || !node.right.literal) return null
  return {
    field: node.left.path,
    operator: node.op,
    value: literalToString(node.right.literal),
  }
}

/** Parse a stored tree back into editor rows. Returns null when the tree
 * isn't an AND-list of field-vs-literal leaves (the editor then falls back to
 * raw JSON editing). */
export function treeToRows(tree: unknown): ConditionRow[] | null {
  if (!tree || typeof tree !== 'object') return null
  const node = tree as ConditionNode

  if (node.op === 'and' && Array.isArray(node.children)) {
    const rows: ConditionRow[] = []
    for (const child of node.children) {
      const row = leafToRow(child)
      if (!row) return null
      rows.push(row)
    }
    return rows
  }

  const row = leafToRow(node)
  return row ? [row] : null
}

/** One-line human summary of a stored tree, e.g.
 * "Resolution == 2160p AND Seeders > 10". Falls back to a generic label for
 * trees the row model can't express. */
export function describeTree(tree: unknown, fields: FieldDefinition[]): string {
  const rows = treeToRows(tree)
  if (!rows) return 'Custom condition tree'
  if (rows.length === 0) return 'No conditions'
  return rows
    .map((row) => {
      const field = fields.find((f) => f.path === row.field)
      return `${field?.label ?? row.field} ${row.operator} ${row.value}`
    })
    .join(' AND ')
}
