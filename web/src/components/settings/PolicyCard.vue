<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useMutation } from '@tanstack/vue-query'
import {
  policiesCreateMutation,
  policiesUpdateMutation,
  policiesDeleteMutation,
  policiesCreateRuleMutation,
  policiesUpdateRuleMutation,
  policiesDeleteRuleMutation,
  policiesCreateActionMutation,
  policiesDeleteActionMutation,
} from '@/client/@tanstack/vue-query.gen'
import type { Policy, FieldDefinition, Downloader, Library, NameTemplate } from '@/client/types.gen'
import { usePolicyFields } from '@/composables/usePolicyFields'
import { buildPolicySummary } from '@/composables/usePolicySummary'
import { useModal } from '@/composables/useModal'
import policyOptions from '@/config/policyOptions.json'
import { indexersListConfiguredOptions } from '@/client/@tanstack/vue-query.gen'
import { useQuery } from '@tanstack/vue-query'
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
import { ChevronDown, ChevronUp, Plus, Trash2, X } from 'lucide-vue-next'

interface Props {
  policy: Policy | null
  fields: FieldDefinition[]
  libraries: Library[]
  nameTemplates: NameTemplate[]
  downloaders: Downloader[]
  isNew?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  isNew: false,
})

const emit = defineEmits<{
  saved: []
  deleted: []
  cancelled: []
}>()

const modal = useModal()
const { getFieldByPath, getValidOperators } = usePolicyFields()
const { data: indexers } = useQuery(indexersListConfiguredOptions())

// Mutations
const createPolicyMutation = useMutation(policiesCreateMutation())
const updatePolicyMutation = useMutation(policiesUpdateMutation())
const deletePolicyMutation = useMutation(policiesDeleteMutation())
const createRuleMutation = useMutation(policiesCreateRuleMutation())
const updateRuleMutation = useMutation(policiesUpdateRuleMutation())
const deleteRuleMutation = useMutation(policiesDeleteRuleMutation())
const createActionMutation = useMutation(policiesCreateActionMutation())
const deleteActionMutation = useMutation(policiesDeleteActionMutation())

// State
const isExpanded = ref(props.isNew)
const isSaving = ref(false)
const showRuleEditor = ref(false)
const nameError = ref(false)

interface RuleForm {
  left_operand: string
  operator: string
  right_operand: string
  exists: boolean
  originalId?: string
}

interface ActionForm {
  type: string
  value: string
  order: number
  originalId?: string
  isNew: boolean
}

const policyForm = ref({
  name: '',
  description: '',
  enabled: true,
  priority: 0,
})

const ruleForm = ref<RuleForm>({
  left_operand: '',
  operator: '',
  right_operand: '',
  exists: false,
})

const actionsForm = ref<ActionForm[]>([])

function initForm() {
  if (props.policy) {
    policyForm.value = {
      name: props.policy.name || '',
      description: props.policy.description || '',
      enabled: props.policy.enabled ?? true,
      priority: props.policy.priority || 0,
    }
    if (props.policy.rule && props.policy.rule.id) {
      ruleForm.value = {
        left_operand: props.policy.rule.leftOperand || '',
        operator: props.policy.rule.operator || '',
        right_operand: props.policy.rule.rightOperand || '',
        exists: true,
        originalId: props.policy.rule.id,
      }
      showRuleEditor.value = true
    } else {
      ruleForm.value = { left_operand: '', operator: '', right_operand: '', exists: false }
      showRuleEditor.value = false
    }
    actionsForm.value = (props.policy.actions || []).map((a, i) => ({
      type: a.type,
      value: a.value,
      order: i,
      originalId: a.id,
      isNew: false,
    }))
  } else {
    policyForm.value = { name: '', description: '', enabled: true, priority: 0 }
    ruleForm.value = { left_operand: '', operator: '', right_operand: '', exists: false }
    showRuleEditor.value = false
    actionsForm.value = []
  }
  nameError.value = false
}

