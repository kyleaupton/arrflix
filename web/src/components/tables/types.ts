export type TableColumn<T = unknown> = {
  key: keyof T | string
  label: string
  sortable?: boolean
  filterable?: boolean
  width?: string
  align?: 'left' | 'center' | 'right'
  // value type is column-dependent; impls narrow per-column
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  render?: (value: any, row: T) => unknown
}

export type TableAction<T = unknown> = {
  key: string
  label: string
  icon?: string
  severity?: 'primary' | 'secondary' | 'success' | 'info' | 'warning' | 'danger'
  variant?: 'text' | 'outlined' | 'filled'
  disabled?: (row: T) => boolean
  visible?: (row: T) => boolean
  command: (row: T) => void
}
