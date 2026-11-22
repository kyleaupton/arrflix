<script setup lang="ts">
import { computed, inject, onBeforeUnmount, ref, watch } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import {
  pathMappingChecksQueryKey,
  pathMappingPreviewMutation,
  volumesCreateMutation,
  volumesListQueryKey,
  volumesUpdateMutation,
} from '@/client/@tanstack/vue-query.gen'
import type { DownloaderPathCheck, Volume } from '@/client/types.gen'
import BaseDialog from '@/components/modals/BaseDialog.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { problemMessage } from '@/lib/api'

const props = defineProps<{
  volume?: Volume | null
  downloaders: Array<{ id: string; name: string }>
}>()

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const queryClient = useQueryClient()

const name = ref(props.volume?.name ?? '')
const arrflixBase = ref(props.volume?.arrflixBase ?? '')
// One alias input per downloader, keyed by downloader ID; blank means none.
const aliases = ref<Record<string, string>>(
  Object.fromEntries(
    props.downloaders.map((d) => [
      d.id,
      props.volume?.aliases?.find((a) => a.downloaderId === d.id)?.base ?? '',
    ]),
  ),
)
const error = ref<string | null>(null)

function aliasBody() {
  return Object.entries(aliases.value)
    .filter(([, base]) => base.trim() !== '')
    .map(([downloaderId, base]) => ({ downloaderId, base: base.trim() }))
}

// Live preview: what each downloader's default save path resolves to with this
// draft saved. Debounced so typing doesn't ask the downloaders on every key.
const preview = ref<DownloaderPathCheck[] | null>(null)
const previewMutation = useMutation({
  ...pathMappingPreviewMutation(),
  onSuccess: (data) => {
    preview.value = data
  },
})
let previewTimer: ReturnType<typeof setTimeout> | undefined
watch(
  [arrflixBase, aliases],
  () => {
    clearTimeout(previewTimer)
    previewTimer = setTimeout(() => {
      previewMutation.mutate({
        body: {
          ...(props.volume?.id ? { volumeId: props.volume.id } : {}),
          arrflixBase: arrflixBase.value.trim(),
          aliases: aliasBody(),
        },
      })
    }, 300)
  },
  { deep: true, immediate: true },
)
onBeforeUnmount(() => clearTimeout(previewTimer))

function onSaved() {
  queryClient.invalidateQueries({ queryKey: volumesListQueryKey() })
  queryClient.invalidateQueries({ queryKey: pathMappingChecksQueryKey() })
  dialogRef.value.close({ saved: true })
}
function onSaveError(err: unknown) {
  error.value = problemMessage(err, 'Failed to save location')
}
const createVolume = useMutation({
  ...volumesCreateMutation(),
  onSuccess: onSaved,
  onError: onSaveError,
})
const updateVolume = useMutation({
  ...volumesUpdateMutation(),
  onSuccess: onSaved,
  onError: onSaveError,
})
const saving = computed(() => createVolume.isPending.value || updateVolume.isPending.value)

function save() {
  if (!name.value.trim() || !arrflixBase.value.trim()) {
    error.value = 'Name and path in Arrflix are required'
    return
  }
  const body = {
    name: name.value.trim(),
    arrflixBase: arrflixBase.value.trim(),
    aliases: aliasBody(),
  }
  if (props.volume?.id) {
    updateVolume.mutate({ path: { id: props.volume.id }, body })
  } else {
    createVolume.mutate({ body })
  }
}
</script>

<template>
  <BaseDialog :title="volume ? 'Edit location' : 'Add location'">
    <div class="flex flex-col gap-4">
      <div
        v-if="error"
        class="p-4 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive text-sm"
      >
        {{ error }}
      </div>
      <div class="flex flex-col gap-2">
        <Label for="location-name">Name</Label>
        <Input id="location-name" v-model="name" placeholder="media" />
      </div>
      <div class="flex flex-col gap-2">
        <Label for="location-arrflix-base">Path in Arrflix</Label>
        <Input id="location-arrflix-base" v-model="arrflixBase" placeholder="/data" />
      </div>
      <div v-for="d in downloaders" :key="d.id" class="flex flex-col gap-2">
        <Label :for="`location-alias-${d.id}`">{{ d.name }} sees it at</Label>
        <Input
          :id="`location-alias-${d.id}`"
          v-model="aliases[d.id]"
          placeholder="Leave blank if it's the same path"
        />
      </div>
      <div v-if="preview?.length" class="flex flex-col gap-1 rounded-md border p-3 text-sm">
        <p class="font-medium">Preview</p>
        <p v-for="c in preview" :key="c.downloaderId" data-testid="location-preview-row">
          <template v-if="c.status === 'unavailable'">
            {{ c.downloaderName }}: couldn't check ({{ c.message }})
          </template>
          <template v-else>
            {{ c.downloaderName }}: <code class="break-all">{{ c.savePath }}</code> →
            <code class="break-all">{{ c.resolvedPath }}</code>
            <span v-if="c.status === 'not_visible'" class="text-amber-600 dark:text-amber-500">
              ⚠ not found in Arrflix</span
            >
            <span v-else class="text-green-600 dark:text-green-500"> ✓ exists</span>
          </template>
        </p>
      </div>
    </div>
    <template #footer>
      <Button variant="outline" @click="dialogRef.value.close()">Cancel</Button>
      <Button :disabled="saving" @click="save">Save</Button>
    </template>
  </BaseDialog>
</template>
