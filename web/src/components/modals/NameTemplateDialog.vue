<script setup lang="ts">
import { ref, inject, watch, computed } from 'vue'
import { useMutation } from '@tanstack/vue-query'
import { toast } from 'vue-sonner'
import {
  postV1NameTemplatesMutation,
  putV1NameTemplatesByIdMutation,
} from '@/client/@tanstack/vue-query.gen'
import { type HandlersNameTemplateSwagger } from '@/client/types.gen'
import BaseDialog from './BaseDialog.vue'
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
import { TemplateTokenEditor } from '@/components/ui/template-editor'
import { presets, type TemplatePreset } from './templatePresets'

interface Props {
  template?: HandlersNameTemplateSwagger | null
}

const props = defineProps<Props>()

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }

const createTemplateMutation = useMutation(postV1NameTemplatesMutation())
const updateTemplateMutation = useMutation(putV1NameTemplatesByIdMutation())

const templateForm = ref({
  name: '',
  type: 'movie' as 'movie' | 'series',
  template: '',
  series_show_template: '',
  series_season_template: '',
  movie_dir_template: '',
  default: false,
})

const templateError = ref<string | null>(null)
const selectedPresetId = ref<string | null>(null)

const isCreateMode = computed(() => !props.template?.id)

const typeOptions = [
  { label: 'Movies', value: 'movie' },
  { label: 'Series', value: 'series' },
]

function applyPreset(preset: TemplatePreset) {
  selectedPresetId.value = preset.id
  if (templateForm.value.type === 'movie') {
    templateForm.value.template = preset.movie.template
    templateForm.value.movie_dir_template = preset.movie.movieDirTemplate
    templateForm.value.series_show_template = ''
    templateForm.value.series_season_template = ''
  } else {
    templateForm.value.template = preset.series.template
    templateForm.value.series_show_template = preset.series.seriesShowTemplate
    templateForm.value.series_season_template = preset.series.seriesSeasonTemplate
    templateForm.value.movie_dir_template = ''
  }
}

function clearPreset() {
  selectedPresetId.value = null
  templateForm.value.template = ''
  templateForm.value.movie_dir_template = ''
  templateForm.value.series_show_template = ''
  templateForm.value.series_season_template = ''
}

// Initialize form when template changes
watch(
  () => props.template,
  (template) => {
    if (template) {
      templateForm.value = {
        name: template.name || '',
        type: (template.type as 'movie' | 'series') || 'movie',
        template: template.template || '',
        series_show_template: template.series_show_template || '',
        series_season_template: template.series_season_template || '',
        movie_dir_template: template.movie_dir_template || '',
        default: template.default || false,
      }
      selectedPresetId.value = null
    } else {
      templateForm.value = {
        name: '',
        type: 'movie',
        template: '',
        series_show_template: '',
        series_season_template: '',
        movie_dir_template: '',
        default: false,
      }
      selectedPresetId.value = null
    }
    templateError.value = null
  },
  { immediate: true },
)

// Re-apply preset when type changes
watch(
  () => templateForm.value.type,
  () => {
    if (selectedPresetId.value && isCreateMode.value) {
      const preset = presets.find((p) => p.id === selectedPresetId.value)
      if (preset) {
        applyPreset(preset)
      }
    }
  },
)

const handleSave = async () => {
  if (!templateForm.value.name || !templateForm.value.template) {
    templateError.value = 'Name and template are required'
    return
  }
  if (templateForm.value.type === 'movie' && !templateForm.value.movie_dir_template) {
    templateError.value = 'Movie directory template is required'
    return
  }

  try {
    const body = {
      name: templateForm.value.name,
      type: templateForm.value.type,
      template: templateForm.value.template,
      series_show_template:
        templateForm.value.type === 'series' ? templateForm.value.series_show_template : '',
      series_season_template:
        templateForm.value.type === 'series' ? templateForm.value.series_season_template : '',
      movie_dir_template:
        templateForm.value.type === 'movie' ? templateForm.value.movie_dir_template : '',
      default: templateForm.value.default,
    }

    if (props.template?.id) {
      await updateTemplateMutation.mutateAsync({
        path: { id: props.template.id },
        body,
      })
      toast.success('Template updated successfully')
    } else {
      await createTemplateMutation.mutateAsync({
        body,
      })
      toast.success('Template created successfully')
    }
    templateError.value = null
    dialogRef.value.close({ saved: true })
  } catch (err) {
    const error = err as { message?: string }
    templateError.value = error.message || 'Failed to save template'
  }
}

