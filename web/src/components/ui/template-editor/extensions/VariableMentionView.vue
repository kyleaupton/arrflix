<script setup lang="ts">
import { NodeViewWrapper } from '@tiptap/vue-3'
import { computed } from 'vue'

interface Props {
  node: {
    attrs: {
      path: string
      func?: 'clean' | 'sanitize' | null
      optional?: boolean
      prefix?: string
      suffix?: string
    }
  }
}

const props = defineProps<Props>()

/**
 * Format the variable label for display
 * Removes leading dot: ".Title" -> "Title"
 */
const label = computed(() => {
  const displayValue = props.node.attrs.path.startsWith('.')
    ? props.node.attrs.path.slice(1)
    : props.node.attrs.path

  if (props.node.attrs.func) {
    return `${props.node.attrs.func} ${displayValue}`
  }
  return displayValue
})

const isOptional = computed(() => !!props.node.attrs.optional)
const prefix = computed(() => props.node.attrs.prefix || '')
const suffix = computed(() => props.node.attrs.suffix || '')
</script>

<template>
  <NodeViewWrapper
    as="span"
    class="variable-mention inline-flex items-center justify-center rounded-full border px-2 py-0.5 text-xs font-medium font-mono whitespace-nowrap cursor-default mx-0.5"
    :class="
      isOptional
        ? 'border-dashed border-primary/60 bg-primary/80 text-primary-foreground'
        : 'border-transparent bg-primary text-primary-foreground'
    "
  >
    <span v-if="isOptional && prefix" class="opacity-50">{{ prefix }}</span>
    {{ label }}
    <span v-if="isOptional && suffix" class="opacity-50">{{ suffix }}</span>
  </NodeViewWrapper>
</template>

<style scoped>
.variable-mention:hover {
  opacity: 0.9;
}
</style>