watch(() => props.policy, initForm, { immediate: true, deep: true })

// Summary
const summary = computed(() =>
  buildPolicySummary(
    props.policy?.rule ?? null,
    props.policy?.actions ?? [],
    props.fields,
    props.downloaders,
    props.libraries,
    props.nameTemplates,
  ),
)

// Rule field logic
const fieldOptions = computed(() => props.fields.map((f) => ({ label: f.label, value: f.path })))

const selectedField = computed(() => {
  if (!ruleForm.value.left_operand) return undefined
  return getFieldByPath(ruleForm.value.left_operand)
})

const validOperators = computed(() => {
  const ops = getValidOperators(selectedField.value)
  return policyOptions.operators.filter((op) => ops.includes(op.value))
})

const rightOperandOptions = computed(() => {
  const field = selectedField.value
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
})

// Action value options
const getActionValueOptions = (actionType: string) => {
  switch (actionType) {
    case 'set_downloader':
      return props.downloaders.map((d) => ({
        label: `${d.name} (${d.type}/${d.protocol})`,
        value: d.id,
      }))
    case 'set_library':
      return props.libraries.map((l) => ({
        label: `${l.name} (${l.type})`,
        value: l.id,
      }))
    case 'set_name_template':
      return props.nameTemplates.map((nt) => ({
        label: `${nt.name} (${nt.type})`,
        value: nt.id,
      }))
    default:
      return []
  }
}

// Handlers
const handleLeftOperandChange = (
  value: string | number | bigint | Record<string, unknown> | null,
) => {
  ruleForm.value.left_operand = String(value ?? '')
  ruleForm.value.operator = ''
  ruleForm.value.right_operand = ''
}

const handleAddRule = () => {
  ruleForm.value = { left_operand: '', operator: '', right_operand: '', exists: false }
  showRuleEditor.value = true
}

const handleRemoveRule = () => {
  ruleForm.value = { left_operand: '', operator: '', right_operand: '', exists: false }
  showRuleEditor.value = false
}

const handleAddAction = () => {
  actionsForm.value.push({
    type: '',
    value: '',
    order: actionsForm.value.length,
    isNew: true,
  })
}

const handleRemoveAction = (index: number) => {
  actionsForm.value.splice(index, 1)
  actionsForm.value.forEach((a, i) => (a.order = i))
}

const handleActionTypeChange = (index: number, value: string) => {
  if (actionsForm.value[index]) {
    actionsForm.value[index].type = value
    actionsForm.value[index].value = ''
  }
}

const toggleExpand = () => {
  if (isExpanded.value) {
    // Collapsing — reset form
    initForm()
  }
  isExpanded.value = !isExpanded.value
}

const handleToggleEnabled = async (newEnabled: boolean) => {
  if (!props.policy?.id) return
  try {
    await updatePolicyMutation.mutateAsync({
      path: { id: props.policy.id },
      body: {
        name: policyForm.value.name,
        description: policyForm.value.description || '',
        enabled: newEnabled,
        priority: policyForm.value.priority,
      },
    })
    emit('saved')
  } catch (error) {
    console.error('Failed to toggle policy:', error)
    policyForm.value.enabled = !newEnabled
  }
}

