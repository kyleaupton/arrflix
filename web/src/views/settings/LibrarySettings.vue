<script setup lang="ts">
import { reactive, onMounted, onUnmounted, ref } from 'vue'
import { useQuery, useMutation } from '@tanstack/vue-query'
import { Loader2, Plus, FolderOpen } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  librariesListOptions,
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
import { useEventsStore } from '@/stores/events'
import { Button } from '@/components/ui/button'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import LibraryDialog from '@/components/modals/LibraryDialog.vue'

// Data queries
const { data: libraries, isLoading, refetch } = useQuery(librariesListOptions())
const modal = useModal()
const events = useEventsStore()

// Mutations
const deleteLibraryMutation = useMutation(librariesDeleteMutation())
const scanLibraryMutation = useMutation(librariesScanMutation())

// State
const libraryError = ref<string | null>(null)

interface ScanProgress {
  scanId: string
  libraryId: string
  libraryName: string
  filesSeen: number
  mediaItemsCreated: number
}

const activeScans = reactive(new Map<string, ScanProgress>())

// SSE scan event listeners
const unsubscribers: (() => void)[] = []

onMounted(() => {
  events.connect(['scan_started', 'scan_progress', 'scan_completed', 'scan_failed'])

  unsubscribers.push(
    events.on('scan_started', (data: any) => {
      const lib = libraries.value?.find((l) => l.id === data.libraryId)
      activeScans.set(data.libraryId, {
        scanId: data.scanId,
        libraryId: data.libraryId,
        libraryName: lib?.name ?? 'Unknown',
        filesSeen: 0,
        mediaItemsCreated: 0,
      })
    }),
    events.on('scan_progress', (data: any) => {
      const scan = activeScans.get(data.libraryId)
      if (scan) {
        scan.filesSeen = data.filesSeen
        scan.mediaItemsCreated = data.mediaItemsCreated
      }
    }),
    events.on('scan_completed', (data: any) => {
      activeScans.delete(data.libraryId)
      const lib = libraries.value?.find((l) => l.id === data.libraryId)
      toast.success(`Scan complete: ${lib?.name ?? 'Library'}`, {
        description: `${data.filesSeen} files seen, ${data.mediaItemsCreated} new items`,
      })
      refetch()
    }),
    events.on('scan_failed', (data: any) => {
      activeScans.delete(data.libraryId)
      const lib = libraries.value?.find((l) => l.id === data.libraryId)
      toast.error(`Scan failed: ${lib?.name ?? 'Library'}`, {
        description: data.error,
      })
    }),
  )
})

onUnmounted(() => {
  unsubscribers.forEach((fn) => fn())
})

// Handlers
const handleAddLibrary = () => {
  modal.open(LibraryDialog, {
    props: {
      library: null,
    },
    onClose: () => {
      refetch()
    },
  })
}

const handleEditLibrary = (library: Library) => {
  modal.open(LibraryDialog, {
    props: {
      library,
    },
    onClose: () => {
      refetch()
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
  try {
    await deleteLibraryMutation.mutateAsync({ path: { id: library.id } })
    refetch()
  } catch (err) {
    libraryError.value = err instanceof Error ? err.message : 'Failed to delete library'
  }
}

const handleScanLibrary = async (library: Library) => {
  if (!library.id) return
  try {
    await scanLibraryMutation.mutateAsync({ path: { id: library.id } })
    libraryError.value = null
  } catch (err) {
    libraryError.value = err instanceof Error ? err.message : 'Failed to start scan'
  }
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
