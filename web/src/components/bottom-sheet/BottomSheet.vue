<script setup lang="ts">
import { ref, watch, watchEffect, onMounted, onBeforeUnmount, useSlots } from 'vue'
import {
  VBottomSheet,
  VBottomSheetDialogManager,
  type BottomSheetElement,
  type SnapPositionChangeEventDetail,
} from 'pure-web-bottom-sheet/vue'
import type { BottomSheetSnapPoint, BottomSheetSnapEvent } from './types'

// The app's blessed bottom sheet: a native-feeling, gesture-driven sheet built
// on pure-web-bottom-sheet (CSS scroll-snap + scroll-driven animation, so drag,
// momentum, and nested-scroll handoff run on the compositor rather than in JS).
//
// The library ships as a shadow-DOM web component, which two things about this
// wrapper exist to reconcile with the rest of the app:
//   1. Control — it's driven imperatively (`showModal()`/`close()`), so the
//      wrapper bridges that to a Vue `v-model:open` (and exposes open/close for
//      the ref-driven dialog system). Every dismissal path — backdrop click,
//      swipe-to-collapse, Escape, programmatic — funnels through the native
//      <dialog> `close` event, which is the single point we sync back to.
//   2. Styling — Tailwind utilities can't cross the shadow boundary, so all sheet
//      chrome is themed once in the block below by mapping the app's design
//      tokens onto the element's custom properties and ::part surface.

const props = withDefaults(
  defineProps<{
    /** Snap detents, ordered largest → smallest (DOM order the library expects). */
    snaps?: BottomSheetSnapPoint[]
    /** Content scrolls within the sheet rather than the whole sheet moving. */
    nestedScroll?: boolean
    /** With nestedScroll: content becomes scrollable only once fully expanded. */
    expandToScroll?: boolean
    /** Swipe-down past the smallest detent dismisses the sheet. */
    swipeToDismiss?: boolean
    /** Size the sheet to its content instead of --sheet-max-height. */
    contentHeight?: boolean
  }>(),
  {
    snaps: () => [{ snap: '100%', initial: true }],
    nestedScroll: false,
    expandToScroll: false,
    swipeToDismiss: true,
    contentHeight: false,
  },
)

const isOpen = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ snapChange: [BottomSheetSnapEvent] }>()
const slots = useSlots()

const dialog = ref<HTMLDialogElement | null>(null)
const sheet = ref<BottomSheetElement | null>(null)

// The element reads its modes via hasAttribute (kebab-case). Toggling attributes
// straight on the element sidesteps Vue's camelCase→attribute mangling through
// the functional wrapper, and works whether or not the element has upgraded yet
// — a pre-set attribute fires attributeChangedCallback on upgrade.
watchEffect(() => {
  const el = sheet.value
  if (!el) return
  el.toggleAttribute('nested-scroll', props.nestedScroll)
  el.toggleAttribute('expand-to-scroll', props.expandToScroll)
  el.toggleAttribute('swipe-to-dismiss', props.swipeToDismiss)
  el.toggleAttribute('content-height', props.contentHeight)
})

// showModal()/close() throw if the dialog is already in the target state, so
// both are guarded on dialog.open.
function show() {
  const d = dialog.value
  if (d && !d.open) d.showModal()
}
function hide() {
  const d = dialog.value
  if (d?.open) d.close()
}

watch(isOpen, (open) => (open ? show() : hide()))
onMounted(() => {
  if (isOpen.value) show()
})

// Native <dialog> `close` is the single funnel for every dismissal; mirror it
// back onto the model. Closing here re-runs the watcher, but hide() is a no-op
// once the dialog is already closed, so there's no loop.
function onDialogClose() {
  isOpen.value = false
}

// Bind snap-position-change on the element directly: it dispatches a hyphenated
// event name that Vue's on-prop matching doesn't reliably catch on a
// functional-wrapper root.
function onSnapChange(e: Event) {
  const detail = (e as CustomEvent<SnapPositionChangeEventDetail>).detail
  emit('snapChange', { sheetState: detail.sheetState, snapIndex: detail.snapIndex })
}
onMounted(() => sheet.value?.addEventListener('snap-position-change', onSnapChange))
onBeforeUnmount(() => sheet.value?.removeEventListener('snap-position-change', onSnapChange))

function snapToPoint(index: number, behavior: ScrollBehavior = 'smooth') {
  sheet.value?.snapToPoint(index, { behavior })
}

defineExpose({
  open: () => (isOpen.value = true),
  close: () => (isOpen.value = false),
  snapToPoint,
})
</script>

<template>
  <!-- `slot=` here projects light-DOM children into the web component's shadow
       slots — correct web-component usage, not the deprecated Vue 2 named-slot
       syntax the lint rule assumes; its autofix would rewrite these into
       <template v-slot>, which the functional wrapper silently drops. -->
  <!-- eslint-disable vue/no-deprecated-slot-attribute -->
  <VBottomSheetDialogManager>
    <dialog ref="dialog" class="bottom-sheet-dialog" @close="onDialogClose">
      <VBottomSheet ref="sheet" tabindex="0">
        <!-- Snap detents must run top→bottom (largest --snap first). Index 0 is
             the fully-expanded detent (`top`); `initial` is where it opens. -->
        <div
          v-for="(sp, i) in snaps"
          :key="i"
          slot="snap"
          :class="{ top: i === 0, initial: sp.initial }"
          :style="{ '--snap': sp.snap }"
        />

        <div v-if="slots.header" slot="header"><slot name="header" /></div>
        <slot />
        <div v-if="slots.footer" slot="footer"><slot name="footer" /></div>
      </VBottomSheet>
    </dialog>
  </VBottomSheetDialogManager>
</template>

<!-- Not scoped: the sheet's chrome lives in shadow DOM, reachable only via the
     host element, its custom properties, and ::part. Injected once regardless of
     how many BottomSheet instances mount. Design tokens come from main.css and
     re-theme automatically in light/dark. -->
<style>
bottom-sheet {
  --sheet-background: var(--background);
  /* Sheet top corners are intentionally chunkier than a card's radius. */
  --sheet-border-radius: 1rem;
  --sheet-max-height: calc(100dvh - env(safe-area-inset-top) - 12px);
  color: var(--foreground);
}
bottom-sheet::part(handle) {
  width: 2.25rem;
  background: var(--muted-foreground);
  opacity: 0.4;
}
bottom-sheet::part(header) {
  padding: 0.5rem 1rem;
  border-bottom: 1px solid var(--border);
}
bottom-sheet::part(content) {
  padding: 1rem;
}
bottom-sheet::part(footer) {
  padding: 0.75rem 1rem calc(env(safe-area-inset-bottom) + 0.75rem);
  border-top: 1px solid var(--border);
}

/* The dialog-manager resets and slides the <dialog> itself; we only dim the
   backdrop, fading it in over the same 0.5s the sheet takes to slide up. */
.bottom-sheet-dialog::backdrop {
  background: rgb(0 0 0 / 0%);
  transition:
    background 0.5s ease-out,
    overlay 0.5s ease-out allow-discrete,
    display 0.5s ease-out allow-discrete;
}
.bottom-sheet-dialog[open]::backdrop {
  background: rgb(0 0 0 / 50%);
}
@starting-style {
  .bottom-sheet-dialog[open]::backdrop {
    background: rgb(0 0 0 / 0%);
  }
}
</style>
