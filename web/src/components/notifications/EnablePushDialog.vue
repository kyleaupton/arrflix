<script setup lang="ts">
import { BellRing, Check } from 'lucide-vue-next'
import { ResponsiveDialog } from '@/components/responsive-dialog'
import { Button } from '@/components/ui/button'

// A deliberately un-automatic prompt: iOS/Safari block a permission request that
// isn't tied to a user gesture, so we make the case first and only call the
// browser API when the user presses Enable (the parent handles the request in the
// confirm handler). ResponsiveDialog renders this as a centered dialog on desktop
// and a bottom sheet on mobile.
defineProps<{ open: boolean; loading?: boolean; error?: string | null }>()
const emit = defineEmits<{ 'update:open': [boolean]; confirm: [] }>()

const benefits = [
  'A heads-up the moment a title you requested is ready to watch',
  'Only what you opt into — no noise, no marketing',
  'Turn it off anytime, on any device',
]
</script>

<template>
  <ResponsiveDialog
    :open="open"
    title="Get notified the moment it's ready"
    @update:open="(v: boolean) => emit('update:open', v)"
  >
    <template #header>
      <div
        class="mx-auto mb-2 flex size-12 items-center justify-center rounded-full bg-primary/10"
      >
        <BellRing class="size-6 text-primary" />
      </div>
      <h2 class="text-center text-lg leading-none font-semibold">
        Get notified the moment it's ready
      </h2>
      <p class="text-center text-sm text-muted-foreground">
        Let this device notify you as soon as something you requested lands in the library — no need
        to keep checking back.
      </p>
    </template>

    <ul class="my-2 space-y-2.5">
      <li v-for="b in benefits" :key="b" class="flex items-start gap-2.5 text-sm">
        <Check class="mt-0.5 size-4 shrink-0 text-primary" />
        <span class="text-muted-foreground">{{ b }}</span>
      </li>
    </ul>

    <p v-if="error" class="text-sm text-destructive">{{ error }}</p>

    <template #footer>
      <Button variant="ghost" :disabled="loading" @click="emit('update:open', false)">
        Not now
      </Button>
      <Button :disabled="loading" @click="emit('confirm')">
        {{ loading ? 'Enabling…' : 'Enable notifications' }}
      </Button>
    </template>
  </ResponsiveDialog>
</template>
