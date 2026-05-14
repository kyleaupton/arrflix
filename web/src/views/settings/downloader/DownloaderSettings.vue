<script setup lang="ts">
import { ref } from 'vue'
import { useQuery, useMutation } from '@tanstack/vue-query'
import { Plus, HardDrive } from 'lucide-vue-next'
import {
  downloadersListOptions,
  downloadersDeleteMutation,
  downloadersTestMutation,
} from '@/client/@tanstack/vue-query.gen'
import { type Downloader, type TestResult } from '@/client/types.gen'
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
const { data: downloaders, isLoading, refetch } = useQuery(downloadersListOptions())
const modal = useModal()

// Error state for table-level errors
const downloaderError = ref<string | null>(null)
const testingId = ref<string | null>(null)

// Mutations
const deleteDownloaderMutation = useMutation({
  ...downloadersDeleteMutation(),
  onSuccess: () => refetch(),
  onError: (err) => {
    downloaderError.value = problemMessage(err, 'Failed to delete downloader')
  },
})
const testDownloaderMutation = useMutation(downloadersTestMutation())

// Handlers
const handleAddDownloader = () => {
  modal.open(DownloaderUpsertDialog, {
    props: {
      class: 'max-w-[90vw] sm:max-w-lg lg:max-w-2xl',
      downloader: null,
    },
    onClose: (result) => {
      const data = result?.data as { saved?: boolean } | undefined
      if (data?.saved) {
        refetch()
      }
    },
  })
}

const handleEditDownloader = (downloader: Downloader) => {
  modal.open(DownloaderUpsertDialog, {
    props: {
      class: 'max-w-[90vw] sm:max-w-lg lg:max-w-2xl',
      downloader,
    },
    onClose: (result) => {
      const data = result?.data as { saved?: boolean } | undefined
      if (data?.saved) {
        refetch()
      }
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

const handleTestDownloader = async (downloader: Downloader) => {
  if (!downloader.id) return
  testingId.value = downloader.id
  try {
    const result = (await testDownloaderMutation.mutateAsync({
      path: { id: downloader.id },
    })) as TestResult
    if (result.success) {
      downloaderError.value = null
      await modal.alert({
        title: 'Connection Test Successful',
        message:
          result.message || `Connection test passed. Version: ${result.version || 'unknown'}`,
        severity: 'success',
      })
    } else {
      downloaderError.value = result.error || 'Connection test failed'
    }
  } catch (err) {
    downloaderError.value = problemMessage(err, 'Connection test failed')
  } finally {
    testingId.value = null
  }
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
