<script setup lang="ts">
import { ref } from 'vue'
import { Bell, BellOff, CheckCheck } from 'lucide-vue-next'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import { useNotifications } from '@/composables/useNotifications'
import NotificationItem from './NotificationItem.vue'

// The list is only fetched while the bell is open (see useNotifications), so the
// open flag doubles as the list-query gate.
const open = ref(false)
const { unreadCount, notifications, isLoading, isError, markRead, markAllRead } =
  useNotifications(open)
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button
        type="button"
        class="relative flex items-center rounded-md p-2 text-foreground/70 transition-colors hover:bg-white/5 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        aria-label="Notifications"
        title="Notifications"
      >
        <Bell class="size-4" />
        <span
          v-if="unreadCount > 0"
          class="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[0.625rem] font-semibold leading-none text-primary-foreground ring-2 ring-background"
        >
          {{ unreadCount > 9 ? '9+' : unreadCount }}
        </span>
      </button>
    </PopoverTrigger>

    <PopoverContent class="w-80 p-0 sm:w-96" align="end" :side-offset="8">
      <div class="flex items-center justify-between border-b px-4 py-2.5">
        <span class="text-sm font-medium">Notifications</span>
        <Button
          variant="ghost"
          size="sm"
          class="h-7 gap-1.5 px-2 text-xs text-muted-foreground"
          :disabled="unreadCount === 0"
          @click="markAllRead"
        >
          <CheckCheck class="size-3.5" />
          Mark all read
        </Button>
      </div>

      <ScrollArea class="max-h-96">
        <!-- Loading: a few skeleton rows on first open (no cached list yet). -->
        <div v-if="isLoading && notifications.length === 0" class="divide-y">
          <div v-for="i in 4" :key="i" class="flex items-start gap-3 px-4 py-3">
            <Skeleton class="h-[3.75rem] w-10 shrink-0 rounded-sm" />
            <div class="flex-1 space-y-2 py-1">
              <Skeleton class="h-3.5 w-3/4" />
              <Skeleton class="h-3 w-full" />
            </div>
          </div>
        </div>

        <!-- Error: soft, non-destructive — the badge/count still works. -->
        <div
          v-else-if="isError"
          class="flex flex-col items-center gap-1 px-4 py-10 text-center text-sm text-muted-foreground"
        >
          <span>Couldn't load notifications.</span>
        </div>

        <!-- Empty. -->
        <div
          v-else-if="notifications.length === 0"
          class="flex flex-col items-center gap-2 px-4 py-10 text-center"
        >
          <BellOff class="size-6 text-muted-foreground/50" />
          <span class="text-sm text-muted-foreground">You're all caught up</span>
        </div>

        <!-- List. -->
        <div v-else class="divide-y">
          <NotificationItem
            v-for="n in notifications"
            :key="n.id"
            :notification="n"
            @read="markRead"
          />
        </div>
      </ScrollArea>
    </PopoverContent>
  </Popover>
</template>
