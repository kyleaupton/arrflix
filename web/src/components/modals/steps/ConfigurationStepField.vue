<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { InfoIcon } from 'lucide-vue-next'
import { useMutation } from '@tanstack/vue-query'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Checkbox } from '@/components/ui/checkbox'
import { Eye, EyeOff } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { type IndexerOutput, type FieldOutput } from '@/client/types.gen'
import { indexersActionMutation } from '@/client/@tanstack/vue-query.gen'
import { cn } from '@/lib/utils'

const model = computed({
  get: () => props.field.value,
  set: (value) => {
    emit('value-change', props.field.name, value)
  },
})

const emit = defineEmits<{
  (e: 'value-change', fieldName: string, value: unknown): void
}>()

const props = defineProps<{
  selectedIndexer: IndexerOutput
  field: FieldOutput
}>()

const options = computed(() => {
  if (props.field.selectOptions) {
    return props.field.selectOptions.map(
      (option: { name: string; value: number; hint: string }) => ({
        label: option.name,
        value: option.value,
        hint: option.hint,
      }),
    )
  } else if (isAsyncAction.value && actionMutation.data?.value) {
    // @ts-expect-error shit ain't got types
    return actionMutation.data.value.options || []
  }

  return []
})

const isMultiSelect = computed(() => {
  // Check if the field should support multiple selections
  // This could be based on field name or other properties
  return (
    props.field.name.includes('searchTypes') ||
    props.field.name.includes('languagesOnly') ||
    Array.isArray(model.value)
  )
})

const hasHelpText = computed(() => {
  return (
    props.field.helpText ||
    ('helpTextWarning' in props.field ? props.field.helpTextWarning : undefined)
  )
})

const helpTextWarning = computed<string | undefined>(() => {
  if (!('helpTextWarning' in props.field)) return undefined
  const value = (props.field as Record<string, unknown>).helpTextWarning
  return typeof value === 'string' ? value : undefined
})

const fieldUnit = computed(() => {
  return 'unit' in props.field ? props.field.unit : undefined
})

const isFloat = computed(() => {
  return 'isFloat' in props.field ? props.field.isFloat : false
})

const fieldId = computed(() => `field-${props.field.name}`)

const isAsyncAction = computed(() => {
  return props.field.selectOptionsProviderAction
})

const selectOptionLabel = computed(() => (isAsyncAction.value ? 'name' : 'label'))

const actionMutation = useMutation({
  ...indexersActionMutation(),
  onSuccess: (data) => {
    console.log('Action performed successfully', data)
  },
  onError: (error) => {
    console.error('Failed to perform action:', error)
  },
})

const performAction = () => {
  if (props.field.selectOptionsProviderAction) {
    actionMutation.mutate({
      path: { name: props.field.selectOptionsProviderAction },
      body: props.selectedIndexer,
    })
  }
}

const showPassword = ref(false)

onMounted(() => {
  performAction()
})
</script>

