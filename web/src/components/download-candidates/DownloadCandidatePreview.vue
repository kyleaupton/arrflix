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

    <div v-else-if="trace" class="space-y-4 p-2">
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
                <div class="text-sm text-muted-foreground">Downloader</div>
                <div class="font-semibold">
                  <DownloaderReference
                    v-if="trace.finalPlan.downloaderId"
                    :downloader-id="trace.finalPlan.downloaderId"
                  />
                  <span v-else class="text-muted-foreground">Not set</span>
                </div>
              </div>
            </div>
            <div class="flex items-center gap-3">
              <Folder class="size-4 text-primary shrink-0" />
              <div>
                <div class="text-sm text-muted-foreground">Library</div>
                <div class="font-semibold">
                  <LibraryReference
                    v-if="trace.finalPlan.libraryId"
                    :library-id="trace.finalPlan.libraryId"
                  />
                  <span v-else class="text-muted-foreground">Not set</span>
                </div>
              </div>
            </div>
            <div class="flex items-center gap-3">
              <FilePenLine class="size-4 text-primary shrink-0" />
              <div>
                <div class="text-sm text-muted-foreground">Name Template</div>
                <div class="font-semibold">
                  <NameTemplateReference
                    v-if="trace.finalPlan.nameTemplateId"
                    :name-template-id="trace.finalPlan.nameTemplateId"
                  />
                  <span v-else class="text-muted-foreground">Not set</span>
                </div>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      <Separator />

      <!-- Tabbed content: Policy Evaluation & Context -->
      <Tabs default-value="policies" class="w-full">
        <TabsList class="grid w-full grid-cols-2">
          <TabsTrigger value="policies">
            <ListChecks class="size-4 mr-2" />
            Policy Evaluation
          </TabsTrigger>
          <TabsTrigger value="context">
            <Braces class="size-4 mr-2" />
            Context
          </TabsTrigger>
        </TabsList>

        <!-- Policy Evaluation Tab -->
        <TabsContent value="policies" class="mt-4">
          <div class="space-y-3">
            <Card
              v-for="policy in trace.policies"
              :key="policy.policyId"
              :class="{
                'bg-green-500/10 border-green-500/30': policy.matched,
                'bg-muted/50 border-border opacity-60': !policy.matched,
              }"
            >
              <CardContent class="pt-6">
                <div class="flex items-start justify-between gap-4">
                  <div class="flex-1">
                    <div class="flex items-center gap-2 mb-2 flex-wrap">
                      <CheckCircle2 v-if="policy.matched" class="size-5 text-green-500 shrink-0" />
                      <XCircle v-else class="size-5 text-muted-foreground shrink-0" />
                      <span class="font-semibold">{{ policy.policyName }}</span>
                      <Badge variant="secondary" class="text-xs">
                        Priority: {{ policy.priority }}
                      </Badge>
                      <Badge v-if="policy.matched" variant="default" class="text-xs">
                        Matched
                      </Badge>
                    </div>

                    <div v-if="policy.ruleEvaluated" class="text-sm text-muted-foreground mb-2">
                      <span class="font-medium">Rule:</span>
                      <code
                        class="ml-2 px-2 py-1 bg-muted rounded text-xs font-mono inline-flex items-center gap-1 flex-wrap"
                      >
                        <span>{{ policy.ruleEvaluated.leftOperand }}</span>
                        <span
                          v-if="
                            policy.ruleEvaluated.leftResolvedValue !== undefined &&
                            policy.ruleEvaluated.leftResolvedValue !== null
                          "
                          class="text-primary font-semibold"
                          >[{{
                            formatResolvedValue(
                              policy.ruleEvaluated.leftResolvedValue,
                              policy.ruleEvaluated.leftOperand,
                            )
                          }}]</span
                        >
                        <span class="text-yellow-500">{{ policy.ruleEvaluated.operator }}</span>
                        <span>{{ policy.ruleEvaluated.rightOperand }}</span>
                        <span
                          v-if="
                            policy.ruleEvaluated.rightResolvedValue !== undefined &&
                            policy.ruleEvaluated.rightResolvedValue !== null &&
                            isVariable(policy.ruleEvaluated.rightOperand)
                          "
                          class="text-primary font-semibold"
                          >[{{
                            formatResolvedValue(
                              policy.ruleEvaluated.rightResolvedValue,
                              policy.ruleEvaluated.rightOperand,
                            )
                          }}]</span
                        >
                      </code>
                    </div>

                    <div v-if="policy.matched && (policy.actionsApplied?.length ?? 0) > 0" class="mt-3">
                      <div class="text-sm font-medium mb-2">Actions Applied:</div>
                      <ul class="list-disc list-inside space-y-1 text-sm text-foreground">
                        <li v-for="action in policy.actionsApplied" :key="action.order">
                          <span class="font-medium">{{ formatActionType(action.type) }}:</span>
                          <span class="ml-1">{{ action.value }}</span>
                        </li>
                      </ul>
                    </div>

                    <div v-if="policy.stoppedProcessing" class="mt-2">
                      <Badge variant="outline" class="text-xs">Processing Stopped</Badge>
                    </div>
                  </div>
                </div>
              </CardContent>
            </Card>

            <div v-if="(trace.policies?.length ?? 0) === 0" class="text-center py-8 text-muted-foreground">
              No policies evaluated
            </div>
          </div>
        </TabsContent>

        <!-- Context Tab -->
        <TabsContent value="context" class="mt-4">
          <div v-if="trace.context" class="space-y-4">
            <!-- Candidate Context -->
            <Card>
              <CardHeader class="pb-3">
                <CardTitle class="text-base flex items-center gap-2">
                  <File class="size-4" />
                  candidate.*
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div class="grid grid-cols-2 gap-2 text-sm">
                  <template v-for="(value, key) in trace.context.candidate" :key="key">
                    <div class="font-mono text-muted-foreground">{{ key }}</div>
                    <div class="font-mono truncate" :title="formatContextValue(value)">
                      {{ formatContextValue(value) }}
                    </div>
                  </template>
                </div>
              </CardContent>
            </Card>

            <!-- Quality Context -->
            <Card>
              <CardHeader class="pb-3">
                <CardTitle class="text-base flex items-center gap-2">
                  <Sparkles class="size-4" />
                  quality.*
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div class="grid grid-cols-2 gap-2 text-sm">
                  <template v-for="(value, key) in trace.context.quality" :key="key">
                    <div class="font-mono text-muted-foreground">{{ key }}</div>
                    <div class="font-mono">{{ formatContextValue(value) }}</div>
                  </template>
                </div>
              </CardContent>
            </Card>

            <!-- Media Context -->
            <Card>
              <CardHeader class="pb-3">
                <CardTitle class="text-base flex items-center gap-2">
                  <Tv class="size-4" />
                  media.*
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div class="grid grid-cols-2 gap-2 text-sm">
                  <template v-for="(value, key) in trace.context.media" :key="key">
                    <div class="font-mono text-muted-foreground">{{ key }}</div>
                    <div class="font-mono">{{ formatContextValue(value) }}</div>
                  </template>
                </div>
              </CardContent>
            </Card>

            <!-- MediaInfo Context (if available) -->
            <Card v-if="trace.context.mediainfo && Object.keys(trace.context.mediainfo).length > 0">
              <CardHeader class="pb-3">
                <CardTitle class="text-base flex items-center gap-2">
                  <Info class="size-4" />
                  mediainfo.*
                  <Badge variant="secondary" class="text-xs">Post-Download</Badge>
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div class="grid grid-cols-2 gap-2 text-sm">
                  <template v-for="(value, key) in trace.context.mediainfo" :key="key">
                    <div class="font-mono text-muted-foreground">{{ key }}</div>
                    <div class="font-mono">{{ formatContextValue(value) }}</div>
                  </template>
                </div>
              </CardContent>
            </Card>

            <div v-else class="text-center py-4 text-muted-foreground text-sm">
              <Info class="size-4 inline mr-1" />
              mediainfo.* fields are available after download completes
            </div>
          </div>

          <div v-else class="text-center py-8 text-muted-foreground">No context available</div>
        </TabsContent>
      </Tabs>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, watch, ref } from 'vue'