const handleCancel = () => {
  dialogRef.value.close()
}

const isLoading = computed(
  () => createTemplateMutation.isPending.value || updateTemplateMutation.isPending.value,
)
</script>

<template>
  <BaseDialog :title="template ? 'Edit Name Template' : 'Add Name Template'">
    <div class="flex flex-col gap-4">
      <div
        v-if="templateError"
        class="p-4 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive text-sm"
      >
        {{ templateError }}
      </div>

      <!-- Preset selector (create mode only) -->
      <div v-if="isCreateMode" class="flex flex-col gap-2">
        <Label>Start from a preset</Label>
        <div class="flex gap-2">
          <button
            :class="[
              'flex-1 rounded-lg border px-3 py-2 text-left text-sm transition-colors',
              selectedPresetId === null
                ? 'border-primary bg-primary/5 text-foreground'
                : 'border-border bg-card text-muted-foreground hover:border-muted-foreground/50',
            ]"
            @click="clearPreset"
          >
            <div class="font-medium">Blank</div>
            <div class="text-xs text-muted-foreground">Start from scratch</div>
          </button>
          <button
            v-for="preset in presets"
            :key="preset.id"
            :class="[
              'flex-1 rounded-lg border px-3 py-2 text-left text-sm transition-colors',
              selectedPresetId === preset.id
                ? 'border-primary bg-primary/5 text-foreground'
                : 'border-border bg-card text-muted-foreground hover:border-muted-foreground/50',
            ]"
            @click="applyPreset(preset)"
          >
            <div class="font-medium">{{ preset.label }}</div>
            <div class="text-xs text-muted-foreground">{{ preset.description }}</div>
          </button>
        </div>
      </div>

      <div class="flex flex-col gap-2">
        <Label for="template-name">Name</Label>
        <Input id="template-name" v-model="templateForm.name" placeholder="My Movie Template" />
      </div>

      <div class="flex flex-col gap-2">
        <Label for="template-type">Type</Label>
        <Select v-model="templateForm.type">
          <SelectTrigger id="template-type" class="w-full">
            <SelectValue placeholder="Select type" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="option in typeOptions" :key="option.value" :value="option.value">
              {{ option.label }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <template v-if="templateForm.type === 'series'">
        <div class="flex flex-col gap-2">
          <Label for="template-show">Show Directory Template</Label>
          <TemplateTokenEditor
            v-model="templateForm.series_show_template"
            :media-type="templateForm.type"
            placeholder="Type { to insert a variable"
            class="min-h-[60px]"
          />
        </div>

        <div class="flex flex-col gap-2">
          <Label for="template-season">Season Directory Template</Label>
          <TemplateTokenEditor
            v-model="templateForm.series_season_template"
            :media-type="templateForm.type"
            placeholder="Type { to insert a variable"
            class="min-h-[60px]"
          />
        </div>
      </template>

      <template v-if="templateForm.type === 'movie'">
        <div class="flex flex-col gap-2">
          <Label for="template-movie-dir">Movie Directory Template</Label>
          <TemplateTokenEditor
            v-model="templateForm.movie_dir_template"
            :media-type="templateForm.type"
            placeholder="Type { to insert a variable"
            class="min-h-[60px]"
          />
        </div>
      </template>

      <div class="flex flex-col gap-2">
        <Label for="template-template">
          {{ templateForm.type === 'series' ? 'Episode File Template' : 'File Template' }}
        </Label>
        <TemplateTokenEditor
          v-model="templateForm.template"
          :media-type="templateForm.type"
          placeholder="Type { to insert a variable"
        />
        <p class="text-xs text-muted-foreground">
          Type <code class="px-1 py-0.5 rounded bg-muted font-mono">{</code> to insert a variable.
          Right-click on a variable to wrap it with a function.
        </p>
      </div>

      <div class="flex items-center justify-between">
        <Label for="template-default">Default</Label>
        <Switch id="template-default" v-model="templateForm.default" />
      </div>
    </div>

    <template #footer>
      <Button variant="outline" @click="handleCancel">Cancel</Button>
      <Button :disabled="isLoading" @click="handleSave">Save</Button>
    </template>
  </BaseDialog>
</template>
