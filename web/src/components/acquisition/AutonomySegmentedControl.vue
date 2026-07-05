<script setup lang="ts">
import { cn } from '@/lib/utils'

type Autonomy = 'auto' | 'propose' | 'manual'

defineProps<{
  modelValue: Autonomy
  label: string
  disabled?: boolean
}>()

const emit = defineEmits<{ 'update:modelValue': [value: Autonomy] }>()

// Trust ladder, low → high automation. Labels mirror the tracking spec's
// heuristic: manual "just tell me what I'm missing", propose "show me your work
// first", auto "I trust the system".
const options: { value: Autonomy; label: string }[] = [
  { value: 'auto', label: 'Automatic' },
  { value: 'propose', label: 'Suggest first' },
  { value: 'manual', label: "I'll pick" },
]
</script>

<template>
  <div
    role="radiogroup"
    :aria-label="label"
    class="inline-flex rounded-md border bg-muted/40 p-0.5"
  >
    <button
      v-for="opt in options"
      :key="opt.value"
      type="button"
      role="radio"
      :aria-checked="modelValue === opt.value"
      :disabled="disabled"
      :class="
        cn(
          'rounded px-2.5 py-1 text-xs font-medium transition-colors disabled:opacity-50',
          modelValue === opt.value
            ? 'bg-background text-foreground shadow-sm'
            : 'text-muted-foreground hover:text-foreground',
        )
      "
      @click="emit('update:modelValue', opt.value)"
    >
      {{ opt.label }}
    </button>
  </div>
</template>
