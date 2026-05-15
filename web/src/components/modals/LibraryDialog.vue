<script setup lang="ts">
import { ref, inject, watch, computed } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import {
  librariesCreateMutation,
  librariesUpdateMutation,
  librariesListQueryKey,
} from '@/client/@tanstack/vue-query.gen'
import { type Library } from '@/client/types.gen'
import BaseDialog from './BaseDialog.vue'
import DirectoryBrowserDialog from './DirectoryBrowserDialog.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { FolderOpen } from 'lucide-vue-next'
import { useModal } from '@/composables/useModal'
import { problemMessage } from '@/lib/api'

interface Props {
  library?: Library | null
}

const props = defineProps<Props>()

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const modal = useModal()
const queryClient = useQueryClient()

const libraryForm = ref({
  name: '',
  type: 'movie' as 'movie' | 'series',
  rootPath: '',
  enabled: true,
  default: false,
})

const libraryError = ref<string | null>(null)

const onSaveSuccess = () => {
  queryClient.invalidateQueries({ queryKey: librariesListQueryKey() })
  libraryError.value = null
  dialogRef.value.close({ saved: true })
}
const onSaveError = (err: unknown) => {
  libraryError.value = problemMessage(err, 'Failed to save library')
}

const createLibraryMutation = useMutation({
  ...librariesCreateMutation(),
  onSuccess: onSaveSuccess,
  onError: onSaveError,
})
const updateLibraryMutation = useMutation({
  ...librariesUpdateMutation(),
  onSuccess: onSaveSuccess,
  onError: onSaveError,
})

const typeOptions = [
  { label: 'Movies', value: 'movie' },
  { label: 'Series', value: 'series' },
]

// Initialize form when library changes
watch(
  () => props.library,
  (library) => {
    if (library) {
      libraryForm.value = {
        name: library.name || '',
        type: (library.type as 'movie' | 'series') || 'movie',
        rootPath: library.rootPath || '',
        enabled: library.enabled ?? true,
        default: library.default || false,
      }
    } else {
      libraryForm.value = { name: '', type: 'movie', rootPath: '', enabled: true, default: false }
    }
    libraryError.value = null
  },
  { immediate: true },
)

const handleBrowse = () => {
  modal.open(DirectoryBrowserDialog, {
    props: {
      initialPath: libraryForm.value.rootPath || '/',
    },
    onClose: (result) => {
      const selectedPath = (result?.data as { selectedPath?: string })?.selectedPath
      if (selectedPath) {
        libraryForm.value.rootPath = selectedPath
      }
    },
  })
}

const handleSave = () => {
  if (!libraryForm.value.name || !libraryForm.value.rootPath) {
    libraryError.value = 'Name and root path are required'
    return
  }

  const body = {
    name: libraryForm.value.name,
    type: libraryForm.value.type,
    rootPath: libraryForm.value.rootPath,
    enabled: libraryForm.value.enabled,
    default: libraryForm.value.default,
  }

  if (props.library?.id) {
    updateLibraryMutation.mutate({ path: { id: props.library.id }, body })
  } else {
    createLibraryMutation.mutate({ body })
  }
}

const handleCancel = () => {
  dialogRef.value.close()
}

const isLoading = computed(
  () => createLibraryMutation.isPending.value || updateLibraryMutation.isPending.value,
)
</script>

<template>
  <BaseDialog :title="library ? 'Edit Library' : 'Add Library'">
    <div class="flex flex-col gap-4">
      <div
        v-if="libraryError"
        class="p-4 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive text-sm"
      >
        {{ libraryError }}
      </div>
      <div class="flex flex-col gap-2">
        <Label for="library-name">Name</Label>
        <Input id="library-name" v-model="libraryForm.name" />
      </div>
      <div class="flex flex-col gap-2">
        <Label for="library-type">Type</Label>
        <Select v-model="libraryForm.type">
          <SelectTrigger id="library-type" class="w-full">
            <SelectValue placeholder="Select type" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="option in typeOptions" :key="option.value" :value="option.value">
              {{ option.label }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div class="flex flex-col gap-2">
        <Label for="library-root-path">Root Path</Label>
        <div class="flex gap-2">
          <Input
            id="library-root-path"
            v-model="libraryForm.rootPath"
            placeholder="/mnt/media/Movies"
          />
          <Button variant="outline" size="icon" class="shrink-0" @click="handleBrowse">
            <FolderOpen class="h-4 w-4" />
          </Button>
        </div>
      </div>
      <div class="flex items-center justify-between">
        <Label for="library-enabled">Enabled</Label>
        <Switch id="library-enabled" v-model="libraryForm.enabled" />
      </div>
      <div class="flex items-center justify-between">
        <Label for="library-default">Default</Label>
        <Switch id="library-default" v-model="libraryForm.default" />
      </div>
    </div>
    <template #footer>
      <Button variant="outline" @click="handleCancel">Cancel</Button>
      <Button :disabled="isLoading" @click="handleSave">Save</Button>
    </template>
  </BaseDialog>
</template>
