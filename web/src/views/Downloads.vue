<script setup lang="ts">
import { computed } from 'vue'
import { Download, AlertTriangle } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { useEventsStore } from '@/stores/events'
import { useDownloadJobsStore, type DownloadFilter } from '@/stores/downloadJobs'
import {
  useDownloadJobsLive,
  useDownloadJobMutations,
  type DownloadJob,
} from '@/composables/useDownloadJobs'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import DownloadCard from '@/components/downloads/DownloadCard.vue'
import DownloadDetailDrawer from '@/components/downloads/DownloadDetailDrawer.vue'

const events = useEventsStore()
const ui = useDownloadJobsStore()
const { isLoading, jobs, activeJobs, needsAttentionJobs, completedJobs } = useDownloadJobsLive()
const { cancelJob, reimportFailed, retryDownload } = useDownloadJobMutations()

// filtered/visible combine the live groups (server state) with the UI filter +
// page size (store state), so they live here at the seam rather than in either.
const filteredJobs = computed(() => {
  switch (ui.filter) {
    case 'active':
      return activeJobs.value
    case 'attention':
      return needsAttentionJobs.value
    case 'completed':
      return completedJobs.value
    default:
      return jobs.value
  }
})
const visibleJobs = computed(() => filteredJobs.value.slice(0, ui.visibleCount))
const hasMore = computed(() => filteredJobs.value.length > ui.visibleCount)

const filters: { key: DownloadFilter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'active', label: 'Active' },
  { key: 'attention', label: 'Needs Attention' },
  { key: 'completed', label: 'Completed' },
]

function filterCount(key: DownloadFilter): number {
  switch (key) {
    case 'active':
      return activeJobs.value.length
    case 'attention':
      return needsAttentionJobs.value.length
    case 'completed':
      return completedJobs.value.length
    default:
      return jobs.value.length
  }
}

const handleViewDetails = (job: DownloadJob) => {
  ui.openDetailDrawer(job.id)
}

const handleReimport = async (jobId: string, all: boolean) => {
  try {
    const result = await reimportFailed(jobId, all)
    const count = result?.created_tasks?.length ?? 0
    if (count > 0) {
      toast.success(`Created ${count} reimport task${count > 1 ? 's' : ''}`)
    } else {
      toast.info('No tasks were reimported')
    }
  } catch {
    toast.error('Failed to reimport tasks')
  }
}

const handleCancelJob = async (jobId: string) => {
  try {
    await cancelJob(jobId)
    toast.success('Download cancelled')
  } catch {
    toast.error('Failed to cancel download')
  }
}

const handleRetry = async (jobId: string) => {
  try {
    await retryDownload(jobId)
    ui.closeDetailDrawer()
    toast.success('Download retry started')
  } catch {
    toast.error('Failed to retry download')
  }
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <div>
      <h1 class="text-2xl font-semibold">Downloads</h1>
    </div>

    <div class="space-y-4">
      <!-- Header bar -->
      <div class="flex items-center justify-between">
        <!-- Filter chips -->
        <div class="flex items-center gap-1.5">
          <button
            v-for="f in filters"
            :key="f.key"
            class="inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium transition-colors"
            :class="
              ui.filter === f.key
                ? 'bg-primary text-primary-foreground'
                : 'bg-muted text-muted-foreground hover:bg-muted/80'
            "
            @click="ui.setFilter(f.key)"
          >
            {{ f.label }}
            <span
              v-if="filterCount(f.key) > 0"
              class="rounded-full px-1.5 py-0.5 text-[10px] leading-none font-semibold"
              :class="
                ui.filter === f.key
                  ? 'bg-primary-foreground/20 text-primary-foreground'
                  : 'bg-foreground/10 text-muted-foreground'
              "
            >
              {{ filterCount(f.key) }}
            </span>
          </button>
        </div>
        <div class="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span
            class="size-2 rounded-full"
            :class="events.isConnected ? 'bg-green-500' : 'bg-red-500'"
          />
          {{
            events.isConnected
              ? 'Live'
              : events.status === 'reconnecting'
                ? 'Reconnecting'
                : 'Disconnected'
          }}
        </div>
      </div>

      <!-- Attention banner -->
      <div
        v-if="needsAttentionJobs.length > 0 && ui.filter !== 'attention'"
        class="flex items-center gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3"
      >
        <AlertTriangle class="size-4 text-destructive shrink-0" />
        <span class="text-sm text-destructive">
          {{ needsAttentionJobs.length }}
          download{{ needsAttentionJobs.length > 1 ? 's' : '' }} need{{
            needsAttentionJobs.length === 1 ? 's' : ''
          }}
          attention
        </span>
        <button
          class="ml-auto text-xs font-medium text-destructive hover:underline"
          @click="ui.setFilter('attention')"
        >
          View
        </button>
      </div>

      <!-- Loading state -->
      <div v-if="isLoading" class="space-y-3">
        <Skeleton class="h-20 w-full rounded-lg" />
        <Skeleton class="h-20 w-full rounded-lg" />
        <Skeleton class="h-20 w-full rounded-lg" />
      </div>

      <!-- Empty state -->
      <div
        v-else-if="!jobs.length"
        class="flex flex-col items-center justify-center py-16 text-muted-foreground"
      >
        <Download class="size-10 mb-3 opacity-50" />
        <p class="text-sm">No download jobs</p>
      </div>

      <!-- Empty filter state -->
      <div
        v-else-if="!filteredJobs.length"
        class="flex flex-col items-center justify-center py-16 text-muted-foreground"
      >
        <p class="text-sm">No {{ ui.filter === 'all' ? '' : ui.filter }} downloads</p>
      </div>

      <!-- Flat job list -->
      <div v-else class="space-y-3">
        <DownloadCard
          v-for="job in visibleJobs"
          :key="job.id"
          :job="job"
          @click="handleViewDetails"
          @cancel="(job) => handleCancelJob(job.id)"
          @retry="(job) => handleRetry(job.id)"
          @reimport="(job, all) => handleReimport(job.id, all)"
        />

        <div v-if="hasMore" class="flex justify-center pt-2">
          <Button variant="ghost" size="sm" @click="ui.showMore()"> Show more </Button>
        </div>
      </div>
    </div>

    <!-- Detail Drawer -->
    <DownloadDetailDrawer
      @reimport="handleReimport"
      @cancel="handleCancelJob"
      @retry="handleRetry"
    />
  </div>
</template>
