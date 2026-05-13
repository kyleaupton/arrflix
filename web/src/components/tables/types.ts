export type TableColumn<T = any> = {
  key: keyof T | string
  label: string
  sortable?: boolean
  filterable?: boolean
  width?: string
  align?: 'left' | 'center' | 'right'
  render?: (value: any, row: T) => any
}

export type TableAction<T = any> = {
  key: string
  label: string
  icon?: string
  severity?: 'primary' | 'secondary' | 'success' | 'info' | 'warning' | 'danger'
  variant?: 'text' | 'outlined' | 'filled'
  disabled?: (row: T) => boolean
  visible?: (row: T) => boolean
  command: (row: T) => void
}
