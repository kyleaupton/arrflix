<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { Loader2, Plus, FolderOpen } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  librariesListOptions,
  librariesListQueryKey,
  librariesDeleteMutation,
  librariesScanMutation,
} from '@/client/@tanstack/vue-query.gen'
import { type Library } from '@/client/types.gen'
import DataTable from '@/components/tables/DataTable.vue'
import {
  libraryColumns,
  createLibraryActions,
} from '@/components/tables/configs/libraryTableConfig'
import { useModal } from '@/composables/useModal'
import { useRealtimeListener } from '@/realtime/useRealtimeListener'
import { Button } from '@/components/ui/button'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import LibraryDialog from '@/components/modals/LibraryDialog.vue'
import { problemMessage } from '@/lib/api'

// Data queries
const { data: libraries, isLoading } = useQuery(librariesListOptions())
const queryClient = useQueryClient()
const modal = useModal()

function invalidateLibraries() {
  queryClient.invalidateQueries({ queryKey: librariesListQueryKey() })
}

// State
const libraryError = ref<string | null>(null)

// Mutations
const deleteLibraryMutation = useMutation({
  ...librariesDeleteMutation(),
  onSuccess: invalidateLibraries,
  onError: (err) => {
    libraryError.value = problemMessage(err, 'Failed to delete library')
  },
})
const scanLibraryMutation = useMutation({
  ...librariesScanMutation(),
  onSuccess: () => {
    libraryError.value = null
  },
  onError: (err) => {
    libraryError.value = problemMessage(err, 'Failed to start scan')
  },
})

interface ScanProgress {
  scanId: string
  libraryId: string
  libraryName: string
  filesSeen: number
  mediaItemsCreated: number
}

// SSE event payload shape — useRealtimeListener delivers `data` as unknown, so
// we narrow here. Fields are optional because each scan event carries a subset.
interface ScanEventData {
  scanId?: string
  libraryId: string
  filesSeen?: number
  mediaItemsCreated?: number
  error?: string
}

const activeScans = reactive(new Map<string, ScanProgress>())

// Live scan progress is ephemeral UI (not server cache), so it uses scoped
// realtime listeners — the app-wide stream already carries the events, and each
// listener auto-unsubscribes when this view unmounts.
useRealtimeListener('scan_started', (data) => {
  const d = data as ScanEventData
  const lib = libraries.value?.find((l) => l.id === d.libraryId)
  activeScans.set(d.libraryId, {
    scanId: d.scanId ?? '',
    libraryId: d.libraryId,
    libraryName: lib?.name ?? 'Unknown',
    filesSeen: 0,
    mediaItemsCreated: 0,
  })
})
useRealtimeListener('scan_progress', (data) => {
  const d = data as ScanEventData
  const scan = activeScans.get(d.libraryId)
  if (scan) {
    scan.filesSeen = d.filesSeen ?? scan.filesSeen
    scan.mediaItemsCreated = d.mediaItemsCreated ?? scan.mediaItemsCreated
  }
})
useRealtimeListener('scan_completed', (data) => {
  const d = data as ScanEventData
  activeScans.delete(d.libraryId)
  const lib = libraries.value?.find((l) => l.id === d.libraryId)
  toast.success(`Scan complete: ${lib?.name ?? 'Library'}`, {
    description: `${d.filesSeen ?? 0} files seen, ${d.mediaItemsCreated ?? 0} new items`,
  })
  invalidateLibraries()
})
useRealtimeListener('scan_failed', (data) => {
  const d = data as ScanEventData
  activeScans.delete(d.libraryId)
  const lib = libraries.value?.find((l) => l.id === d.libraryId)
  toast.error(`Scan failed: ${lib?.name ?? 'Library'}`, {
    description: d.error,
  })
})

// Handlers
const handleAddLibrary = () => {
  modal.open(LibraryDialog, {
    props: {
      library: null,
    },
  })
}

const handleEditLibrary = (library: Library) => {
  modal.open(LibraryDialog, {
    props: {
      library,
    },
  })
}

const handleDeleteLibrary = async (library: Library) => {
  if (!library.id) return
  const confirmed = await modal.confirm({
    title: 'Delete Library',
    message: `Are you sure you want to delete "${library.name}"?`,
    severity: 'danger',
  })
  if (!confirmed) return
  deleteLibraryMutation.mutate({ path: { id: library.id } })
}

const handleScanLibrary = (library: Library) => {
  if (!library.id) return
  scanLibraryMutation.mutate({ path: { id: library.id } })
}

const libraryActions = createLibraryActions(
  handleScanLibrary,
  handleEditLibrary,
  handleDeleteLibrary,
)
</script>

<template>
  <div class="flex flex-col gap-6">
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <div>
            <CardTitle class="text-xl font-semibold mb-2">Library Settings</CardTitle>
            <p class="text-sm text-muted-foreground">
              Configure libraries to organize your media content.
            </p>
          </div>
          <Button @click="handleAddLibrary">
            <Plus class="mr-2 size-4" />
            Add Library
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <div
          v-if="libraryError"
          class="p-4 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive mb-4"
        >
          {{ libraryError }}
        </div>
        <Alert v-for="[libId, scan] of activeScans" :key="libId" class="mb-4">
          <Loader2 class="size-4 animate-spin" />
          <AlertTitle>Scanning {{ scan.libraryName }}</AlertTitle>
          <AlertDescription>
            {{ scan.filesSeen }} files seen, {{ scan.mediaItemsCreated }} new items created
          </AlertDescription>
        </Alert>
        <div v-if="isLoading" class="space-y-3">
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
        </div>
        <DataTable
          v-else
          :data="libraries || []"
          :columns="libraryColumns"
          :actions="libraryActions"
          :loading="isLoading"
          empty-message="No libraries configured"
          searchable
          search-placeholder="Search libraries..."
          paginator
          :rows="10"
        >
          <template #empty-icon>
            <FolderOpen class="size-5" />
          </template>
        </DataTable>
      </CardContent>
    </Card>
  </div>
</template>
