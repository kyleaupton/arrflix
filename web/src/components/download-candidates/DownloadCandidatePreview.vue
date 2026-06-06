<template>
  <div class="w-full max-h-[70vh] overflow-y-auto">
    <div v-if="isLoading" class="flex items-center justify-center p-8">
      <Loader2 class="size-6 animate-spin text-muted-foreground" />
      <span class="ml-2 text-muted-foreground">Loading preview...</span>
    </div>

    <div v-else-if="error" class="p-4 bg-destructive/10 border border-destructive/30 rounded-lg">
      <div class="text-destructive font-semibold mb-1">Error loading preview</div>
      <div class="text-destructive/80 text-sm">{{ error }}</div>
    </div>

    <div v-else-if="evaluation" class="space-y-4 p-2">
      <!-- Candidate Info -->
      <Card>
        <CardHeader>
          <CardTitle class="flex items-center gap-2">
            <File class="size-4" />
            <span>{{ candidate.title }}</span>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div class="grid grid-cols-2 gap-4 text-sm">
            <div>
              <span class="font-semibold">Size:</span>
              <span class="ml-2">{{ formatSize(candidate.size) }}</span>
            </div>
            <div>
              <span class="font-semibold">Seeders:</span>
              <span class="ml-2">{{ candidate.seeders }}</span>
            </div>
            <div>
              <span class="font-semibold">Indexer:</span>
              <span class="ml-2">{{ candidate.indexer }}</span>
            </div>
            <div>
              <span class="font-semibold">Categories:</span>
              <span class="ml-2">{{ candidate.categories?.join(', ') || 'None' }}</span>
            </div>
          </div>
        </CardContent>
      </Card>

      <!-- Final Plan -->
      <Card class="border-primary/30 bg-primary/5">
        <CardHeader>
          <CardTitle class="flex items-center gap-2">
            <CheckCircle2 class="size-4 text-primary" />
            <span>Final Download Plan</span>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div class="space-y-3">
            <div class="flex items-center gap-3">
              <Download class="size-4 text-primary shrink-0" />
              <div>
                <div class="text-sm text-muted-foreground">
                  Downloader
                  <Badge
                    v-if="evaluation.fellBack.downloader"
                    variant="secondary"
                    class="text-xs ml-1"
                  >
                    default
                  </Badge>
                </div>
                <div class="font-semibold">
                  <DownloaderReference
                    v-if="evaluation.final.downloaderId"
                    :downloader-id="evaluation.final.downloaderId"
                  />
                  <span v-else class="text-muted-foreground">Not set</span>
                </div>
              </div>
            </div>
            <div class="flex items-center gap-3">
              <Folder class="size-4 text-primary shrink-0" />
              <div>
                <div class="text-sm text-muted-foreground">
                  Library
                  <Badge
                    v-if="evaluation.fellBack.library"
                    variant="secondary"
                    class="text-xs ml-1"
                  >
                    default
                  </Badge>
                </div>
                <div class="font-semibold">
                  <LibraryReference
                    v-if="evaluation.final.libraryId"
                    :library-id="evaluation.final.libraryId"
                  />
                  <span v-else class="text-muted-foreground">Not set</span>
                </div>
              </div>
            </div>
            <div class="flex items-center gap-3">
              <FilePenLine class="size-4 text-primary shrink-0" />
              <div>
                <div class="text-sm text-muted-foreground">
                  Name Template
                  <Badge
                    v-if="evaluation.fellBack.nameTemplate"
                    variant="secondary"
                    class="text-xs ml-1"
                  >
                    default
                  </Badge>
                </div>
                <div class="font-semibold">
                  <NameTemplateReference
                    v-if="evaluation.final.nameTemplateId"
                    :name-template-id="evaluation.final.nameTemplateId"
                  />
                  <span v-else class="text-muted-foreground">Not set</span>
                </div>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      <Separator />

      <!-- Per-rule dispatch record -->
      <div class="space-y-3">
        <div class="flex items-center gap-2 text-sm font-medium">
          <ListChecks class="size-4" />
          Rule Evaluation
        </div>

        <Card
          v-for="rule in evaluation.rules"
          :key="rule.id"
          :class="{
            'bg-green-500/10 border-green-500/30': rule.fired,
            'bg-muted/50 border-border opacity-60': !rule.fired,
          }"
        >
          <CardContent class="pt-6">
            <div class="flex items-center gap-2 flex-wrap">
              <CheckCircle2 v-if="rule.result === 'true'" class="size-5 text-green-500 shrink-0" />
              <CircleHelp
                v-else-if="rule.result === 'unknown'"
                class="size-5 text-yellow-500 shrink-0"
              />
              <XCircle v-else class="size-5 text-muted-foreground shrink-0" />
              <span class="font-semibold">{{ rule.name }}</span>
              <Badge v-if="rule.fired" variant="default" class="text-xs">Fired</Badge>
              <Badge v-if="rule.skipped" variant="secondary" class="text-xs">Skipped</Badge>
              <Badge v-if="rule.result === 'unknown'" variant="outline" class="text-xs">
                Not yet evaluable
              </Badge>
              <Badge v-if="rule.incomplete" variant="outline" class="text-xs">Incomplete</Badge>
            </div>

            <!-- Resolved leaf conditions from the neutral trace -->
            <div v-if="rule.trace" class="mt-3 space-y-1">
              <code
                v-for="(leaf, idx) in traceLeaves(rule.trace)"
                :key="idx"
                class="block px-2 py-1 bg-muted rounded text-xs font-mono"
              >
                <span>{{ leaf.path }}</span>
                <span v-if="leaf.available" class="text-primary font-semibold">
                  [{{ leaf.value }}]</span
                >
                <span v-else class="text-yellow-500"> [unavailable]</span>
                <span class="text-yellow-500"> {{ leaf.op }} </span>
                <span>{{ leaf.right }}</span>
                <span class="ml-2" :class="resultClass(leaf.result)">→ {{ leaf.result }}</span>
              </code>
            </div>
          </CardContent>
        </Card>

        <div
          v-if="(evaluation.rules?.length ?? 0) === 0"
          class="text-center py-8 text-muted-foreground"
        >
          No routing rules configured — defaults apply
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, watch, ref } from 'vue'
import { useMutation } from '@tanstack/vue-query'
import {
  CheckCircle2,
  CircleHelp,
  Download,
  File,
  FilePenLine,
  Folder,
  ListChecks,
  Loader2,
  XCircle,
} from 'lucide-vue-next'
import {
  downloadCandidatesPreviewMovieMutation,
  downloadCandidatesPreviewSeriesMutation,
} from '@/client/@tanstack/vue-query.gen'
import { type DownloadCandidate, type Evaluation, type Trace } from '@/client/types.gen'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import LibraryReference from '@/components/references/LibraryReference.vue'
import DownloaderReference from '@/components/references/DownloaderReference.vue'
import NameTemplateReference from '@/components/references/NameTemplateReference.vue'