import { useMutation } from '@tanstack/vue-query'
import {
  Braces,
  CheckCircle2,
  Download,
  File,
  FilePenLine,
  Folder,
  Info,
  ListChecks,
  Loader2,
  Sparkles,
  Tv,
  XCircle,
} from 'lucide-vue-next'
import {
  downloadCandidatesPreviewMovieMutation,
  downloadCandidatesPreviewSeriesMutation,
} from '@/client/@tanstack/vue-query.gen'
import { type DownloadCandidate, type EvaluationTrace } from '@/client/types.gen'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
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

// Local state for trace
const trace = ref<EvaluationTrace | undefined>(undefined)
const error = ref<string | undefined>(undefined)

// Preview movie mutation
const moviePreviewMutation = useMutation({
  ...downloadCandidatesPreviewMovieMutation(),
  onSuccess: (data) => {
    trace.value = data
    error.value = undefined
  },
  onError: (err) => {
    error.value = err?.detail || err?.title || 'Failed to load preview'
    trace.value = undefined
  },
})

// Preview series mutation
const seriesPreviewMutation = useMutation({
  ...downloadCandidatesPreviewSeriesMutation(),
  onSuccess: (data) => {
    trace.value = data
    error.value = undefined
  },
  onError: (err) => {
    error.value = err?.detail || err?.title || 'Failed to load preview'
    trace.value = undefined
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
      trace.value = undefined
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

const formatActionType = (type: string): string => {
  const actionMap: Record<string, string> = {
    set_downloader: 'Set Downloader',
    set_library: 'Set Library',
    set_name_template: 'Set Name Template',
    stop_processing: 'Stop Processing',
  }
  return actionMap[type] || type
}

// Check if an operand is a variable reference (contains a dot)
const isVariable = (operand: string): boolean => {
  return operand.includes('.')
}

// Format resolved value for display
const formatResolvedValue = (value: unknown, operand: string): string => {
  if (value === null || value === undefined) {
    return 'null'
  }

  // Special formatting for size values
  if (operand.includes('size') && typeof value === 'number') {
    return formatSize(value)
  }

  // Arrays
  if (Array.isArray(value)) {
    return value.length > 3 ? `[${value.slice(0, 3).join(', ')}...]` : `[${value.join(', ')}]`
  }

  // Booleans
  if (typeof value === 'boolean') {
    return value ? 'true' : 'false'
  }

  // Strings (truncate if too long)
  if (typeof value === 'string') {
    return value.length > 30 ? value.slice(0, 30) + '...' : value
  }

  return String(value)
}

// Format context values for display in the context tab
const formatContextValue = (value: unknown): string => {
  if (value === null || value === undefined) {
    return '-'
  }

  if (Array.isArray(value)) {
    return value.join(', ') || '-'
  }

  if (typeof value === 'boolean') {
    return value ? 'true' : 'false'
  }

  if (typeof value === 'object') {
    return JSON.stringify(value)
  }

  return String(value)
}
</script>
