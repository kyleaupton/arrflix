<script setup lang="ts">
import { BellRing, Check } from 'lucide-vue-next'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

// A deliberately un-automatic prompt: iOS/Safari block a permission request that
// isn't tied to a user gesture, so we make the case first and only call the
// browser API when the user presses Enable (parent handles the actual request in
// the click handler). Plain centered Dialog for now — the mobile drawer is a
// later design pass.
defineProps<{ open: boolean; loading?: boolean; error?: string | null }>()
const emit = defineEmits<{ 'update:open': [boolean]; confirm: [] }>()

const benefits = [
  'A heads-up the moment a title you requested is ready to watch',
  'Only what you opt into — no noise, no marketing',
  'Turn it off anytime, on any device',
]
</script>

<template>
  <Dialog :open="open" @update:open="(v: boolean) => emit('update:open', v)">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <div
          class="mx-auto mb-2 flex size-12 items-center justify-center rounded-full bg-primary/10"
        >
          <BellRing class="size-6 text-primary" />
        </div>
        <DialogTitle class="text-center">Get notified the moment it's ready</DialogTitle>
        <DialogDescription class="text-center">
          Let this device notify you as soon as something you requested lands in the library — no
          need to keep checking back.
        </DialogDescription>
      </DialogHeader>

      <ul class="my-2 space-y-2.5">
        <li v-for="b in benefits" :key="b" class="flex items-start gap-2.5 text-sm">
          <Check class="mt-0.5 size-4 shrink-0 text-primary" />
          <span class="text-muted-foreground">{{ b }}</span>
        </li>
      </ul>

      <p v-if="error" class="text-sm text-destructive">{{ error }}</p>

      <DialogFooter class="gap-2 sm:justify-center">
        <Button variant="ghost" :disabled="loading" @click="emit('update:open', false)">
          Not now
        </Button>
        <Button :disabled="loading" @click="emit('confirm')">
          {{ loading ? 'Enabling…' : 'Enable notifications' }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
