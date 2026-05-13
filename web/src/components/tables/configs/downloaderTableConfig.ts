import { type TableColumn, type TableAction } from '../types'
import { type Downloader } from '@/client/types.gen'
import { PrimeIcons } from '@/icons'

export const downloaderColumns: TableColumn<Downloader>[] = [
  {
    key: 'name',
    label: 'Name',
    sortable: true,
    filterable: true,
  },
  {
    key: 'type',
    label: 'Type',
    sortable: true,
    filterable: true,
    width: '120px',
    render: (value: string) => {
      return `<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200">${value}</span>`
    },
  },
  {
    key: 'protocol',
    label: 'Protocol',
    sortable: true,
    filterable: true,
    width: '120px',
    render: (value: string) => {
      const label = value === 'torrent' ? 'Torrent' : value === 'usenet' ? 'Usenet' : value
      return `<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200">${label}</span>`
    },
  },
  {
    key: 'url',
    label: 'URL',
    sortable: true,
    filterable: true,
    render: (value: string) => {
      return `<span class="font-mono text-sm">${value || ''}</span>`
    },
  },
  {
    key: 'enabled',
    label: 'Status',
    sortable: true,
    width: '120px',
    align: 'center',
    render: (value: boolean, row: Downloader & { initialized?: boolean }) => {
      const initialized = row.initialized ?? false
      if (!value) {
        return '<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300">Disabled</span>'
      }
      if (initialized) {
        return '<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200">Active</span>'
      }
      return '<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200">Inactive</span>'
    },
  },
  {
    key: 'default',
    label: 'Default',
    sortable: true,
    width: '100px',
    align: 'center',
    render: (value: boolean) => {
      return value
        ? '<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200">Yes</span>'
        : '<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300">No</span>'
    },
  },
]

export const createDownloaderActions = (
  onTest: (downloader: Downloader) => void,
  onEdit: (downloader: Downloader) => void,
  onDelete: (downloader: Downloader) => void,
): TableAction<Downloader>[] => [
  {
    key: 'test',
    label: 'Test',
    icon: PrimeIcons.CHECK,
    severity: 'secondary',
    variant: 'text',
    command: onTest,
  },
  {
    key: 'edit',
    label: 'Edit',
    icon: PrimeIcons.PENCIL,
    severity: 'primary',
    variant: 'text',
    command: onEdit,
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
