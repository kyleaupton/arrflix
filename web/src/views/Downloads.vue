<script setup lang="ts">
import { onMounted } from 'vue'
import { RefreshCw, Download } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { useEventsStore } from '@/stores/events'
import { useDownloadJobsStore, type DownloadJob } from '@/stores/downloadJobs'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import DownloadSection from '@/components/downloads/DownloadSection.vue'
import DownloadCard from '@/components/downloads/DownloadCard.vue'
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
      <!-- Header bar -->
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-3 text-sm text-muted-foreground">
          <span v-if="jobs.activeJobs.length" class="flex items-center gap-1.5">
            <span class="size-2 rounded-full bg-blue-500" />
            {{ jobs.activeJobs.length }} Active
          </span>
          <span v-if="jobs.importingJobs.length" class="flex items-center gap-1.5">
            <span class="size-2 rounded-full bg-yellow-500" />
            {{ jobs.importingJobs.length }} Importing
          </span>
          <span v-if="jobs.needsAttentionJobs.length" class="flex items-center gap-1.5">
            <span class="size-2 rounded-full bg-red-500" />
            {{ jobs.needsAttentionJobs.length }} Needs Attention
          </span>
          <span v-if="!jobs.jobsSorted.length && !jobs.isLoading" class="text-muted-foreground">
            No downloads
          </span>
        </div>
        <div class="flex items-center gap-2">
          <div class="text-xs text-muted-foreground">
            {{ events.status }}
          </div>
          <Button variant="outline" size="sm" @click="jobs.refresh()">
            <RefreshCw class="mr-2 size-4" />
            Refresh
          </Button>
        </div>
      </div>

      <!-- Loading state -->
      <div v-if="jobs.isLoading" class="space-y-3">
        <Skeleton class="h-20 w-full rounded-lg" />
        <Skeleton class="h-20 w-full rounded-lg" />
        <Skeleton class="h-20 w-full rounded-lg" />
      </div>

      <!-- Empty state -->
      <div
        v-else-if="!jobs.jobsSorted.length"
        class="flex flex-col items-center justify-center py-16 text-muted-foreground"
      >
        <Download class="size-10 mb-3 opacity-50" />
        <p class="text-sm">No download jobs</p>
      </div>

      <!-- Sections -->
      <div v-else class="space-y-6">
        <!-- Needs Attention -->
        <DownloadSection
          title="Needs Attention"
          :count="jobs.needsAttentionJobs.length"
        >
          <DownloadCard
            v-for="job in jobs.needsAttentionJobs"
            :key="job.id"
            :job="job"
            @click="handleViewDetails"
            @cancel="handleCancelJob"
            @retry="handleRetry"
            @reimport="handleReimport"
          />
        </DownloadSection>

        <!-- Active Downloads -->
        <DownloadSection
          title="Active Downloads"
          :count="jobs.activeJobs.length"
        >
          <DownloadCard
            v-for="job in jobs.activeJobs"
            :key="job.id"
            :job="job"
            @click="handleViewDetails"
            @cancel="handleCancelJob"
            @retry="handleRetry"
            @reimport="handleReimport"
          />
        </DownloadSection>

        <!-- Importing -->
        <DownloadSection
          title="Importing"
          :count="jobs.importingJobs.length"
        >
          <DownloadCard
            v-for="job in jobs.importingJobs"
            :key="job.id"
            :job="job"
            @click="handleViewDetails"
            @cancel="handleCancelJob"
            @retry="handleRetry"
            @reimport="handleReimport"
          />
        </DownloadSection>

        <!-- Completed -->
        <DownloadSection
          title="Completed"
          :count="jobs.completedJobs.length"
          collapsible
          :default-open="jobs.completedJobs.length <= 5"
        >
          <DownloadCard
            v-for="job in jobs.completedJobs"
            :key="job.id"
            :job="job"
            @click="handleViewDetails"
            @cancel="handleCancelJob"
            @retry="handleRetry"
            @reimport="handleReimport"
          />
        </DownloadSection>
      </div>
    </div>

    <!-- Detail Drawer -->
    <DownloadDetailDrawer @reimport="handleDrawerReimport" @cancel="handleDrawerCancel" @retry="handleDrawerRetry" />
  </div>
</template>
