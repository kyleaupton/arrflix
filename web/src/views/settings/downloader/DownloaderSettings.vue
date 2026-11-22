<script setup lang="ts">
import { ref } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { Plus, HardDrive } from 'lucide-vue-next'
import {
  downloadersListOptions,
  downloadersListQueryKey,
  downloadersDeleteMutation,
  downloadersTestMutation,
} from '@/client/@tanstack/vue-query.gen'
import { type Downloader } from '@/client/types.gen'
import DataTable from '@/components/tables/DataTable.vue'
import {
  downloaderColumns,
  createDownloaderActions,
} from '@/components/tables/configs/downloaderTableConfig'
import { useModal } from '@/composables/useModal'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import DownloaderUpsertDialog from './DownloaderUpsertDialog.vue'
import { problemMessage } from '@/lib/api'

// Data queries
const { data: downloaders, isLoading } = useQuery(downloadersListOptions())
const queryClient = useQueryClient()
const modal = useModal()

function invalidateDownloaders() {
  queryClient.invalidateQueries({ queryKey: downloadersListQueryKey() })
}

// Error state for table-level errors
const downloaderError = ref<string | null>(null)

// The path check from the last successful test, shown with a link to Path
// Mapping. Absent when the test couldn't ask the downloader for its save path.
const pathsResult = ref<{ name: string; ok: boolean } | null>(null)

// Mutations
const deleteDownloaderMutation = useMutation({
  ...downloadersDeleteMutation(),
  onSuccess: invalidateDownloaders,
  onError: (err) => {
    downloaderError.value = problemMessage(err, 'Failed to delete downloader')
  },
})
const testDownloaderMutation = useMutation({
  ...downloadersTestMutation(),
  onSuccess: async (result, variables) => {
    pathsResult.value = null
    if (result.success) {
      downloaderError.value = null
      const name = downloaders.value?.find((d) => d.id === variables.path.id)?.name ?? 'Downloader'
      if (result.pathsResolve != null) {
        pathsResult.value = { name, ok: result.pathsResolve }
      }
      await modal.alert({
        title: 'Connection Test Successful',
        message:
          result.message || `Connection test passed. Version: ${result.version || 'unknown'}`,
        severity: 'success',
      })
    } else {
      downloaderError.value = result.error || 'Connection test failed'
    }
  },
  onError: (err) => {
    downloaderError.value = problemMessage(err, 'Connection test failed')
  },
})

function pathsLine(ok: boolean) {
  return ok ? '✓ Paths resolve' : "⚠ Arrflix can't see this downloader's downloads"
}

// Handlers
const handleAddDownloader = () => {
  modal.open(DownloaderUpsertDialog, {
    props: {
      class: 'max-w-[90vw] sm:max-w-lg lg:max-w-2xl',
      downloader: null,
    },
  })
}

const handleEditDownloader = (downloader: Downloader) => {
  modal.open(DownloaderUpsertDialog, {
    props: {
      class: 'max-w-[90vw] sm:max-w-lg lg:max-w-2xl',
      downloader,
    },
  })
}

const handleDeleteDownloader = async (downloader: Downloader) => {
  if (!downloader.id) return
  const confirmed = await modal.confirm({
    title: 'Delete Downloader',
    message: `Are you sure you want to delete "${downloader.name}"?`,
    severity: 'danger',
  })
  if (!confirmed) return
  deleteDownloaderMutation.mutate({ path: { id: downloader.id } })
}

const handleTestDownloader = (downloader: Downloader) => {
  if (!downloader.id) return
  testDownloaderMutation.mutate({ path: { id: downloader.id } })
}

const downloaderActions = createDownloaderActions(
  handleTestDownloader,
  handleEditDownloader,
  handleDeleteDownloader,
)
</script>

<template>
  <div class="flex flex-col gap-6">
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <div>
            <CardTitle class="text-xl font-semibold mb-2">Downloaders</CardTitle>
            <p class="text-sm text-muted-foreground">
              Configure download clients for managing torrents and usenet downloads.
            </p>
          </div>
          <Button @click="handleAddDownloader">
            <Plus class="mr-2 size-4" />
            Add Downloader
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <div
          v-if="downloaderError"
          class="p-4 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive mb-4"
        >
          {{ downloaderError }}
        </div>
        <div
          v-if="pathsResult"
          class="mb-4 flex flex-wrap items-center gap-x-2 rounded-lg border p-3 text-sm"
          data-testid="downloader-paths-result"
        >
          <span class="font-medium">{{ pathsResult.name }}:</span>
          <span
            :class="
              pathsResult.ok
                ? 'text-green-600 dark:text-green-500'
                : 'text-amber-600 dark:text-amber-500'
            "
          >
            {{ pathsLine(pathsResult.ok) }}
          </span>
          <RouterLink to="/settings/path-mapping" class="underline underline-offset-4">
            Path Mapping
          </RouterLink>
        </div>
        <div v-if="isLoading" class="space-y-3">
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
        </div>
        <DataTable
          v-else
          :data="downloaders || []"
          :columns="downloaderColumns"
          :actions="downloaderActions"
          :loading="isLoading"
          empty-message="No downloaders configured"
          searchable
          search-placeholder="Search downloaders..."
          paginator
          :rows="10"
        >
          <template #empty-icon>
            <HardDrive class="size-5" />
          </template>
        </DataTable>
      </CardContent>
    </Card>
  </div>
</template>