const props = defineProps<{
  movieId?: number
  seriesId?: number
  season?: number
  episode?: number
  candidate: DownloadCandidate
}>()

// Local state for the dispatch record
const evaluation = ref<Evaluation | undefined>(undefined)
const error = ref<string | undefined>(undefined)

// Preview movie mutation
const moviePreviewMutation = useMutation({
  ...downloadCandidatesPreviewMovieMutation(),
  onSuccess: (data) => {
    evaluation.value = data
    error.value = undefined
  },
  onError: (err) => {
    error.value = err?.detail || err?.title || 'Failed to load preview'
    evaluation.value = undefined
  },
})

// Preview series mutation
const seriesPreviewMutation = useMutation({
  ...downloadCandidatesPreviewSeriesMutation(),
  onSuccess: (data) => {
    evaluation.value = data
    error.value = undefined
  },
  onError: (err) => {
    error.value = err?.detail || err?.title || 'Failed to load preview'
    evaluation.value = undefined
  },
})

const isLoading = computed(
  () => moviePreviewMutation.isPending.value || seriesPreviewMutation.isPending.value,
)

// Trigger preview when candidate changes
watch(
  () => props.candidate,
  () => {
    if (props.candidate) {
      evaluation.value = undefined
      error.value = undefined
      if (props.movieId) {
        moviePreviewMutation.mutate({
          path: { id: props.movieId },
          body: {
            indexerId: props.candidate.indexerId,
            guid: props.candidate.guid,
          },
        })
      } else if (props.seriesId) {
        seriesPreviewMutation.mutate({
          path: { id: props.seriesId },
          body: {
            indexerId: props.candidate.indexerId,
            guid: props.candidate.guid,
            season: props.season,
            episode: props.episode,
          },
        })
      }
    }
  },
  { immediate: true },
)

// Helper functions
const formatSize = (bytes: number): string => {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = bytes
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }
  return `${size.toFixed(2)} ${units[unitIndex]}`
}

interface TraceLeaf {
  path: string
  value: string
  available: boolean
  op: string
  right: string
  result: string
}

// Flatten the neutral trace into displayable comparison leaves.
const traceLeaves = (trace: Trace): TraceLeaf[] => {
  const leaves: TraceLeaf[] = []
  const walk = (node: Trace) => {
    if (node.children?.length) {
      node.children.forEach(walk)
      return
    }
    if (!node.left && !node.right) return
    leaves.push({
      path: node.left?.path || formatValue(node.left?.value),
      value: formatValue(node.left?.value),
      available: node.left?.available ?? false,
      op: node.op,
      right: node.right?.path || formatValue(node.right?.value),
      result: node.result,
    })
  }
  walk(trace)
  return leaves
}

const formatValue = (value: unknown): string => {
  if (value === null || value === undefined) return '-'
  if (Array.isArray(value)) {
    return value.length > 3 ? `[${value.slice(0, 3).join(', ')}...]` : `[${value.join(', ')}]`
  }
  if (typeof value === 'string') {
    return value.length > 40 ? value.slice(0, 40) + '...' : value
  }
  return String(value)
}

const resultClass = (result: string): string => {
  switch (result) {
    case 'true':
      return 'text-green-500'
    case 'unknown':
      return 'text-yellow-500'
    default:
      return 'text-muted-foreground'
  }
}
</script>
