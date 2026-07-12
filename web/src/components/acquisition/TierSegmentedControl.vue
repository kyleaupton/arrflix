<script setup lang="ts">
import { cn } from '@/lib/utils'

type Tier = 'HD' | '4K'

// Quality-tier picker, structurally the sibling of AutonomySegmentedControl. The
// caller passes the tiers the user actually holds a grant for, so it never offers
// a tier the API would 403.
defineProps<{
  modelValue: Tier
  options: Tier[]
  label?: string
  disabled?: boolean
}>()

const emit = defineEmits<{ 'update:modelValue': [value: Tier] }>()
</script>

<template>
  <div
    role="radiogroup"
    :aria-label="label ?? 'Quality tier'"
    class="inline-flex rounded-md border bg-muted/40 p-0.5"
  >
    <button
      v-for="opt in options"
      :key="opt"
      type="button"
      role="radio"
      :aria-checked="modelValue === opt"
      :disabled="disabled"
      :class="
        cn(
          'rounded px-3 py-1 text-sm font-medium transition-colors disabled:opacity-50',
          modelValue === opt
            ? 'bg-background text-foreground shadow-sm'
            : 'text-muted-foreground hover:text-foreground',
        )
      "
      @click="emit('update:modelValue', opt)"
    >
      {{ opt }}
    </button>
  </div>
</template>