const handleSave = async () => {
  if (!policyForm.value.name.trim()) {
    nameError.value = true
    return
  }

  isSaving.value = true
  try {
    let policyId = props.policy?.id

    // 1. Create or update policy
    if (policyId) {
      await updatePolicyMutation.mutateAsync({
        path: { id: policyId },
        body: {
          name: policyForm.value.name,
          description: policyForm.value.description || '',
          enabled: policyForm.value.enabled,
          priority: policyForm.value.priority,
        },
      })
    } else {
      const result = await createPolicyMutation.mutateAsync({
        body: {
          name: policyForm.value.name,
          description: policyForm.value.description || '',
          enabled: policyForm.value.enabled,
          priority: policyForm.value.priority,
        },
      })
      policyId = (result as { id: string }).id
    }

    if (!policyId) throw new Error('No policy ID')

    // 2. Handle rule
    if (showRuleEditor.value && ruleForm.value.left_operand) {
      if (ruleForm.value.exists) {
        await updateRuleMutation.mutateAsync({
          path: { id: policyId },
          body: {
            leftOperand: ruleForm.value.left_operand,
            operator: ruleForm.value.operator,
            rightOperand: ruleForm.value.right_operand,
          },
        })
      } else {
        await createRuleMutation.mutateAsync({
          path: { id: policyId },
          body: {
            leftOperand: ruleForm.value.left_operand,
            operator: ruleForm.value.operator,
            rightOperand: ruleForm.value.right_operand,
          },
        })
      }
    } else if (ruleForm.value.exists && !showRuleEditor.value) {
      // Rule was removed
      await deleteRuleMutation.mutateAsync({ path: { id: policyId } })
    }

    // 3. Handle actions — delete existing, create all
    const existingActions = actionsForm.value.filter((a) => !a.isNew && a.originalId)
    for (const action of existingActions) {
      try {
        await deleteActionMutation.mutateAsync({
          path: { id: policyId, actionId: action.originalId! },
        })
      } catch {
        // Ignore delete errors
      }
    }

    for (const action of actionsForm.value) {
      if (action.type) {
        await createActionMutation.mutateAsync({
          path: { id: policyId },
          body: {
            type: action.type,
            value: action.value,
            order: action.order,
          },
        })
      }
    }

    isExpanded.value = false
    emit('saved')
  } catch (error) {
    console.error('Failed to save policy:', error)
  } finally {
    isSaving.value = false
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
  if (!props.policy?.id) return
  const confirmed = await modal.confirm({
    title: 'Delete Policy',
    message: `Are you sure you want to delete "${props.policy.name}"?`,
    severity: 'danger',
  })
  if (!confirmed) return
  try {
    await deletePolicyMutation.mutateAsync({ path: { id: props.policy.id } })
    emit('deleted')
  } catch (error) {
    console.error('Failed to delete policy:', error)
  }
}
</script>

<template>
  <div class="rounded-lg border bg-card text-card-foreground shadow-sm">
    <!-- Collapsed header -->
    <div class="flex items-center gap-3 px-4 py-3 cursor-pointer select-none" @click="toggleExpand">
      <Switch
        v-model="policyForm.enabled"
        @click.stop
        @update:model-value="handleToggleEnabled"
        class="shrink-0"
      />
      <span class="font-medium truncate" :class="{ 'text-muted-foreground': !policyForm.enabled }">
        {{ policyForm.name || (isNew ? 'New Policy' : 'Unnamed Policy') }}
      </span>
      <span v-if="!isExpanded" class="text-sm text-muted-foreground truncate ml-2 hidden sm:inline">
        {{ summary }}
      </span>
      <div class="ml-auto shrink-0">
        <ChevronUp v-if="isExpanded" class="size-4 text-muted-foreground" />
        <ChevronDown v-else class="size-4 text-muted-foreground" />
      </div>
    </div>

    <!-- Expanded content -->
    <div v-if="isExpanded" class="px-4 pb-4">
      <Separator class="mb-4" />

      <!-- Policy metadata -->
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-6">
        <div class="flex flex-col gap-2">
          <Label>Name</Label>
          <Input
            v-model="policyForm.name"
            placeholder="Policy name"
            :class="{ 'border-destructive': nameError }"
            @input="nameError = false"
          />
          <p v-if="nameError" class="text-sm text-destructive">Name is required.</p>
        </div>
        <div class="flex flex-col gap-2">
          <Label>Description</Label>
          <Input v-model="policyForm.description" placeholder="Optional description" />
        </div>
      </div>

      <!-- Rule section -->
      <div class="mb-6">
        <div class="flex items-center justify-between mb-3">
          <span class="text-sm font-medium">Rule</span>
          <Button v-if="!showRuleEditor" size="sm" variant="outline" @click="handleAddRule">
            <Plus class="size-3 mr-1" />
            Add Rule
          </Button>
        </div>

        <div v-if="showRuleEditor" class="flex items-start gap-2">
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-2 flex-1">
            <!-- Left operand (field) -->
            <Select
              :model-value="ruleForm.left_operand"
              @update:model-value="handleLeftOperandChange"
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
            <Select v-model="ruleForm.operator" :disabled="!selectedField">
              <SelectTrigger class="w-full">
                <SelectValue placeholder="Operator" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="op in validOperators" :key="op.value" :value="op.value">
                  {{ op.label }}
                </SelectItem>
              </SelectContent>
            </Select>

            <!-- Right operand -->
            <Select
              v-if="
                selectedField &&
                (selectedField.type === 'enum' ||
                  selectedField.type === 'dynamic' ||
                  selectedField.type === 'boolean')
              "
              v-model="ruleForm.right_operand"
            >
              <SelectTrigger class="w-full">
                <SelectValue placeholder="Select value" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="option in rightOperandOptions"
                  :key="option.value"
                  :value="option.value"
                >
                  {{ option.label }}
                </SelectItem>
              </SelectContent>
            </Select>
            <Input
              v-else-if="selectedField && selectedField.type === 'number'"
              type="number"
              :model-value="Number(ruleForm.right_operand) || 0"
              @update:model-value="(val) => (ruleForm.right_operand = String(val ?? 0))"
              placeholder="Enter number"
            />
            <Input
              v-else
              v-model="ruleForm.right_operand"
              placeholder="Enter value"
              :disabled="!selectedField"
            />
          </div>

          <Button size="icon" variant="ghost" class="shrink-0 mt-0.5" @click="handleRemoveRule">
            <X class="size-4" />
          </Button>
        </div>

        <p v-if="!showRuleEditor" class="text-sm text-muted-foreground">
          No rule configured. Click "Add Rule" to add a condition.
        </p>
      </div>

      <!-- Actions section -->
      <div class="mb-6">
        <div class="flex items-center justify-between mb-3">
          <span class="text-sm font-medium">Actions</span>
          <Button size="sm" variant="outline" @click="handleAddAction">
            <Plus class="size-3 mr-1" />
            Add Action
          </Button>
        </div>

        <div v-if="actionsForm.length === 0" class="text-sm text-muted-foreground">
          No actions configured. Click "Add Action" to add one.
        </div>

        <div v-else class="space-y-2">
          <div v-for="(action, index) in actionsForm" :key="index" class="flex items-start gap-2">
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-2 flex-1">
              <!-- Action type -->
              <Select
                :model-value="action.type"
                @update:model-value="(val) => handleActionTypeChange(index, val as string)"
              >
                <SelectTrigger class="w-full">
                  <SelectValue placeholder="Select action type" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="option in policyOptions.actionTypes"
                    :key="option.value"
                    :value="option.value"
                  >
                    {{ option.label }}
                  </SelectItem>
                </SelectContent>
              </Select>

              <!-- Action value -->
              <Select
                v-if="action.type && getActionValueOptions(action.type).length > 0"
                v-model="action.value"
              >
                <SelectTrigger class="w-full">
                  <SelectValue placeholder="Select value" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="option in getActionValueOptions(action.type)"
                    :key="option.value"
                    :value="String(option.value)"
                  >
                    {{ option.label }}
                  </SelectItem>
                </SelectContent>
              </Select>
              <Input v-else-if="action.type === 'stop_processing'" model-value="N/A" disabled />
              <Input v-else v-model="action.value" placeholder="Enter value" />
            </div>

            <Button
              size="icon"
              variant="ghost"
              class="shrink-0 mt-0.5"
              @click="handleRemoveAction(index)"
            >
              <X class="size-4" />
            </Button>
          </div>
        </div>
      </div>

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
