import { type TableColumn, type TableAction } from '../types'
import type { DownloadJob } from '@/stores/downloadJobs'
import { Badge } from '@/components/ui/badge'
import { CircularProgress, type CircularProgressState } from '@/components/ui/progress'
import { h } from 'vue'

function getProgressState(importStatus: string): CircularProgressState {
  switch (importStatus) {
    case 'download_pending':
      return 'progress'
    case 'importing':
    case 'awaiting_import':
      return 'indeterminate'
    case 'fully_imported':
      return 'success'
    case 'import_failed':
    case 'download_failed':
    case 'partial_failure':
      return 'error'
    case 'download_cancelled':
      return 'cancelled'
    default:
      return 'indeterminate'
  }
}

const statusConfig: Record<string, { label: string; class: string }> = {
  download_pending: { label: 'Downloading', class: 'bg-blue-500 text-white' },
  download_failed: { label: 'Download Failed', class: 'bg-red-500 text-white' },
  download_cancelled: { label: 'Cancelled', class: 'bg-gray-600 text-white' },
  awaiting_import: { label: 'Awaiting Import', class: 'bg-yellow-500 text-white' },
  importing: { label: 'Importing', class: 'bg-yellow-600 text-white' },
  partial_failure: { label: 'Partial Failure', class: 'bg-orange-500 text-white' },
  import_failed: { label: 'Import Failed', class: 'bg-red-500 text-white' },
  fully_imported: { label: 'Imported', class: 'bg-green-500 text-white' },
  unknown: { label: 'Unknown', class: 'bg-gray-500 text-white' },
}

export const downloadJobColumns: TableColumn<DownloadJob>[] = [
  {
    key: 'candidate_title',
    label: 'Title',
    sortable: true,
    filterable: true,
    render: (value: string, row: DownloadJob) => {
      return h('div', { class: 'flex flex-col gap-1' }, [
        h('div', { class: 'font-medium' }, value || ''),
        h('div', { class: 'text-xs text-muted-foreground' }, `${row.protocol} • ${row.id.slice(0, 8)}`),
      ])
    },
  },
  {
    key: 'import_status',
    label: 'Status',
    sortable: true,
    filterable: true,
    width: '150px',
    render: (_value: string, row: DownloadJob) => {
      const config = statusConfig[row.import_status] || statusConfig['unknown']!
      return h(
        Badge,
        {
          class: `${config.class} border-transparent`,
        },
        () => config.label,
      )
    },
  },
  {
    key: 'progress',
    label: '',
    sortable: false,
    width: '50px',
    render: (_value: number | null | undefined, row: DownloadJob) => {
      const state = getProgressState(row.import_status)
      const value =
        row.import_status === 'download_pending' ? Math.round((row.progress ?? 0) * 100) : undefined

      return h(CircularProgress, {
        state,
        value,
        size: 'sm',
      })
    },
  },
]

export const createDownloadJobActions = (
  onViewDetails: (job: DownloadJob) => void,
  onReimport: (job: DownloadJob, all: boolean) => void,
  onCancel: (job: DownloadJob) => void,
  onRetry: (job: DownloadJob) => void,
): TableAction<DownloadJob>[] => [
  {
    key: 'view_details',
    label: 'View Details',
    command: onViewDetails,
  },
  {
    key: 'reimport_failed',
    label: 'Re-import Failed',
    visible: (row: DownloadJob) => {
      return ['partial_failure', 'import_failed'].includes(row.import_status)
    },
    command: (job: DownloadJob) => onReimport(job, false),
  },
  {
    key: 'reimport_all',
    label: 'Re-import All',
    visible: (row: DownloadJob) => {
      return ['partial_failure', 'import_failed', 'fully_imported'].includes(row.import_status)
    },
    command: (job: DownloadJob) => onReimport(job, true),
  },
  {
    key: 'retry',
    label: 'Retry Download',
    visible: (row: DownloadJob) => {
      return row.import_status === 'download_failed'
    },
    command: onRetry,
  },
  {
    key: 'cancel',
    label: 'Cancel',
    severity: 'danger',
    visible: (row: DownloadJob) => {
      return row.import_status === 'download_pending'
    },
    command: onCancel,
  },
]
