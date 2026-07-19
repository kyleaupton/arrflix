<script setup lang="ts">
import { ref, watch, watchEffect, onMounted, onBeforeUnmount, useSlots } from 'vue'
import {
  VBottomSheet,
  VBottomSheetDialogManager,
  type BottomSheetElement,
  type SnapPositionChangeEventDetail,
} from 'pure-web-bottom-sheet/vue'
import type { BottomSheetSnapPoint, BottomSheetSnapEvent } from './types'
import SafeAreaBars from '@/components/debug/SafeAreaBars.vue'

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
  // Re-arm the swipe-dismiss front-run for this open (see onSheetScroll).
  dismissArmed = false
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

// Front-run the library's swipe-dismiss. pure-web decides you've swiped closed by
// watching the sheet scroll settle — via `scrollend` where supported, else a fixed
// 100ms-after-last-scroll timer. On engines without `scrollend` (notably iOS
// Safari / standalone) that 100ms leaves the backdrop up after a *slow* swipe has
// already carried the sheet off-screen. The host is the scroll container and lands
// at scrollTop<=1 on the dismiss detent (the library's own threshold), so close
// the moment it gets there — same instant path as a backdrop tap, no settle wait.
// Armed only once the sheet has opened past the dismiss zone, so the open
// animation (which also passes through scrollTop 0) can't self-close.
let dismissArmed = false
function onSheetScroll() {
  const el = sheet.value
  if (!el) return
  if (el.scrollTop > 8) dismissArmed = true
  else if (dismissArmed && props.swipeToDismiss && el.scrollTop <= 1) hide()
}

onMounted(() => {
  sheet.value?.addEventListener('snap-position-change', onSnapChange)
  sheet.value?.addEventListener('scroll', onSheetScroll, { passive: true })
})
onBeforeUnmount(() => {
  sheet.value?.removeEventListener('snap-position-change', onSnapChange)
  sheet.value?.removeEventListener('scroll', onSheetScroll)
})

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

        <!-- Padding + borders live on these light-DOM slot wrappers (styled by the
             document, so Tailwind applies), NOT on the shadow ::part scroll-rails
             — see the style block for why. The footerless content wrapper owns the
             bottom safe-area inset so consumers never reapply it. -->
        <div v-if="slots.header" slot="header" class="border-b border-border px-4 py-2.5">
          <slot name="header" />
        </div>
        <div
          :class="[
            'px-4 pt-4',
            slots.footer ? 'pb-4' : 'pb-[calc(env(safe-area-inset-bottom)_+_1rem)]',
          ]"
        >
          <slot />
        </div>
        <div
          v-if="slots.footer"
          slot="footer"
          class="border-t border-border px-4 pt-3 pb-[calc(env(safe-area-inset-bottom)_+_0.75rem)]"
        >
          <slot name="footer" />
        </div>
      </VBottomSheet>

      <!-- Developer safe-area guide (no-op unless the debug flag is on). Lives
           inside the <dialog> so it renders in the top layer, painting over the
           open sheet the way the app-root overlay can't. -->
      <SafeAreaBars />
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
  /* The sheet host is focusable (tabindex=0) and takes focus when the dialog opens,
     which the library reflects as a ring around the handle — noise on a touch
     surface. Suppress it; !important is required to beat the shadow-internal
     :host(:focus-visible) .handle rule across the part boundary. */
  outline: none !important;
}
/* Neutralize the library's padding on the header/content/footer scroll-rails.
   Each rail carries a horizontal-scroll sentinel (::after { padding: inherit;
   width: calc(100% + 1px) }); any inline padding here is inherited into that
   sentinel and becomes over-wide scroll space that clips edge content (full-width
   buttons, long lines). Real padding + borders live on the light-DOM slot
   wrappers instead; zeroing here leaves only the intended 1px sentinel. */
bottom-sheet::part(header),
bottom-sheet::part(content),
bottom-sheet::part(footer) {
  padding: 0;
}

/* The dialog-manager resets and slides the <dialog> itself; we only dim the
   backdrop. Timing is asymmetric: the base (close) transition is fast so the
   backdrop doesn't linger after a swipe-dismiss — where the sheet is already gone
   by the time the dialog's close fires — while the [open] (open) transition stays
   slow to fade in over the sheet's slide-up. A CSS transition reads its duration
   from the state being transitioned *to*, so these don't cross-contaminate. */
.bottom-sheet-dialog::backdrop {
  background: rgb(0 0 0 / 0%);
  transition:
    background 0.25s ease-out,
    overlay 0.25s ease-out allow-discrete,
    display 0.25s ease-out allow-discrete;
}
.bottom-sheet-dialog[open]::backdrop {
  background: rgb(0 0 0 / 50%);
  transition:
    background 0.45s ease-out,
    overlay 0.45s ease-out allow-discrete,
    display 0.45s ease-out allow-discrete;
}
@starting-style {
  .bottom-sheet-dialog[open]::backdrop {
    background: rgb(0 0 0 / 0%);
  }
}
</style>
