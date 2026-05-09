<script setup lang="ts">
import { computed } from 'vue'
import { Film, Tv, RefreshCw, RotateCw } from 'lucide-vue-next'
import type { DownloadJob } from '@/stores/downloadJobs'
import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { Button } from '@/components/ui/button'
import { getStatusConfig } from './statusConfig'
import { formatBytes, formatSpeed, formatEta } from '@/lib/format'

const props = defineProps<{
  job: DownloadJob
}>()

const emit = defineEmits<{
  (e: 'click', job: DownloadJob): void
  (e: 'cancel', job: DownloadJob): void
  (e: 'retry', job: DownloadJob): void
  (e: 'reimport', job: DownloadJob, all: boolean): void
}>()

const status = computed(() => getStatusConfig(props.job.importStatus))
const progressPercent = computed(() => Math.round((props.job.progress ?? 0) * 100))
const isActive = computed(() => props.job.importStatus === 'download_pending')
const isImporting = computed(() =>
  ['awaiting_import', 'importing'].includes(props.job.importStatus),
)
const isFailed = computed(() =>
  ['download_failed', 'partial_failure', 'import_failed'].includes(props.job.importStatus),
)

const importPercent = computed(() => {
  if (!props.job.totalImportTasks) return 0
  return Math.round((props.job.completedImports / props.job.totalImportTasks) * 100)
})

const posterUrl = computed(() => {
  if (!props.job.mediaPosterPath) return ''
  return `https://image.tmdb.org/t/p/w185${props.job.mediaPosterPath}`
})

const subtitle = computed(() => {
  const parts: string[] = []
  if (props.job.mediaYear) parts.push(String(props.job.mediaYear))
  if (props.job.mediaCertification) parts.push(props.job.mediaCertification)
  if (props.job.mediaType === 'series' && props.job.seasonNumber) {
    const ep = props.job.episodeNumber
      ? `E${String(props.job.episodeNumber).padStart(2, '0')}`
      : ''
    parts.push(`S${String(props.job.seasonNumber).padStart(2, '0')}${ep}`)
  }
  return parts.join(' \u00B7 ')
})

const statsLine = computed(() => {
  const parts: string[] = []
  const speed = formatSpeed(props.job.downloadSpeed)
  if (speed) parts.push(speed)
  const eta = formatEta(props.job.etaSeconds)
  if (eta) parts.push(eta)
  const size = formatBytes(props.job.totalSize)
  if (size) parts.push(size)
  return parts.join('  \u00B7  ')
})
</script>

<template>
  <div
    class="flex gap-3 p-3 rounded-lg border bg-card cursor-pointer transition-colors hover:bg-accent/50"
    @click="emit('click', job)"
  >
    <!-- Poster -->
    <div class="w-[4.5rem] shrink-0 rounded overflow-hidden bg-muted aspect-[2/3]">
      <img
        v-if="posterUrl"
        :src="posterUrl"
        :alt="job.mediaTitle || job.candidateTitle"
        class="w-full h-full object-cover"
      />
      <div v-else class="w-full h-full flex items-center justify-center">
        <component
          :is="job.mediaType === 'series' ? Tv : Film"
          class="size-5 text-muted-foreground"
        />
      </div>
    </div>

    <!-- Content -->
    <div class="flex-1 min-w-0 flex flex-col gap-1.5">
      <!-- Title row -->
      <div class="flex items-start justify-between gap-2">
        <div class="min-w-0">
          <p class="font-medium text-sm truncate">
            {{ job.mediaTitle || job.candidateTitle }}
          </p>
          <p v-if="subtitle" class="text-xs text-muted-foreground">
            {{ subtitle }}
          </p>
        </div>
        <Badge :class="`${status.class} border-transparent shrink-0`">
          {{ status.label }}
        </Badge>
      </div>

      <!-- Download progress (active) -->
      <div v-if="isActive" class="space-y-1">
        <div class="flex items-center gap-2">
          <Progress :model-value="progressPercent" class="flex-1" />
          <span class="text-xs text-muted-foreground tabular-nums w-8 text-right">
            {{ progressPercent }}%
          </span>
        </div>
        <p v-if="statsLine" class="text-xs text-muted-foreground">
          {{ statsLine }}
        </p>
      </div>

      <!-- Import progress -->
      <div v-else-if="isImporting && job.totalImportTasks > 0" class="space-y-1">
        <div class="flex items-center gap-2">
          <Progress :model-value="importPercent" class="flex-1" />
          <span class="text-xs text-muted-foreground tabular-nums">
            {{ job.completedImports }}/{{ job.totalImportTasks }}
          </span>
        </div>
      </div>

      <!-- Error info -->
      <p
        v-if="isFailed && job.lastError"
        class="text-xs text-destructive line-clamp-1"
      >
        {{ job.lastError }}
      </p>

      <!-- Release name (muted, always visible) -->
      <p class="text-xs text-muted-foreground/60 truncate">
        {{ job.candidateTitle }}
      </p>

      <!-- Inline action buttons for failed states -->
      <div v-if="isFailed" class="flex items-center gap-2" @click.stop>
        <Button
          v-if="job.importStatus === 'download_failed'"
          variant="outline"
          size="sm"
          class="h-7 text-xs"
          @click="emit('retry', job)"
        >
          <RefreshCw class="mr-1.5 size-3" />
          Retry
        </Button>
        <Button
          v-if="['partial_failure', 'import_failed'].includes(job.importStatus)"
          variant="outline"
          size="sm"
          class="h-7 text-xs"
          @click="emit('reimport', job, false)"
        >
          <RotateCw class="mr-1.5 size-3" />
          Re-import
        </Button>
      </div>
    </div>
  </div>
</template>
