<script setup lang="ts">
import { computed } from 'vue'
import type { InboxNotification } from '@/client/types.gen'
import { timeAgo } from '@/lib/timeAgo'

const props = defineProps<{
  notification: InboxNotification
}>()

const emit = defineEmits<{
  read: [id: string]
}>()

const isUnread = computed(() => !props.notification.readAt)

// The payload is an opaque bag of template extras (see InboxNotification). Read
// the one field the card uses defensively — a poster thumbnail when the event
// carried media, nothing otherwise.
const posterUrl = computed(() => {
  const payload = props.notification.payload as { media?: { posterPath?: string } } | null
  const path = payload?.media?.posterPath
  if (!path) return null
  return `https://image.tmdb.org/t/p/w92/${path.replace(/^\//, '')}`
})

const when = computed(() => timeAgo(props.notification.createdAt))
</script>

<template>
  <button
    type="button"
    class="flex w-full items-start gap-3 px-4 py-3 text-left transition-colors hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
    :class="{ 'bg-primary/5': isUnread }"
    @click="emit('read', notification.id)"
  >
    <!-- Poster thumbnail, or a neutral placeholder so text aligns across rows. -->
    <div
      class="shrink-0 overflow-hidden rounded-sm bg-muted"
      style="width: 2.5rem; height: 3.75rem"
    >
      <img
        v-if="posterUrl"
        :src="posterUrl"
        :alt="notification.title"
        class="h-full w-full object-cover"
        loading="lazy"
      />
    </div>

    <div class="min-w-0 flex-1">
      <div class="flex items-start gap-2">
        <p class="min-w-0 flex-1 text-sm font-medium leading-snug text-foreground">
          {{ notification.title }}
        </p>
        <!-- Unread dot: the one at-a-glance marker of what's new in the list. -->
        <span
          v-if="isUnread"
          class="mt-1 size-2 shrink-0 rounded-full bg-primary"
          aria-label="Unread"
        />
      </div>
      <p class="mt-0.5 line-clamp-2 text-sm text-muted-foreground">
        {{ notification.body }}
      </p>
      <p class="mt-1 text-xs text-muted-foreground/70">{{ when }}</p>
    </div>
  </button>
</template>
