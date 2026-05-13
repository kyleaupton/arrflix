<script setup lang="ts">
import { computed, watch } from 'vue'
import { useDownloadJobsStore } from '@/stores/downloadJobs'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
} from '@/components/ui/sheet'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { ScrollArea } from '@/components/ui/scroll-area'
import { AlertCircle, FileIcon, Film, Tv, RefreshCw, XCircle } from 'lucide-vue-next'
import type { ImportTask } from '@/client/types.gen'
import { statusConfig } from './statusConfig'
import { formatBytes, formatSpeed, formatEta } from '@/lib/format'

const jobs = useDownloadJobsStore()

const emit = defineEmits<{
  (e: 'reimport', jobId: string, all: boolean): void
  (e: 'cancel', jobId: string): void
  (e: 'retry', jobId: string): void
}>()

// Import task status config
const taskStatusConfig: Record<string, { label: string; class: string }> = {
  pending: { label: 'Pending', class: 'bg-gray-500 text-white' },
  in_progress: { label: 'In Progress', class: 'bg-blue-500 text-white' },
  completed: { label: 'Completed', class: 'bg-green-500 text-white' },
  failed: { label: 'Failed', class: 'bg-red-500 text-white' },
  cancelled: { label: 'Cancelled', class: 'bg-gray-600 text-white' },
}

const job = computed(() => jobs.selectedJob)
const isOpen = computed(() => jobs.isDetailDrawerOpen)

const statusLabel = computed(() => {
  if (!job.value) return 'Unknown'
  return statusConfig[job.value.importStatus]?.label ?? 'Unknown'
})

const statusClass = computed(() => {
  if (!job.value) return statusConfig['unknown']!.class
  return statusConfig[job.value.importStatus]?.class ?? statusConfig['unknown']!.class
})

const canCancel = computed(() => {
  return job.value?.importStatus === 'download_pending'
})

const canRetry = computed(() => {
  return job.value?.importStatus === 'download_failed'
})

const canReimport = computed(() => {
  if (!job.value) return false
  return ['partial_failure', 'import_failed', 'fully_imported'].includes(job.value.importStatus)
})

const hasFailedImports = computed(() => {
  return (job.value?.failedImports ?? 0) > 0
})

const posterUrl = computed(() => {
  if (!job.value?.mediaPosterPath) return ''
  return `https://image.tmdb.org/t/p/w185${job.value.mediaPosterPath}`
})

const drawerTitle = computed(() => {
  if (!job.value) return 'Download Details'
  return job.value.mediaTitle || job.value.candidateTitle
})

const drawerSubtitle = computed(() => {
  if (!job.value) return ''
  const parts: string[] = []
  if (job.value.mediaYear) parts.push(String(job.value.mediaYear))
  if (job.value.mediaCertification) parts.push(job.value.mediaCertification)
  if (job.value.mediaType === 'series' && job.value.seasonNumber) {
    const ep = job.value.episodeNumber ? `E${String(job.value.episodeNumber).padStart(2, '0')}` : ''
    parts.push(`S${String(job.value.seasonNumber).padStart(2, '0')}${ep}`)
  }
  return parts.join(' \u00B7 ')
})

// Refresh import tasks when job updates
watch(
  () => job.value?.id,
  (newId) => {
    if (newId && isOpen.value) {
      jobs.loadImportTasks(newId)
    }
  },
)

function handleOpenChange(open: boolean) {
  if (!open) {
    jobs.closeDetailDrawer()
  }
}

function handleCancel() {
  if (job.value) {
    emit('cancel', job.value.id)
  }
}

function handleRetry() {
  if (job.value) {
    emit('retry', job.value.id)
  }
}

function handleReimportFailed() {
  if (job.value) {
    emit('reimport', job.value.id, false)
  }
}

function handleReimportAll() {
  if (job.value) {
    emit('reimport', job.value.id, true)
  }
}

function getTaskFilename(task: ImportTask): string {
  const path = task.sourcePath || ''
  return path.split('/').pop() || path
}

function getTaskStatusConfig(status: string) {
  return taskStatusConfig[status] ?? taskStatusConfig['pending']!
}
</script>

