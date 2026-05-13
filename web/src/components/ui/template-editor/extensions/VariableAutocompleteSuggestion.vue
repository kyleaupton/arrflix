<script setup lang="ts">
import { computed, ref, watch, onMounted } from 'vue'
import { useTemplateVariables, type TemplateVariable } from '@/composables/useTemplateVariables'
import { ScrollArea } from '@/components/ui/scroll-area'

interface Props {
  query: string
  mediaType?: 'movie' | 'series'
  command: (props: any) => void
}

const props = withDefaults(defineProps<Props>(), {
  mediaType: 'movie',
})

const { variablesByNamespace, searchVariables, isLoading } = useTemplateVariables({
  mediaType: props.mediaType,
})

const selectedIndex = ref(0)

// Filter variables based on search
const filteredVariables = computed(() => {
  if (!props.query) {
    return variablesByNamespace.value
  }

  const results = searchVariables(props.query)

  // Group by namespace
  const grouped: Record<string, TemplateVariable[]> = {}
  for (const variable of results) {
    if (!grouped[variable.namespace]) {
      grouped[variable.namespace] = []
    }
    grouped[variable.namespace]!.push(variable)
  }

  return grouped
})

// Order of namespaces for display
const namespaceOrder = ['Media', 'Quality', 'Release', 'Candidate', 'MediaInfo']

const orderedNamespaces = computed(() => {
  const groups = filteredVariables.value
  return namespaceOrder.filter((ns) => groups[ns] && groups[ns].length > 0)
})

const hasResults = computed(() => {
  return orderedNamespaces.value.length > 0
})

// Flatten all variables for keyboard navigation
const allVariables = computed(() => {
  const vars: TemplateVariable[] = []
  for (const namespace of orderedNamespaces.value) {
    const nsVars = filteredVariables.value[namespace]
    if (nsVars) {
      vars.push(...nsVars)
    }
  }
  return vars
})

// Reset selected index when results change
watch(allVariables, () => {
  selectedIndex.value = 0
})

function selectVariable(variable: TemplateVariable) {
  props.command(variable)
}

function selectByIndex(index: number) {
  const variable = allVariables.value[index]
  if (variable) {
    selectVariable(variable)
  }
}

// Expose keyboard navigation for Tiptap
function onKeyDown(props: { event: KeyboardEvent }): boolean {
  if (props.event.key === 'ArrowUp') {
    selectedIndex.value = Math.max(0, selectedIndex.value - 1)
    return true
  }

  if (props.event.key === 'ArrowDown') {
    selectedIndex.value = Math.min(allVariables.value.length - 1, selectedIndex.value + 1)
    return true
  }

  if (props.event.key === 'Enter') {
    selectByIndex(selectedIndex.value)
    return true
  }

  return false
}

defineExpose({
  onKeyDown,
})
</script>

<template>
  <div
    class="variable-autocomplete w-72 rounded-md border bg-popover p-2 text-popover-foreground shadow-md"
  >
    <div v-if="isLoading" class="py-6 text-center text-sm text-muted-foreground">
      Loading variables...
    </div>

    <div v-else-if="!hasResults" class="py-6 text-center text-sm text-muted-foreground">
      No variables found.
    </div>

    <ScrollArea v-else class="h-[300px]">
      <div v-for="namespace in orderedNamespaces" :key="namespace" class="mb-3 last:mb-0">
        <div class="px-2 py-1.5 text-xs font-semibold text-muted-foreground">
          {{ namespace }}
        </div>
        <div
          v-for="(variable, idx) in filteredVariables[namespace]"
          :key="variable.path"
          :class="[
            'flex items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-sm cursor-pointer',
            allVariables.indexOf(variable) === selectedIndex
              ? 'bg-accent text-accent-foreground'
              : 'hover:bg-accent/50',
          ]"
          @click="selectVariable(variable)"
          @mouseenter="selectedIndex = allVariables.indexOf(variable)"
        >
          <div class="flex flex-col gap-0.5">
            <span class="font-medium">{{ variable.label }}</span>
            <code class="text-xs text-muted-foreground">{{ variable.path }}</code>
          </div>
          <span
            v-if="variable.postDownloadOnly"
            class="text-xs text-amber-500"
            title="Only available after download"
          >
            post-DL
          </span>
        </div>
      </div>
    </ScrollArea>
  </div>
</template>

<style scoped>
.variable-autocomplete {
  animation: fadeIn 0.15s ease-out;
}

@keyframes fadeIn {
  from {
    opacity: 0;
    transform: translateY(-4px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
</style>