<template>
  <div
    class="field-container space-y-2"
    :class="{ hidden: field.hidden === 'hidden', 'advanced-field': field.advanced }"
  >
    <!-- Text Input Fields -->
    <template v-if="field.type === 'textbox'">
      <Label :for="fieldId" class="flex items-center gap-1">
        {{ field.label }}
        <span v-if="field.advanced" class="text-xs text-muted-foreground">(Advanced)</span>
      </Label>
      <Input
        :id="fieldId"
        v-model="model as string"
        :placeholder="`Enter ${field.label}`"
        :class="cn('w-full', helpTextWarning && 'border-destructive')"
        :aria-invalid="helpTextWarning ? 'true' : undefined"
      />
    </template>

    <!-- Password Input Fields -->
    <template v-else-if="field.type === 'password'">
      <Label :for="fieldId" class="flex items-center gap-1">
        {{ field.label }}
        <span v-if="field.advanced" class="text-xs text-muted-foreground">(Advanced)</span>
      </Label>
      <div class="relative">
        <Input
          :id="fieldId"
          v-model="model as string"
          :type="showPassword ? 'text' : 'password'"
          :placeholder="`Enter ${field.label}`"
          :class="cn('w-full pr-10', helpTextWarning && 'border-destructive')"
          :aria-invalid="helpTextWarning ? 'true' : undefined"
        />
        <Button
          type="button"
          variant="ghost"
          size="icon"
          class="absolute right-0 top-0 h-full px-3 py-2 hover:bg-transparent"
          @click="showPassword = !showPassword"
        >
          <Eye v-if="!showPassword" class="size-4 text-muted-foreground" />
          <EyeOff v-else class="size-4 text-muted-foreground" />
          <span class="sr-only">{{ showPassword ? 'Hide' : 'Show' }} password</span>
        </Button>
      </div>
    </template>

    <!-- Number Input Fields -->
    <template v-else-if="field.type === 'number'">
      <Label :for="fieldId" class="flex items-center gap-1">
        {{ field.label }}
        <span v-if="fieldUnit" class="text-xs text-muted-foreground">({{ fieldUnit }})</span>
        <span v-if="field.advanced" class="text-xs text-muted-foreground">(Advanced)</span>
      </Label>
      <Input
        :id="fieldId"
        v-model="model as string"
        type="number"
        :placeholder="`Enter ${field.label}`"
        :step="isFloat ? 0.01 : 1"
        :class="cn('w-full', helpTextWarning && 'border-destructive')"
        :aria-invalid="helpTextWarning ? 'true' : undefined"
        @update:model-value="
          (val) => {
            if (val === '' || val === null) {
              model = undefined
            } else {
              const num = isFloat ? parseFloat(String(val)) : parseInt(String(val), 10)
              model = isNaN(num) ? undefined : num
            }
          }
        "
      />
    </template>

    <!-- Select Fields -->
    <template v-else-if="field.type === 'select'">
      <Label :for="fieldId" class="flex items-center gap-1">
        {{ field.label }}
        <span v-if="field.advanced" class="text-xs text-muted-foreground">(Advanced)</span>
      </Label>
      <!-- MultiSelect: Simple implementation using comma-separated display -->
      <div v-if="isMultiSelect" class="space-y-2">
        <div class="flex flex-wrap gap-2">
          <div v-for="option in options" :key="option.value" class="flex items-center gap-2">
            <Checkbox
              :id="`${fieldId}-${option.value}`"
              :checked="Array.isArray(model) && model.includes(option.value)"
              @update:checked="
                (checked: boolean) => {
                  const current = Array.isArray(model) ? [...model] : []
                  if (checked) {
                    if (!current.includes(option.value)) {
                      model = [...current, option.value]
                    }
                  } else {
                    model = current.filter((v) => v !== option.value)
                  }
                }
              "
            />
            <Label :for="`${fieldId}-${option.value}`" class="cursor-pointer text-sm font-normal">
              {{ option[selectOptionLabel] }}
            </Label>
          </div>
        </div>
        <p v-if="Array.isArray(model) && model.length > 0" class="text-xs text-muted-foreground">
          {{ model.length }} item{{ model.length !== 1 ? 's' : '' }} selected
        </p>
      </div>
      <!-- Single Select -->
      <Select
        v-else
        :model-value="model !== null && model !== undefined ? String(model) : undefined"
        @update:model-value="
          (val: unknown) => {
            if (val === undefined || val === null || val === '') {
              model = undefined
            } else {
              // Try to preserve original type if possible
              const matchingOption = options.find(
                (opt: { value: unknown; label?: string; name?: string }) =>
                  String(opt.value) === String(val),
              )
              model = matchingOption ? matchingOption.value : val
            }
          }
        "
        :disabled="options.length === 0"
      >
        <SelectTrigger
          :id="fieldId"
          :class="cn('w-full', helpTextWarning && 'border-destructive')"
          :aria-invalid="helpTextWarning ? 'true' : undefined"
        >
          <SelectValue :placeholder="`Select ${field.label}`" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem v-for="option in options" :key="option.value" :value="String(option.value)">
            {{ option[selectOptionLabel] }}
          </SelectItem>
        </SelectContent>
      </Select>
    </template>

    <!-- Checkbox Fields -->
    <template v-else-if="field.type === 'checkbox'">
      <div class="flex items-center gap-2">
        <Checkbox v-model="model as boolean" :id="fieldId" />
        <Label :for="fieldId" class="cursor-pointer flex items-center gap-1">
          {{ field.label }}
          <span v-if="field.advanced" class="text-xs text-muted-foreground">(Advanced)</span>
        </Label>
      </div>
    </template>

    <!-- Info Fields -->
    <template v-else-if="field.type === 'info'">
      <Alert :id="fieldId">
        <InfoIcon />
        <AlertTitle>{{ field.label }}</AlertTitle>
        <AlertDescription v-html="field.value as string" />
      </Alert>
    </template>

    <!-- Fallback for unknown field types -->
    <template v-else>
      <div
        class="rounded-md border border-yellow-200 bg-yellow-50 p-4 dark:border-yellow-800 dark:bg-yellow-950"
      >
        <p class="text-sm font-medium text-yellow-800 dark:text-yellow-200">
          <strong>Unknown field type:</strong> {{ field.type }}
        </p>
        <p class="mt-1 text-sm text-yellow-700 dark:text-yellow-300">
          Field: {{ field.name }} ({{ field.label }})
        </p>
      </div>
    </template>

    <!-- Help Text -->
    <div
      v-if="hasHelpText"
      :class="
        cn(
          'text-sm',
          helpTextWarning ? 'text-yellow-600 dark:text-yellow-400' : 'text-muted-foreground',
        )
      "
    >
      {{ field.helpText || helpTextWarning }}
      <a
        v-if="field.helpLink"
        :href="field.helpLink"
        target="_blank"
        class="ml-1 text-primary hover:underline"
      >
        Learn more
      </a>
    </div>
  </div>
</template>
