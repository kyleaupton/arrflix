<script setup lang="ts">
import { computed, ref } from 'vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { FolderTree, Plus } from 'lucide-vue-next'
import {
  pathMappingChecksOptions,
  pathMappingChecksQueryKey,
  volumesDeleteMutation,
  volumesListOptions,
  volumesListQueryKey,
} from '@/client/@tanstack/vue-query.gen'
import type { DownloaderPathCheck, Volume } from '@/client/types.gen'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useModal } from '@/composables/useModal'
import { problemMessage } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import LocationDialog from './LocationDialog.vue'
import PathTestPanel from './PathTestPanel.vue'

const auth = useAuthStore()
const canEdit = computed(() => auth.can('admin.settings.write'))
const modal = useModal()
const queryClient = useQueryClient()

const checks = useQuery(pathMappingChecksOptions())
const volumes = useQuery(volumesListOptions())

const downloaders = computed(() =>
  (checks.data.value ?? []).map((c) => ({ id: c.downloaderId, name: c.downloaderName })),
)
const downloaderName = (id: string) =>
  downloaders.value.find((d) => d.id === id)?.name ?? 'Unknown downloader'

// Which downloaders have their Test panel open.
const testing = ref(new Set<string>())
function toggleTest(id: string) {
  const next = new Set(testing.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  testing.value = next
}

function checkLine(c: DownloaderPathCheck): { ok: boolean; text: string } {
  switch (c.status) {
    case 'no_mapping_needed':
      return { ok: true, text: '✓ No mapping needed' }
    case 'mapped':
      return { ok: true, text: `✓ Mapped via ${c.volumeName}` }
    case 'not_visible':
      return {
        ok: false,
        text: `⚠ Arrflix can't see ${c.downloaderName}'s downloads at ${c.resolvedPath}. Add a path mapping.`,
      }
    default:
      return { ok: false, text: `⚠ Couldn't check: ${c.message}` }
  }
}

const error = ref<string | null>(null)
const deleteVolume = useMutation({
  ...volumesDeleteMutation(),
  onSuccess: () => {
    error.value = null
    queryClient.invalidateQueries({ queryKey: volumesListQueryKey() })
    queryClient.invalidateQueries({ queryKey: pathMappingChecksQueryKey() })
  },
  onError: (err) => {
    error.value = problemMessage(err, 'Failed to delete location')
  },
})

function openLocation(volume: Volume | null) {
  modal.open(LocationDialog, {
    props: { class: 'max-w-[90vw] sm:max-w-lg', volume, downloaders: downloaders.value },
  })
}

async function removeLocation(volume: Volume) {
  const confirmed = await modal.confirm({
    title: 'Delete location',
    message: `Delete "${volume.name}"? Downloads under it will no longer be mapped.`,
    severity: 'danger',
  })
  if (confirmed) deleteVolume.mutate({ path: { id: volume.id } })
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <Card>
      <CardHeader>
        <CardTitle class="text-xl font-semibold mb-2">Path Mapping</CardTitle>
        <p class="text-sm text-muted-foreground">
          Whether Arrflix can see where each downloader saves. When a downloader runs in its own
          container and mounts storage at a different path, add a shared location below.
        </p>
      </CardHeader>
      <CardContent>
        <div v-if="checks.isLoading.value" class="space-y-3">
          <Skeleton class="h-12 w-full" />
        </div>
        <p v-else-if="checks.isError.value" class="text-destructive text-sm">
          {{ problemMessage(checks.error.value, 'Failed to check downloaders') }}
        </p>
        <p v-else-if="!checks.data.value?.length" class="text-sm text-muted-foreground">
          No downloaders configured.
        </p>
        <ul v-else class="flex flex-col divide-y">
          <li
            v-for="c in checks.data.value"
            :key="c.downloaderId"
            class="flex flex-col gap-2 py-3"
            :data-testid="`path-check-${c.downloaderId}`"
          >
            <div class="flex items-start justify-between gap-4">
              <div class="min-w-0">
                <p class="font-medium">{{ c.downloaderName }}</p>
                <p
                  class="text-sm break-words"
                  :class="
                    checkLine(c).ok
                      ? 'text-green-600 dark:text-green-500'
                      : 'text-amber-600 dark:text-amber-500'
                  "
                >
                  {{ checkLine(c).text }}
                </p>
              </div>
              <Button
                variant="outline"
                size="sm"
                :aria-label="`Test ${c.downloaderName}`"
                @click="toggleTest(c.downloaderId)"
              >
                Test
              </Button>
            </div>
            <PathTestPanel
              v-if="testing.has(c.downloaderId)"
              :downloader-id="c.downloaderId"
              :downloader-name="c.downloaderName"
              :default-path="c.savePath ?? ''"
            />
          </li>
        </ul>
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <div class="flex items-center justify-between gap-4">
          <div>
            <CardTitle class="text-xl font-semibold mb-2">Shared locations</CardTitle>
            <p class="text-sm text-muted-foreground">
              Storage that Arrflix and a downloader reach at different paths.
            </p>
          </div>
          <Button v-if="canEdit" :disabled="checks.isLoading.value" @click="openLocation(null)">
            <Plus class="mr-2 size-4" />
            Add location
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <p v-if="error" class="mb-4 text-sm text-destructive">{{ error }}</p>
        <div v-if="volumes.isLoading.value" class="space-y-3">
          <Skeleton class="h-12 w-full" />
        </div>
        <div
          v-else-if="!volumes.data.value?.length"
          class="flex items-center gap-3 text-sm text-muted-foreground"
        >
          <FolderTree class="size-5 shrink-0" />
          No shared locations. Installs where every downloader sees the same paths as Arrflix don't
          need one.
        </div>
        <ul v-else class="flex flex-col divide-y">
          <li
            v-for="v in volumes.data.value"
            :key="v.id"
            class="flex items-start justify-between gap-4 py-3"
          >
            <div class="min-w-0 text-sm">
              <p class="font-medium text-base">{{ v.name }}</p>
              <p>
                Path in Arrflix: <code class="break-all">{{ v.arrflixBase }}</code>
              </p>
              <p v-for="a in v.aliases ?? []" :key="a.downloaderId" class="text-muted-foreground">
                {{ downloaderName(a.downloaderId) }} sees it at
                <code class="break-all">{{ a.base }}</code>
              </p>
            </div>
            <div v-if="canEdit" class="flex shrink-0 gap-2">
              <Button
                variant="outline"
                size="sm"
                :aria-label="`Edit ${v.name}`"
                @click="openLocation(v)"
                >Edit</Button
              >
              <Button
                variant="outline"
                size="sm"
                :aria-label="`Delete ${v.name}`"
                @click="removeLocation(v)"
                >Delete</Button
              >
            </div>
          </li>
        </ul>
      </CardContent>
    </Card>
  </div>
</template>
