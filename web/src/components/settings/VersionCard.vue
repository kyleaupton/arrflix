<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { getV1Version } from '@/client/sdk.gen'
import type { ServiceVersionInfo } from '@/client/types.gen'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { CheckCircle2, ArrowUpCircle, ExternalLink, RefreshCw, ChevronRight, Code2 } from 'lucide-vue-next'
import { useVersionDebugStore, type VersionDebugState } from '@/stores/versionDebug'

const debug = useVersionDebugStore()
const devMode = import.meta.env.DEV

const isLoading = ref(true)
const isRefreshing = ref(false)
const error = ref<string | null>(null)
const data = ref<ServiceVersionInfo | null>(null)

async function load(refreshing = false) {
  if (debug.isActive) return
  if (refreshing) {
    isRefreshing.value = true
  } else {
    isLoading.value = true
  }
  error.value = null
  try {
    const res = await getV1Version<true>({ throwOnError: true })
    data.value = res.data as ServiceVersionInfo
  } catch {
    error.value = 'Failed to load version information'
  } finally {
    isLoading.value = false
    isRefreshing.value = false
  }
}

watch(
  () => debug.state,
  (state) => {
    if (state === 'off') {
      load()
      return
    }
    isRefreshing.value = false
    if (state === 'loading') {
      isLoading.value = true
      error.value = null
      data.value = null
    } else if (state === 'error') {
      isLoading.value = false
      error.value = 'Failed to load version information'
      data.value = null
    } else {
      isLoading.value = false
      error.value = null
      data.value = debug.mockData
    }
  },
)

onMounted(() => load())

const debugStates: { value: VersionDebugState; label: string }[] = [
  { value: 'off', label: 'Off (live)' },
  { value: 'loading', label: 'Loading' },
  { value: 'error', label: 'Error' },
  { value: 'up_to_date', label: 'Up to date' },
  { value: 'update_available', label: 'Update available' },
  { value: 'dev', label: 'Dev build' },
  { value: 'unknown', label: 'Unknown' },
]

const isDev = computed(() => {
  return data.value?.update.status === 'unknown' && data.value?.update.reason === 'dev_build'
})

const isUpdateAvailable = computed(() => {
  return data.value?.update.status === 'update_available'
})

const isUpToDate = computed(() => {
  return data.value?.update.status === 'up_to_date'
})

const formattedBuildDate = computed(() => {
  if (!data.value?.buildDate) return null
  try {
    return new Date(data.value.buildDate).toLocaleDateString()
  } catch {
    return data.value.buildDate
  }
})

const versionMeta = computed(() => {
  const parts: string[] = []
  if (data.value?.version) parts.push(data.value.version)
  if (data.value?.commit) parts.push(data.value.commit)
  if (formattedBuildDate.value) parts.push(formattedBuildDate.value)
  return parts.join(' \u00b7 ')
})

const statusHeading = computed(() => {
  if (isUpToDate.value) return 'Up to date'
  if (isUpdateAvailable.value) return 'Update available'
  if (isDev.value) return 'Development build'
  return 'Unable to check for updates'
})

const hasComponents = computed(() => {
  return data.value?.components && Object.keys(data.value.components).length > 0
})
</script>

<template>
  <Card>
    <CardHeader>
      <div class="flex items-center justify-between">
        <CardTitle>Version</CardTitle>
        <Select v-if="devMode" v-model="debug.state">
          <SelectTrigger class="h-7 w-[150px] text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="s in debugStates" :key="s.value" :value="s.value">
              {{ s.label }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
    </CardHeader>
    <CardContent>
      <!-- Loading -->
      <div v-if="isLoading" class="space-y-3">
        <Skeleton class="h-6 w-40" />
        <Skeleton class="h-4 w-56" />
      </div>

      <!-- Error -->
      <div v-else-if="error" class="text-sm text-destructive">
        {{ error }}
      </div>

      <!-- Loaded -->
      <div v-else-if="data" class="space-y-4">
        <!-- Status hero -->
        <div class="flex items-start justify-between gap-2">
          <div class="space-y-1">
            <div class="flex items-center gap-2">
              <CheckCircle2
                v-if="isUpToDate"
                class="size-5 text-emerald-600 dark:text-emerald-400 shrink-0"
              />
              <ArrowUpCircle
                v-else-if="isUpdateAvailable"
                class="size-5 text-blue-600 dark:text-blue-400 shrink-0"
              />
              <Code2
                v-else-if="isDev"
                class="size-5 text-muted-foreground shrink-0"
              />
              <span
                class="text-lg font-medium"
                :class="{
                  'text-emerald-600 dark:text-emerald-400': isUpToDate,
                  'text-blue-600 dark:text-blue-400': isUpdateAvailable,
                }"
              >
                {{ statusHeading }}
              </span>
            </div>
            <div v-if="versionMeta" class="text-xs text-muted-foreground font-mono">
              {{ versionMeta }}
            </div>
          </div>
          <Button
            v-if="!isDev"
            variant="ghost"
            size="icon"
            class="size-8 text-muted-foreground shrink-0"
            :disabled="isRefreshing"
            @click="load(true)"
          >
            <RefreshCw class="size-3.5" :class="{ 'animate-spin': isRefreshing }" />
          </Button>
        </div>

        <!-- Update details -->
        <div v-if="isUpdateAvailable && data.update.latest" class="space-y-3">
          <div
            class="rounded-lg border border-blue-200 bg-blue-50 dark:border-blue-900 dark:bg-blue-950/50 p-3 space-y-2"
          >
            <div class="text-sm font-medium text-blue-900 dark:text-blue-100">
              {{ data.update.latest.version }}
            </div>
            <div
              v-if="data.update.latest.notes"
              class="text-xs text-blue-800 dark:text-blue-200/80 max-h-32 overflow-y-auto whitespace-pre-wrap"
            >
              {{ data.update.latest.notes }}
            </div>
            <Button
              v-if="data.update.latest.url"
              as="a"
              :href="data.update.latest.url"
              target="_blank"
              variant="outline"
              size="sm"
              class="h-7 px-2 text-xs"
            >
              View on GitHub
              <ExternalLink class="size-3 ml-1" />
            </Button>
          </div>
        </div>

        <!-- Components -->
        <Collapsible v-if="hasComponents">
          <CollapsibleTrigger class="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors group">
            <ChevronRight class="size-3 transition-transform group-data-[state=open]:rotate-90" />
            Components
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div class="mt-2 space-y-1 pl-4">
              <div
                v-for="(version, name) in data.components"
                :key="name"
                class="flex items-center justify-between text-xs text-muted-foreground"
              >
                <span class="capitalize">{{ name }}</span>
                <span class="font-mono">{{ version }}</span>
              </div>
            </div>
          </CollapsibleContent>
        </Collapsible>
      </div>
    </CardContent>
  </Card>
</template>
