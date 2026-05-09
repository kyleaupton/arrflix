import { type TableColumn, type TableAction } from '../types'
import { type IndexerOutput } from '@/client/types.gen'
import { PrimeIcons } from '@/icons'

export const indexerColumns: TableColumn<IndexerOutput>[] = [
  {
    key: 'name',
    label: 'Name',
    sortable: true,
    filterable: true,
    // width: '200px',
  },
  {
    key: 'description',
    label: 'Description',
    sortable: true,
    filterable: true,
    // width: '300px',
  },
  {
    key: 'protocol',
    label: 'Type',
    sortable: true,
    filterable: true,
    // width: '120px',
  },
  {
    key: 'enable',
    label: 'Status',
    sortable: true,
    width: '100px',
    // align: 'center',
    render: (value: boolean) => {
      return value
        ? '<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-green-100 text-green-800">Enabled</span>'
        : '<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-gray-100 text-gray-800">Disabled</span>'
    },
  },
]

export const createIndexerActions = (
  onEdit: (indexer: IndexerOutput) => void,
  onTest: (indexer: IndexerOutput) => void,
  onToggle: (indexer: IndexerOutput) => void,
  onDelete: (indexer: IndexerOutput) => void,
): TableAction<IndexerOutput>[] => [
  {
    key: 'edit',
    label: 'Edit',
    icon: PrimeIcons.PENCIL,
    severity: 'primary',
    variant: 'text',
    command: onEdit,
  },
  {
    key: 'test',
    label: 'Test',
    icon: PrimeIcons.CHECK,
    severity: 'secondary',
    variant: 'text',
    command: onTest,
  },
  {
    key: 'toggle',
    label: 'Toggle',
    icon: PrimeIcons.POWER_OFF,
    severity: 'secondary',
    variant: 'text',
    command: onToggle,
  },
  {
    key: 'delete',
    label: 'Delete',
    icon: PrimeIcons.TRASH,
    severity: 'danger',
    variant: 'text',
    command: onDelete,
  },
]
