<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import {
  routingCreateRuleMutation,
  routingUpdateRuleMutation,
  routingDeleteRuleMutation,
  routingListRulesQueryKey,
} from '@/client/@tanstack/vue-query.gen'
import type {
  RoutingRule,
  FieldDefinition,
  Downloader,
  Library,
  NameTemplate,
} from '@/client/types.gen'
import { buildRoutingSummary } from '@/composables/useRoutingSummary'
import { useModal } from '@/composables/useModal'
import { problemMessage } from '@/lib/api'
import ConditionBuilder from '@/components/conditions/ConditionBuilder.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ArrowDown, ArrowUp, ChevronDown, ChevronUp, Trash2 } from 'lucide-vue-next'

interface Props {
  rule: RoutingRule | null
  fields: FieldDefinition[]
  libraries: Library[]
  nameTemplates: NameTemplate[]
  downloaders: Downloader[]
  isNew?: boolean
  canMoveUp?: boolean
  canMoveDown?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  isNew: false,
  canMoveUp: false,
  canMoveDown: false,
})

const emit = defineEmits<{
  saved: []
  deleted: []
  cancelled: []
  moveUp: []
  moveDown: []
}>()

const modal = useModal()
const queryClient = useQueryClient()

function invalidateRules() {
  queryClient.invalidateQueries({ queryKey: routingListRulesQueryKey() })
}

const createMutation = useMutation({
  ...routingCreateRuleMutation(),
  onSuccess: () => {
    invalidateRules()
    isExpanded.value = false
    emit('saved')
  },
  onError: (err) => {
    saveError.value = problemMessage(err, 'Failed to create routing rule')
  },
})
const updateMutation = useMutation({
  ...routingUpdateRuleMutation(),
  onSuccess: () => {
    invalidateRules()
    isExpanded.value = false
    emit('saved')
  },
  onError: (err) => {
    saveError.value = problemMessage(err, 'Failed to update routing rule')
  },
})
const deleteMutation = useMutation({
  ...routingDeleteRuleMutation(),
  onSuccess: () => {
    invalidateRules()
    emit('deleted')
  },
  onError: (err) => {
    saveError.value = problemMessage(err, 'Failed to delete routing rule')
  },
})

// State
const isExpanded = ref(props.isNew)
const nameError = ref(false)
const saveError = ref<string | null>(null)

const form = ref({
  name: '',
  enabled: true,
  continue: false,
  downloaderId: '',
  libraryId: '',
  nameTemplateId: '',
})

// The canonical condition tree, edited via ConditionBuilder (null = none).
const conditionTree = ref<unknown>(null)

function initForm() {
  saveError.value = null
  if (props.rule) {
    form.value = {
      name: props.rule.name,
      enabled: props.rule.enabled,
      continue: props.rule.continue,
      downloaderId: props.rule.downloaderId ?? '',
      libraryId: props.rule.libraryId ?? '',
      nameTemplateId: props.rule.nameTemplateId ?? '',
    }
    conditionTree.value = props.rule.conditions ?? null
  } else {
    form.value = {
      name: '',
      enabled: true,
      continue: false,
      downloaderId: '',
      libraryId: '',
      nameTemplateId: '',
    }
    conditionTree.value = null
  }
  nameError.value = false
}

watch(() => props.rule, initForm, { immediate: true, deep: true })

// Summary
const summary = computed(() =>
  props.rule
    ? buildRoutingSummary(
        props.rule,
        props.fields,
        props.downloaders,
        props.libraries,
        props.nameTemplates,
      )
    : '',
)

const toggleExpand = () => {
  if (isExpanded.value) {
    initForm()
  }
  isExpanded.value = !isExpanded.value
}

function buildBody() {
  return {
    name: form.value.name,
    enabled: form.value.enabled,
    continue: form.value.continue,
    conditions: conditionTree.value,
    downloaderId: form.value.downloaderId || undefined,
    libraryId: form.value.libraryId || undefined,
    nameTemplateId: form.value.nameTemplateId || undefined,
  }
}

const handleToggleEnabled = (newEnabled: boolean) => {
  if (!props.rule?.id) return
  updateMutation.mutate({
    path: { id: props.rule.id },
    body: { ...buildBody(), enabled: newEnabled },
  })
}

const isSaving = computed(() => createMutation.isPending.value || updateMutation.isPending.value)

const handleSave = () => {
  saveError.value = null
  if (!form.value.name.trim()) {
    nameError.value = true
    return
  }
  if (conditionTree.value == null) {
    saveError.value = 'Add at least one condition'
    return
  }

  const body = buildBody()
  if (props.rule?.id) {
    updateMutation.mutate({ path: { id: props.rule.id }, body })
  } else {
    createMutation.mutate({ body })
  }
}

