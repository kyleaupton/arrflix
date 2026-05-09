import { type TableColumn, type TableAction } from '../types'
import { type IndexerOutput } from '@/client/types.gen'
import { PrimeIcons } from '@/icons'

export const availableIndexerColumns: TableColumn<IndexerOutput>[] = [
  {
    key: 'name',
    label: 'Name',
    width: '100px',
    sortable: true,
    filterable: true,
  },
  {
    key: 'description',
    label: 'Description',
  },
  {
    key: 'language',
    label: 'Language',
    filterable: true,
    width: '120px',
    align: 'center',
    render: (value: string) => {
      return `<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-blue-100 text-blue-800">${value}</span>`
    },
  },
]

export const createAvailableIndexerActions = (
  onSelect: (indexer: IndexerOutput) => void,
): TableAction<IndexerOutput>[] => [
  {
    key: 'select',
    label: 'Select',
    icon: PrimeIcons.CHECK,
    severity: 'primary',
    variant: 'outlined',
    command: onSelect,
  },
]
