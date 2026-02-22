<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import type { Token } from './TemplateTokenEditor.vue'
import { Trash2, ToggleLeft, ToggleRight } from 'lucide-vue-next'

interface Props {
  position: { x: number; y: number }
  token: Token
}

const props = defineProps<Props>()

const emit = defineEmits<{
  toggleOptional: []
  updateOptional: [attrs: { prefix: string; suffix: string }]
  delete: []
  close: []
}>()

const isOptional = computed(() => !!props.token.optional)

const prefixInput = ref(props.token.prefix || '')
const suffixInput = ref(props.token.suffix || '')

// Sync inputs when token changes
watch(
  () => props.token,
  (token) => {
    prefixInput.value = token.prefix || ''
    suffixInput.value = token.suffix || ''
  },
)

function handleToggleOptional() {
  emit('toggleOptional')
}

function handlePrefixSuffixUpdate() {
  emit('updateOptional', {
    prefix: prefixInput.value,
    suffix: suffixInput.value,
  })
}

function handleDelete() {
  emit('delete')
}

function handleKeyDown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    emit('close')
  }
}

function handleClickOutside(event: MouseEvent) {
  const target = event.target as HTMLElement
  if (!target.closest('.token-context-menu')) {
    emit('close')
  }
}

onMounted(() => {
  document.addEventListener('keydown', handleKeyDown)
  setTimeout(() => {
    document.addEventListener('click', handleClickOutside)
  }, 10)
})

onUnmounted(() => {
  document.removeEventListener('keydown', handleKeyDown)
  document.removeEventListener('click', handleClickOutside)
})

// Teleport into the dialog content element so the menu is inside the dialog's
// focus trap and dismissable layer, but outside the overflow-auto scroll container.
// The dialog content has a CSS transform, so position: fixed won't work relative
// to the viewport — we need to adjust coordinates to be relative to the dialog.
const teleportTarget = ref<string | Element>('body')
const adjustedPosition = ref({ x: 0, y: 0 })

onMounted(() => {
  const dialogContent = document.querySelector('[data-slot="dialog-content"]')
  if (dialogContent) {
    teleportTarget.value = dialogContent
    const rect = dialogContent.getBoundingClientRect()
    adjustedPosition.value = {
      x: props.position.x - rect.left,
      y: props.position.y - rect.top,
    }
  } else {
    // Fallback: no dialog, use body with original coordinates
    adjustedPosition.value = props.position
  }
})
</script>

<template>
  <Teleport :to="teleportTarget">
    <div
      class="token-context-menu fixed z-[200] min-w-56 rounded-md border bg-popover p-1 text-popover-foreground shadow-md"
      :style="{
        top: `${adjustedPosition.y}px`,
        left: `${adjustedPosition.x}px`,
      }"
      @pointerdown.stop
      @focusin.stop
      @mousedown.stop
      @click.stop
    >
      <!-- Token label -->
      <div class="px-2 py-1.5 text-xs font-semibold font-mono text-muted-foreground">
        {{
          token.func
            ? `${token.func} ${token.value.replace(/^\./, '')}`
            : `${token.value.replace(/^\./, '')}`
        }}
      </div>

      <div class="h-px bg-border my-1" />

      <!-- Toggle optional -->
      <button
        class="flex w-full cursor-default items-center rounded-sm px-2 py-1.5 text-sm outline-none hover:bg-accent hover:text-accent-foreground"
        @click="handleToggleOptional"
      >
        <component :is="isOptional ? ToggleRight : ToggleLeft" class="mr-2 h-4 w-4" />
        <span>{{ isOptional ? 'Make Required' : 'Make Optional' }}</span>
      </button>

      <!-- Prefix/suffix inputs (only when optional) -->
      <div v-if="isOptional" class="px-2 py-1.5 space-y-1.5">
        <div class="flex items-center gap-2">
          <label class="text-xs text-muted-foreground w-10 shrink-0">Prefix</label>
          <input
            v-model="prefixInput"
            class="flex-1 h-6 rounded border border-input bg-background px-1.5 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-ring"
            placeholder="e.g. ' - '"
            @blur="handlePrefixSuffixUpdate"
            @keydown.enter="handlePrefixSuffixUpdate"
          />
        </div>
        <div class="flex items-center gap-2">
          <label class="text-xs text-muted-foreground w-10 shrink-0">Suffix</label>
          <input
            v-model="suffixInput"
            class="flex-1 h-6 rounded border border-input bg-background px-1.5 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-ring"
            placeholder="e.g. ']'"
            @blur="handlePrefixSuffixUpdate"
            @keydown.enter="handlePrefixSuffixUpdate"
          />
        </div>
      </div>

      <div class="h-px bg-border my-1" />

      <!-- Delete token -->
      <button
        class="flex w-full cursor-default items-center rounded-sm px-2 py-1.5 text-sm outline-none text-destructive hover:bg-destructive/10"
        @click="handleDelete"
      >
        <Trash2 class="mr-2 h-4 w-4" />
        <span>Delete</span>
      </button>
    </div>
  </Teleport>
</template>

<style scoped>
.token-context-menu {
  animation: fadeIn 0.1s ease-out;
}

@keyframes fadeIn {
  from {
    opacity: 0;
    transform: scale(0.95);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}
</style>
