import { type TableColumn } from '../types'
import type { FileInfo } from '@/client/types.gen'
import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import LibraryReference from '@/components/references/LibraryReference.vue'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Loader2 } from 'lucide-vue-next'
import { h } from 'vue'

export const movieFilesColumns: TableColumn<FileInfo>[] = [
  {
    key: 'path',
    label: 'Path',
    sortable: true,
    filterable: true,
    render: (value: string, row: FileInfo) => {
      const isPredicted = value && value.includes('.{ext}')

      if (isPredicted) {
        return h(
          Tooltip,
          {},
          {
            default: () => [
              h(
                TooltipTrigger,
                { asChild: true },
                {
                  default: () =>
                    h(
                      'span',
                      { class: 'font-mono text-sm text-muted-foreground cursor-help' },
                      value || '',
                    ),
                },
              ),
              h(
                TooltipContent,
                {},
                {
                  default: () => "File doesn't exist yet. This is the predicted path.",
                },
              ),
            ],
          },
        )
      }

      return h('span', { class: 'font-mono text-sm' }, value || '')
    },
  },
  {
    key: 'libraryId',
    label: 'Library',
    sortable: true,
    filterable: true,
    width: '200px',
    render: (value: string) => {
      return h(LibraryReference, { libraryId: value })
    },
  },
  {
    key: 'status',
    label: 'Status',
    sortable: true,
    filterable: true,
    width: '140px',
    render: (value: string, row: FileInfo) => {
      const statusColors: Record<string, string> = {
        available: 'bg-green-500 text-white',
        downloading: 'bg-blue-500 text-white',
        importing: 'bg-yellow-500 text-white',
        missing: 'bg-gray-500 text-white',
        failed: 'bg-red-500 text-white',
        deleted: 'bg-gray-600 text-white',
      }
      const colorClass = statusColors[value] || 'bg-gray-500 text-white'

      // Show spinner for downloading/importing status
      const showSpinner = value === 'downloading' || value === 'importing'

      return h(
        Badge,
        {
          class: `${colorClass} capitalize border-transparent flex items-center gap-1.5`,
        },
        () => [showSpinner ? h(Loader2, { class: 'size-3 animate-spin' }) : null, value],
      )
    },
  },
  {
    key: 'progress',
    label: 'Progress',
    sortable: true,
    width: '220px',
    render: (value: number | null | undefined, row: FileInfo) => {
      // Only show progress if downloadJobId exists and progress is available
      if (!row.downloadJobId || value === null || value === undefined) {
        return h('span', { class: 'text-xs text-muted-foreground' }, '-')
      }
      const progressValue = Math.round(value * 100)
      return h('div', { class: 'flex items-center gap-2' }, [
        h(Progress, { modelValue: progressValue, class: 'flex-1' }),
        h('span', { class: 'text-xs text-muted-foreground min-w-[3ch]' }, `${progressValue}%`),
      ])
    },
  },
]
