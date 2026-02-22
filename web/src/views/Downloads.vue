<script setup lang="ts">
import { onMounted } from 'vue'
import { RefreshCw } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { useEventsStore } from '@/stores/events'
import { useDownloadJobsStore, type DownloadJob } from '@/stores/downloadJobs'
import DataTable from '@/components/tables/DataTable.vue'
import {
  downloadJobColumns,
  createDownloadJobActions,
} from '@/components/tables/configs/downloadJobsTableConfig'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import DownloadDetailDrawer from '@/components/downloads/DownloadDetailDrawer.vue'

const events = useEventsStore()
const jobs = useDownloadJobsStore()

const handleViewDetails = (job: DownloadJob) => {
  jobs.openDetailDrawer(job.id)
}

const handleReimport = async (job: DownloadJob, all: boolean) => {
  try {
    const result = await jobs.reimportFailed(job.id, all)
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

const handleCancelJob = async (job: DownloadJob) => {
  try {
    await jobs.cancelJob(job.id)
    toast.success('Download cancelled')
  } catch {
    toast.error('Failed to cancel download')
  }
}

const handleDrawerReimport = (jobId: string, all: boolean) => {
  const job = jobs.getJobById(jobId)
  if (job) {
    handleReimport(job, all)
  }
}

const handleRetry = async (job: DownloadJob) => {
  try {
    await jobs.retryDownload(job.id)
    toast.success('Download retry started')
  } catch {
    toast.error('Failed to retry download')
  }
}

const handleDrawerRetry = (jobId: string) => {
  const job = jobs.getJobById(jobId)
  if (job) {
    handleRetry(job)
  }
}

const handleDrawerCancel = (jobId: string) => {
  const job = jobs.getJobById(jobId)
  if (job) {
    handleCancelJob(job)
  }
}

const jobActions = createDownloadJobActions(handleViewDetails, handleReimport, handleCancelJob, handleRetry)

onMounted(async () => {
  jobs.connectLive()
  await jobs.refresh()
})
</script>

<template>
  <div class="flex flex-col gap-6">
    <div>
      <h1 class="text-2xl font-semibold">Downloads</h1>
    </div>
    <div class="space-y-4">
      <div class="flex items-center justify-between">
        <div class="text-sm text-muted-foreground">
          <span class="font-semibold">Live:</span>
          <span class="ml-2">{{ events.status }}</span>
          <span v-if="events.lastError" class="ml-3 text-destructive">{{ events.lastError }}</span>
        </div>
        <div class="flex items-center gap-2">
          <Button variant="outline" @click="jobs.refresh()">
            <RefreshCw class="mr-2 size-4" />
            Refresh
          </Button>
        </div>
      </div>

      <div v-if="jobs.isLoading" class="space-y-3">
        <Skeleton class="h-12 w-full" />
        <Skeleton class="h-12 w-full" />
        <Skeleton class="h-12 w-full" />
      </div>
      <DataTable
        v-else
        :data="jobs.jobsSorted"
        :columns="downloadJobColumns"
        :actions="jobActions"
        :loading="jobs.isLoading"
        empty-message="No download jobs"
        searchable
        search-placeholder="Search downloads..."
        paginator
        :rows="10"
      />
    </div>

    <!-- Detail Drawer -->
    <DownloadDetailDrawer @reimport="handleDrawerReimport" @cancel="handleDrawerCancel" @retry="handleDrawerRetry" />
  </div>
</template>
