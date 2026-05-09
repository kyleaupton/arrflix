import { h } from 'vue'
import { type TableColumn, type TableAction } from '../types'
import { type NameTemplate } from '@/client/types.gen'
import { PrimeIcons } from '@/icons'
import TemplateDisplay from '@/components/ui/template-editor/TemplateDisplay.vue'

export const nameTemplateColumns: TableColumn<NameTemplate>[] = [
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
      const label = value === 'movie' ? 'Movie' : value === 'series' ? 'Series' : value
      return `<span class="inline-flex items-center px-2 py-1 rounded-full text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200">${label}</span>`
    },
  },
  {
    key: 'template',
    label: 'Template',
    sortable: true,
    filterable: true,
    render: (value: string, row: NameTemplate) => {
      if (row.type === 'series') {
        return h(TemplateDisplay, {
          template: value || '',
          seriesTemplates: [
            row.seriesShowTemplate || '',
            row.seriesSeasonTemplate || '',
            value || '',
          ],
          isSeries: true,
        })
      }
      return h(TemplateDisplay, {
        template: value || '',
      })
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

export const createNameTemplateActions = (
  onEdit: (template: NameTemplate) => void,
  onDelete: (template: NameTemplate) => void,
): TableAction<NameTemplate>[] => [
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