<template>
  <Sheet :open="isOpen" @update:open="handleOpenChange">
    <SheetContent side="right" class="w-full sm:max-w-lg flex flex-col overflow-hidden">
      <SheetHeader class="px-6 pt-6">
        <div class="flex gap-4">
          <!-- Poster -->
          <div class="w-20 shrink-0 rounded overflow-hidden bg-muted aspect-[2/3]">
            <img
              v-if="posterUrl"
              :src="posterUrl"
              :alt="drawerTitle"
              class="w-full h-full object-cover"
            />
            <div v-else class="w-full h-full flex items-center justify-center">
              <component
                :is="job?.mediaType === 'series' ? Tv : Film"
                class="size-6 text-muted-foreground"
              />
            </div>
          </div>
          <div class="min-w-0 flex-1">
            <SheetTitle class="truncate">{{ drawerTitle }}</SheetTitle>
            <p v-if="drawerSubtitle" class="text-xs text-muted-foreground mt-0.5">
              {{ drawerSubtitle }}
            </p>
            <SheetDescription class="flex items-center gap-2 mt-1.5">
              <Badge :class="`${statusClass} border-transparent`">
                {{ statusLabel }}
              </Badge>
              <span class="text-muted-foreground">{{ job?.protocol }}</span>
            </SheetDescription>
          </div>
        </div>
      </SheetHeader>

      <ScrollArea class="flex-1 min-h-0 px-6">
        <div class="space-y-6 py-4">
          <!-- Release name -->
          <div v-if="job?.candidateTitle" class="space-y-2">
            <h4 class="text-sm font-medium">Release</h4>
            <p class="text-xs text-muted-foreground break-all font-mono bg-muted p-2 rounded">
              {{ job.candidateTitle }}
            </p>
          </div>

          <!-- Progress Section (during download) -->
          <div v-if="job?.importStatus === 'download_pending'" class="space-y-2">
            <h4 class="text-sm font-medium">Download Progress</h4>
            <div class="flex items-center gap-2">
              <Progress :model-value="Math.round((job?.progress ?? 0) * 100)" class="flex-1" />
              <span class="text-sm text-muted-foreground">
                {{ Math.round((job?.progress ?? 0) * 100) }}%
              </span>
            </div>
            <div class="flex items-center gap-3 text-xs text-muted-foreground">
              <span v-if="formatSpeed(job?.downloadSpeed)">{{
                formatSpeed(job?.downloadSpeed)
              }}</span>
              <span v-if="formatEta(job?.etaSeconds)">ETA: {{ formatEta(job?.etaSeconds) }}</span>
              <span v-if="formatBytes(job?.totalSize)">{{ formatBytes(job?.totalSize) }}</span>
            </div>
            <p v-if="job?.downloaderStatus" class="text-xs text-muted-foreground">
              Downloader status: {{ job.downloaderStatus }}
            </p>
          </div>

          <!-- Import Progress (after download) -->
          <div v-else-if="job && job.totalImportTasks > 0" class="space-y-2">
            <h4 class="text-sm font-medium">Import Progress</h4>
            <div class="flex items-center gap-2">
              <Progress
                :model-value="Math.round((job.completedImports / job.totalImportTasks) * 100)"
                class="flex-1"
              />
              <span class="text-sm text-muted-foreground">
                {{ job.completedImports }}/{{ job.totalImportTasks }} files
              </span>
            </div>
          </div>

          <!-- Source Path -->
          <div v-if="job?.contentPath" class="space-y-2">
            <h4 class="text-sm font-medium">Source Path</h4>
            <p class="text-xs text-muted-foreground break-all font-mono bg-muted p-2 rounded">
              {{ job.contentPath }}
            </p>
          </div>

          <!-- Error Section -->
          <div
            v-if="job?.lastError"
            class="space-y-2 p-3 bg-destructive/10 border border-destructive/20 rounded-lg"
          >
            <div class="flex items-center gap-2 text-destructive">
              <AlertCircle class="size-4" />
              <h4 class="text-sm font-medium">Error</h4>
            </div>
            <p class="text-xs text-destructive break-all">
              {{ job.lastError }}
            </p>
          </div>

          <!-- Import Tasks Section -->
          <div class="space-y-3">
            <h4 class="text-sm font-medium">Import Tasks</h4>

            <!-- Loading state -->
            <div v-if="jobs.isLoadingImportTasks" class="space-y-2">
              <Skeleton class="h-16 w-full" />
              <Skeleton class="h-16 w-full" />
            </div>

            <!-- Empty state -->
            <div
              v-else-if="jobs.drawerImportTasks.length === 0"
              class="text-sm text-muted-foreground py-4 text-center"
            >
              No import tasks yet
            </div>

            <!-- Task list -->
            <div v-else class="space-y-2">
              <div
                v-for="task in jobs.drawerImportTasks"
                :key="task.id"
                class="p-3 border rounded-lg space-y-2"
                :class="{
                  'border-destructive/50 bg-destructive/5': task.status === 'failed',
                }"
              >
                <div class="flex items-start justify-between gap-2">
                  <div class="flex items-center gap-2 min-w-0">
                    <FileIcon class="size-4 shrink-0 text-muted-foreground" />
                    <span class="text-sm font-medium truncate">
                      {{ getTaskFilename(task) }}
                    </span>
                  </div>
                  <Badge
                    :class="`${getTaskStatusConfig(task.status).class} border-transparent shrink-0`"
                  >
                    {{ getTaskStatusConfig(task.status).label }}
                  </Badge>
                </div>

                <!-- Destination path for completed -->
                <p
                  v-if="task.destPath"
                  class="text-xs text-muted-foreground break-all font-mono bg-muted p-1.5 rounded"
                >
                  {{ task.destPath }}
                </p>

                <!-- Error for failed tasks -->
                <div
                  v-if="task.status === 'failed' && task.lastError"
                  class="flex items-start gap-2"
                >
                  <XCircle class="size-3 text-destructive shrink-0 mt-0.5" />
                  <p class="text-xs text-destructive break-all">
                    {{ task.lastError }}
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </ScrollArea>

      <SheetFooter class="px-6 pb-6 pt-4 border-t flex-row gap-2">
        <Button v-if="canRetry" variant="outline" size="sm" @click="handleRetry">
          <RefreshCw class="mr-2 size-4" />
          Retry Download
        </Button>
        <Button v-if="canCancel" variant="destructive" size="sm" @click="handleCancel">
          Cancel Download
        </Button>
        <Button
          v-if="canReimport && hasFailedImports"
          variant="outline"
          size="sm"
          @click="handleReimportFailed"
        >
          <RefreshCw class="mr-2 size-4" />
          Re-import Failed
        </Button>
        <Button v-if="canReimport" variant="outline" size="sm" @click="handleReimportAll">
          <RefreshCw class="mr-2 size-4" />
          Re-import All
        </Button>
      </SheetFooter>
    </SheetContent>
  </Sheet>
</template>
