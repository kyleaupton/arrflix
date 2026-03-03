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

const status = computed(() => getStatusConfig(props.job.import_status))
const progressPercent = computed(() => Math.round((props.job.progress ?? 0) * 100))
const isActive = computed(() => props.job.import_status === 'download_pending')
const isImporting = computed(() =>
  ['awaiting_import', 'importing'].includes(props.job.import_status),
)
const isFailed = computed(() =>
  ['download_failed', 'partial_failure', 'import_failed'].includes(props.job.import_status),
)

const importPercent = computed(() => {
  if (!props.job.total_import_tasks) return 0
  return Math.round((props.job.completed_imports / props.job.total_import_tasks) * 100)
})

const posterUrl = computed(() => {
  if (!props.job.media_poster_path) return ''
  return `https://image.tmdb.org/t/p/w185${props.job.media_poster_path}`
})

const subtitle = computed(() => {
  const parts: string[] = []
  if (props.job.media_year) parts.push(String(props.job.media_year))
  if (props.job.media_certification) parts.push(props.job.media_certification)
  if (props.job.media_type === 'series' && props.job.season_number) {
    const ep = props.job.episode_number
      ? `E${String(props.job.episode_number).padStart(2, '0')}`
      : ''
    parts.push(`S${String(props.job.season_number).padStart(2, '0')}${ep}`)
  }
  return parts.join(' \u00B7 ')
})

const statsLine = computed(() => {
  const parts: string[] = []
  const speed = formatSpeed(props.job.download_speed)
  if (speed) parts.push(speed)
  const eta = formatEta(props.job.eta_seconds)
  if (eta) parts.push(eta)
  const size = formatBytes(props.job.total_size)
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
        :alt="job.media_title || job.candidate_title"
        class="w-full h-full object-cover"
      />
      <div v-else class="w-full h-full flex items-center justify-center">
        <component
          :is="job.media_type === 'series' ? Tv : Film"
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
            {{ job.media_title || job.candidate_title }}
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
      <div v-else-if="isImporting && job.total_import_tasks > 0" class="space-y-1">
        <div class="flex items-center gap-2">
          <Progress :model-value="importPercent" class="flex-1" />
          <span class="text-xs text-muted-foreground tabular-nums">
            {{ job.completed_imports }}/{{ job.total_import_tasks }}
          </span>
        </div>
      </div>

      <!-- Error info -->
      <p
        v-if="isFailed && job.last_error"
        class="text-xs text-destructive line-clamp-1"
      >
        {{ job.last_error }}
      </p>

      <!-- Release name (muted, always visible) -->
      <p class="text-xs text-muted-foreground/60 truncate">
        {{ job.candidate_title }}
      </p>

      <!-- Inline action buttons for failed states -->
      <div v-if="isFailed" class="flex items-center gap-2" @click.stop>
        <Button
          v-if="job.import_status === 'download_failed'"
          variant="outline"
          size="sm"
          class="h-7 text-xs"
          @click="emit('retry', job)"
        >
          <RefreshCw class="mr-1.5 size-3" />
          Retry
        </Button>
        <Button
          v-if="['partial_failure', 'import_failed'].includes(job.import_status)"
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
