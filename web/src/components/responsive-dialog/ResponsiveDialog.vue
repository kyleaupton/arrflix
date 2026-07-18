<script setup lang="ts">
import { ref, watch, useSlots } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { BottomSheet } from '@/components/bottom-sheet'

// A viewport-adaptive dialog: a centered modal on desktop, a native-feeling
// bottom sheet on mobile, from one authored surface. Content is authored once via
// slots; this component only chooses the frame and maps the shared regions
// (header / body / footer) onto whichever primitive is showing. The frame is
// frozen while open (see below) so a mid-dialog resize/rotate never remounts into
// the other primitive.

const props = withDefaults(
  defineProps<{
    /** Accessible name. Rendered as the visible header unless #header overrides it. */
    title: string
    /** Optional supporting line under the title in the default header. */
    description?: string
    /** Force a frame, or let the viewport decide (default). */
    mode?: 'responsive' | 'dialog' | 'sheet'
  }>(),
  { description: undefined, mode: 'responsive' },
)

const open = defineModel<boolean>('open', { default: false })
const slots = useSlots()

// Below md → bottom sheet; at/above → centered dialog. 768px matches the app's
// existing mobile breakpoint (SidebarProvider).
const isMobile = useMediaQuery('(max-width: 768px)')

function resolveSheet(): boolean {
  if (props.mode === 'dialog') return false
  if (props.mode === 'sheet') return true
  return isMobile.value
}

// Freeze the frame on the rising edge of `open`: swapping primitives mid-open
// would remount and drop state. flush:'sync' sets it before the frame renders;
// immediate covers a mount that starts open.
const asSheet = ref(resolveSheet())
watch(
  open,
  (isOpen) => {
    if (isOpen) asSheet.value = resolveSheet()
  },
  { immediate: true, flush: 'sync' },
)
</script>

<template>
  <!-- Sheet frame (mobile). content-height sizes it to the content rather than a
       fixed detent, so short dialogs don't open to a full-height sheet. -->
  <BottomSheet v-if="asSheet" v-model:open="open" content-height>
    <template #header>
      <template v-if="slots.header">
        <!-- Custom header owns the visuals; the sr-only title still names the sheet. -->
        <h2 class="sr-only">{{ title }}</h2>
        <slot name="header" />
      </template>
      <div v-else class="flex flex-col gap-2 text-center sm:text-left">
        <h2 class="text-lg leading-none font-semibold">{{ title }}</h2>
        <p v-if="description" class="text-muted-foreground text-sm">{{ description }}</p>
      </div>
    </template>

    <slot />

    <!-- Stacked, full-width actions read better under the thumb on a phone. -->
    <template v-if="slots.footer" #footer>
      <div class="flex flex-col-reverse gap-2">
        <slot name="footer" />
      </div>
    </template>
  </BottomSheet>

  <!-- Dialog frame (desktop). -->
  <Dialog v-else v-model:open="open">
    <DialogContent>
      <DialogHeader>
        <!-- DialogContent requires a DialogTitle for its accessible name; when a
             custom #header supplies its own visible title, this one goes sr-only. -->
        <DialogTitle :class="slots.header ? 'sr-only' : undefined">{{ title }}</DialogTitle>
        <slot v-if="slots.header" name="header" />
        <DialogDescription v-else-if="description">{{ description }}</DialogDescription>
      </DialogHeader>

      <slot />

      <DialogFooter v-if="slots.footer">
        <slot name="footer" />
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