const handleCancel = () => {
  initForm()
  isExpanded.value = false
  if (props.isNew) {
    emit('cancelled')
  }
}

const handleDelete = async () => {
  if (!props.rule?.id) return
  const confirmed = await modal.confirm({
    title: 'Delete Routing Rule',
    message: `Are you sure you want to delete "${props.rule.name}"?`,
    severity: 'danger',
  })
  if (!confirmed) return
  deleteMutation.mutate({ path: { id: props.rule.id } })
}
</script>

<template>
  <div class="rounded-lg border bg-card text-card-foreground shadow-sm">
    <!-- Collapsed header -->
    <div class="flex items-center gap-3 px-4 py-3 cursor-pointer select-none" @click="toggleExpand">
      <Switch
        v-model="form.enabled"
        @click.stop
        @update:model-value="handleToggleEnabled"
        class="shrink-0"
      />
      <span class="font-medium truncate" :class="{ 'text-muted-foreground': !form.enabled }">
        {{ form.name || (isNew ? 'New Rule' : 'Unnamed Rule') }}
      </span>
      <span v-if="!isExpanded" class="text-sm text-muted-foreground truncate ml-2 hidden sm:inline">
        {{ summary }}
      </span>
      <div class="ml-auto flex items-center gap-1 shrink-0">
        <template v-if="!isNew">
          <Button
            size="icon"
            variant="ghost"
            class="size-7"
            :disabled="!canMoveUp"
            @click.stop="emit('moveUp')"
          >
            <ArrowUp class="size-4" />
          </Button>
          <Button
            size="icon"
            variant="ghost"
            class="size-7"
            :disabled="!canMoveDown"
            @click.stop="emit('moveDown')"
          >
            <ArrowDown class="size-4" />
          </Button>
        </template>
        <ChevronUp v-if="isExpanded" class="size-4 text-muted-foreground" />
        <ChevronDown v-else class="size-4 text-muted-foreground" />
      </div>
    </div>

    <!-- Expanded content -->
    <div v-if="isExpanded" class="px-4 pb-4">
      <Separator class="mb-4" />

      <!-- Rule metadata -->
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-6">
        <div class="flex flex-col gap-2">
          <Label>Name</Label>
          <Input
            v-model="form.name"
            placeholder="Rule name"
            :class="{ 'border-destructive': nameError }"
            @input="nameError = false"
          />
          <p v-if="nameError" class="text-sm text-destructive">Name is required.</p>
        </div>
        <div class="flex items-end gap-2 pb-1">
          <Switch v-model="form.continue" id="continue-switch" />
          <Label for="continue-switch" class="font-normal text-muted-foreground">
            Continue evaluating after this rule matches
          </Label>
        </div>
      </div>

      <!-- Conditions section -->
      <div class="mb-6">
        <span class="text-sm font-medium block mb-3">Conditions (all must match)</span>
        <ConditionBuilder v-model="conditionTree" :fields="props.fields" />
      </div>

      <!-- Actions section -->
      <div class="mb-6">
        <span class="text-sm font-medium block mb-3">Actions on match</span>
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <div class="flex flex-col gap-2">
            <Label>Downloader</Label>
            <Select v-model="form.downloaderId">
              <SelectTrigger class="w-full">
                <SelectValue placeholder="Default" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="d in downloaders" :key="d.id" :value="d.id">
                  {{ d.name }} ({{ d.type }}/{{ d.protocol }})
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="flex flex-col gap-2">
            <Label>Library</Label>
            <Select v-model="form.libraryId">
              <SelectTrigger class="w-full">
                <SelectValue placeholder="Default" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="l in libraries" :key="l.id" :value="l.id">
                  {{ l.name }} ({{ l.type }})
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="flex flex-col gap-2">
            <Label>Name Template</Label>
            <Select v-model="form.nameTemplateId">
              <SelectTrigger class="w-full">
                <SelectValue placeholder="Default" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="nt in nameTemplates" :key="nt.id" :value="nt.id">
                  {{ nt.name }} ({{ nt.type }})
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <p class="text-xs text-muted-foreground mt-2">
          Unset actions fall back to the configured defaults.
        </p>
      </div>

      <p v-if="saveError" class="text-sm text-destructive mb-3">{{ saveError }}</p>

      <!-- Footer buttons -->
      <Separator class="mb-4" />
      <div class="flex items-center justify-between">
        <Button v-if="!isNew" variant="destructive" size="sm" @click="handleDelete">
          <Trash2 class="size-3 mr-1" />
          Delete
        </Button>
        <div v-else />
        <div class="flex gap-2">
          <Button variant="outline" size="sm" @click="handleCancel">Cancel</Button>
          <Button size="sm" :disabled="isSaving" @click="handleSave">Save</Button>
        </div>
      </div>
    </div>
  </div>
</template>
