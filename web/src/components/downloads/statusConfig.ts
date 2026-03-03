import type { CircularProgressState } from '@/components/ui/progress'

export const statusConfig: Record<string, { label: string; class: string }> = {
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

export function getStatusConfig(importStatus: string) {
  return statusConfig[importStatus] ?? statusConfig['unknown']!
}

export function getProgressState(importStatus: string): CircularProgressState {
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
