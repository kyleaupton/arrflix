<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { Bell, Mail } from 'lucide-vue-next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import { Skeleton } from '@/components/ui/skeleton'
import { useNotificationPreferences } from '@/composables/useNotificationPreferences'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const { bundles, isLoading, isError, setPreference } = useNotificationPreferences()

// Friendly copy for the catalog keys the API returns. Unknown keys fall back to
// a humanized version of the slug, so a new bundle/channel still renders.
const BUNDLE_META: Record<string, { title: string; description: string }> = {
  my_requests: {
    title: 'My Requests',
    description: "Titles you've requested — tell me when they're ready to watch.",
  },
  library_activity: {
    title: 'Library Activity',
    description: 'New additions and updates across the shared library.',
  },
}

const CHANNEL_META: Record<string, { label: string; description: string; icon: unknown }> = {
  in_app: { label: 'In-app', description: 'Show in the notifications bell.', icon: Bell },
  email: { label: 'Email', description: 'Send to your email address.', icon: Mail },
}

function bundleTitle(key: string) {
  return BUNDLE_META[key]?.title ?? key.replace(/_/g, ' ')
}
function bundleDescription(key: string) {
  return BUNDLE_META[key]?.description ?? ''
}
function channelMeta(key: string) {
  return CHANNEL_META[key] ?? { label: key, description: '', icon: Bell }
}
</script>

<template>
  <div class="mx-auto flex max-w-2xl flex-col gap-6">
    <header class="flex flex-col gap-1">
      <h1 class="text-2xl font-semibold">Notifications</h1>
      <p class="text-muted-foreground">Choose how you want to be notified.</p>
    </header>

    <div v-if="isLoading" class="space-y-4">
      <Skeleton class="h-40 w-full rounded-xl" />
      <Skeleton class="h-40 w-full rounded-xl" />
    </div>

    <div
      v-else-if="isError"
      class="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive"
    >
      Couldn't load your notification preferences. Try refreshing.
    </div>

    <template v-else>
      <Card v-for="bundle in bundles" :key="bundle.bundle">
        <CardHeader>
          <CardTitle>{{ bundleTitle(bundle.bundle) }}</CardTitle>
          <CardDescription>{{ bundleDescription(bundle.bundle) }}</CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col divide-y">
          <div
            v-for="channel in bundle.channels ?? []"
            :key="channel.channel"
            class="flex items-center gap-4 py-3 first:pt-0 last:pb-0"
          >
            <component
              :is="channelMeta(channel.channel).icon"
              class="size-4 text-muted-foreground"
            />
            <div class="min-w-0 flex-1">
              <p class="text-sm font-medium">{{ channelMeta(channel.channel).label }}</p>
              <p class="text-xs text-muted-foreground">
                {{ channelMeta(channel.channel).description }}
              </p>
              <!-- Opting in ahead of setup is allowed (availability ≠ enablement),
                   so we warn rather than block when a channel can't deliver yet. -->
              <p
                v-if="channel.enabled && !channel.available"
                class="mt-1 text-xs text-amber-600 dark:text-amber-500"
              >
                <template v-if="channel.channel === 'email'">
                  Email isn't set up yet, so these won't be delivered.
                  <RouterLink
                    v-if="auth.canManageSettings"
                    to="/settings/email"
                    class="underline underline-offset-2 hover:text-foreground"
                  >
                    Set up email
                  </RouterLink>
                </template>
                <template v-else>This channel can't deliver right now.</template>
              </p>
            </div>
            <Switch
              :model-value="channel.enabled"
              :aria-label="`${channelMeta(channel.channel).label} for ${bundleTitle(bundle.bundle)}`"
              @update:model-value="(v: boolean) => setPreference(bundle.bundle, channel.channel, v)"
            />
          </div>
        </CardContent>
      </Card>
    </template>
  </div>
</template>
