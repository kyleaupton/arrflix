<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { indexersListConfiguredOptions } from '@/client/@tanstack/vue-query.gen'
import type { FieldDefinition } from '@/client/types.gen'
import { rowsToTree, treeToRows, type ConditionRow } from '@/lib/conditions'
import routingOptions from '@/config/routingOptions.json'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Plus, X } from 'lucide-vue-next'

// ConditionBuilder is the shared condition-tree editor extracted from the
// routing rule card: a flat AND-list of field/operator/value rows backed by
// lib/conditions, with a raw-JSON fallback for trees the row model can't
// represent (nested or/not). It is a v-model of the canonical condition tree —
// routing rules and quality-profile gates both author against the same Subject
// field registry, so both reuse this component.
//
// modelValue is the canonical tree (null means "no conditions"). The component
// emits the rebuilt tree on every edit; while the raw-JSON fallback holds
// invalid JSON it emits null and surfaces an inline error.
const props = defineProps<{
  modelValue: unknown | null
  fields: FieldDefinition[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: unknown]
}>()

const { data: indexers } = useQuery(indexersListConfiguredOptions())

const rows = ref<ConditionRow[]>([])
// Trees authored outside the AND-list model (nested or/not) can't round-trip
// through rows — fall back to editing the canonical JSON directly.
const rawMode = ref(false)
const rawConditions = ref('')
const jsonError = ref(false)

// Tracks the last value we emitted so the modelValue watcher can ignore the
// echo of our own edits and only re-seed on genuinely external changes.
let lastEmitted = ''

function initFromModel() {
  const tree = props.modelValue
  const parsed = treeToRows(tree)
  if (parsed) {
    rows.value = parsed
    rawMode.value = false
  } else if (tree == null) {
    rows.value = []
    rawMode.value = false
    rawConditions.value = ''
  } else {
    rows.value = []
    rawMode.value = true
    rawConditions.value = JSON.stringify(tree, null, 2)
  }
  jsonError.value = false
}

watch(
  () => props.modelValue,
  () => {
    if (JSON.stringify(props.modelValue ?? null) === lastEmitted) return
    initFromModel()
  },
  { immediate: true, deep: true },
)

function emitTree() {
  let tree: unknown = null
  if (rawMode.value) {
    const text = rawConditions.value.trim()
    if (!text) {
      jsonError.value = false
    } else {
      try {
        tree = JSON.parse(text)
        jsonError.value = false
      } catch {
        jsonError.value = true
        tree = null
      }
    }
  } else {
    tree = rowsToTree(rows.value, props.fields)
  }
  lastEmitted = JSON.stringify(tree ?? null)
  emit('update:modelValue', tree)
}

// Row mutations happen through v-model on the row objects (operator, value), so
// a deep watch is the reliable trigger to rebuild and emit the tree.
watch([rows, rawConditions, rawMode], emitTree, { deep: true })

const fieldOptions = computed(() => props.fields.map((f) => ({ label: f.label, value: f.path })))

const getFieldByPath = (path: string): FieldDefinition | undefined =>
  props.fields.find((f) => f.path === path)

const selectedField = (row: ConditionRow) => getFieldByPath(row.field)

const validOperatorsFor = (row: ConditionRow) => {
  const ops = selectedField(row)?.operators || []
  return routingOptions.operators.filter((op) => ops.includes(op.value))
}

const valueOptionsFor = (row: ConditionRow) => {
  const field = selectedField(row)
  if (!field) return []
  if (field.type === 'enum' && field.enumValues) {
    return field.enumValues.map((ev) => ({ label: ev.label, value: ev.value }))
  }
  if (field.type === 'dynamic' && field.dynamicSource === '/api/v1/indexers/configured') {
    return (
      indexers.value?.map((idx) => ({
        label: idx.name || 'Unknown',
        value: idx.name || '',
      })) || []
    )
  }
  if (field.type === 'boolean') {
    return [
      { label: 'True', value: 'true' },
      { label: 'False', value: 'false' },
    ]
  }
  return []
}

// A select widget fits single-value operators on enum/dynamic/boolean fields;
// `in`/`not in` take a comma-separated list via the text input.
const useSelectWidget = (row: ConditionRow) => {
  if (row.operator === 'in' || row.operator === 'not in') return false
  const field = selectedField(row)
  return !!field && ['enum', 'dynamic', 'boolean'].includes(field.type)
}

const useNumberWidget = (row: ConditionRow) => {
  if (row.operator === 'in' || row.operator === 'not in') return false
  return selectedField(row)?.type === 'number'
}

const handleFieldChange = (row: ConditionRow, value: unknown) => {
  row.field = String(value ?? '')
  row.operator = ''
  row.value = ''
}

const handleAddRow = () => {
  rows.value.push({ field: '', operator: '', value: '' })
}

const handleRemoveRow = (index: number) => {
  rows.value.splice(index, 1)
}
</script>

<template>
  <div>
    <!-- Raw JSON fallback for trees the AND-list can't represent -->
    <div v-if="rawMode" class="flex flex-col gap-2">
      <p class="text-sm text-muted-foreground">
        This uses a condition tree the list editor can't represent — edit the JSON directly.
      </p>
      <Textarea v-model="rawConditions" rows="8" class="font-mono text-xs" />
      <p v-if="jsonError" class="text-sm text-destructive">Conditions must be valid JSON.</p>
    </div>

    <template v-else>
      <div v-if="rows.length === 0" class="text-sm text-muted-foreground mb-3">
        No conditions configured. Click "Add Condition" to add one.
      </div>

      <div v-else class="space-y-2 mb-3">
        <div v-for="(row, index) in rows" :key="index" class="flex items-start gap-2">
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-2 flex-1">
            <!-- Field -->
            <Select
              :model-value="row.field"
              @update:model-value="(val) => handleFieldChange(row, val)"
            >
              <SelectTrigger class="w-full">
                <SelectValue placeholder="Select field" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="option in fieldOptions"
                  :key="option.value"
                  :value="option.value"
                >
                  {{ option.label }}
                </SelectItem>
              </SelectContent>
            </Select>

            <!-- Operator -->
            <Select v-model="row.operator" :disabled="!selectedField(row)">
              <SelectTrigger class="w-full">
                <SelectValue placeholder="Operator" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="op in validOperatorsFor(row)" :key="op.value" :value="op.value">
                  {{ op.label }}
                </SelectItem>
              </SelectContent>
            </Select>

            <!-- Value -->
            <Select v-if="useSelectWidget(row)" v-model="row.value">
              <SelectTrigger class="w-full">
                <SelectValue placeholder="Select value" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="option in valueOptionsFor(row)"
                  :key="option.value"
                  :value="option.value"
                >
                  {{ option.label }}
                </SelectItem>
              </SelectContent>
            </Select>
            <Input
              v-else-if="useNumberWidget(row)"
              type="number"
              :model-value="Number(row.value) || 0"
              @update:model-value="(val) => (row.value = String(val ?? 0))"
              placeholder="Enter number"
            />
            <Input
              v-else
              v-model="row.value"
              :placeholder="
                row.operator === 'in' || row.operator === 'not in'
                  ? 'Comma-separated values'
                  : 'Enter value'
              "
              :disabled="!selectedField(row)"
            />
          </div>

          <Button
            size="icon"
            variant="ghost"
            class="shrink-0 mt-0.5"
            @click="handleRemoveRow(index)"
          >
            <X class="size-4" />
          </Button>
        </div>
      </div>

      <Button size="sm" variant="outline" @click="handleAddRow">
        <Plus class="size-3 mr-1" />
        Add Condition
      </Button>
    </template>
  </div>
</template>
