<script setup lang="ts">
import { computed } from 'vue'
import { Film, Tv, RefreshCw, RotateCw } from 'lucide-vue-next'
import type { DownloadJob } from '@/stores/downloadJobs'
import { Card, CardContent } from '@/components/ui/card'
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
  <Card
    class="cursor-pointer transition-colors hover:bg-accent/50"
    @click="emit('click', job)"
  >
    <CardContent class="p-4 space-y-3">
      <!-- Header row -->
      <div class="flex items-start justify-between gap-3">
        <div class="flex items-start gap-3 min-w-0">
          <component
            :is="job.media_type === 'series' ? Tv : Film"
            class="size-5 shrink-0 mt-0.5 text-muted-foreground"
          />
          <div class="min-w-0">
            <p class="font-medium text-sm truncate">{{ job.candidate_title }}</p>
            <p class="text-xs text-muted-foreground">
              {{ job.protocol }} &middot; {{ job.media_type }}
            </p>
          </div>
        </div>
        <Badge :class="`${status.class} border-transparent shrink-0`">
          {{ status.label }}
        </Badge>
      </div>

      <!-- Download progress (active) -->
      <div v-if="isActive" class="space-y-1.5">
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
      <div v-else-if="isImporting && job.total_import_tasks > 0" class="space-y-1.5">
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
        class="text-xs text-destructive line-clamp-2"
      >
        {{ job.last_error }}
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
    </CardContent>
  </Card>
</template>
