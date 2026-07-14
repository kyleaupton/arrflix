<script setup lang="ts">
import { ref } from 'vue'
import { toast } from 'vue-sonner'
import { BellRing, Laptop, Send, Trash2, MonitorSmartphone } from 'lucide-vue-next'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import EnablePushDialog from './EnablePushDialog.vue'
import { usePushSubscription } from '@/composables/usePushSubscription'
import { problemMessage } from '@/lib/api'
import { timeAgo } from '@/lib/timeAgo'
import type { PushSubscriptionView } from '@/client/types.gen'

const {
  supported,
  secureContext,
  needsInstall,
  devices,
  isLoading,
  isError,
  isThisDeviceSubscribed,
  isCurrentDevice,
  isEnabling,
  enableError,
  enableOnThisDevice,
  removeDevice,
  removingId,
  testDevice,
  testingId,
} = usePushSubscription()

const dialogOpen = ref(false)

async function confirmEnable() {
  const ok = await enableOnThisDevice()
  if (ok) {
    dialogOpen.value = false
    toast.success('Push notifications enabled on this device')
  }
  // On failure the dialog stays open; the reason renders inline (enableError).
}

async function remove(device: PushSubscriptionView) {
  try {
    await removeDevice(device.id)
    toast.success('Device removed')
  } catch (err) {
    toast.error(problemMessage(err, 'Could not remove device'))
  }
}

async function test(device: PushSubscriptionView) {
  try {
    await testDevice(device.id)
    toast.success('Test notification sent')
  } catch (err) {
    toast.error(problemMessage(err, 'Test failed'))
  }
}

// A friendly label from the stored User-Agent. Best-effort — an unknown UA still
// renders a sensible fallback rather than a raw string.
function deviceLabel(ua: string | null): string {
  if (!ua) return 'Unknown device'
  const browser = /edg/i.test(ua)
    ? 'Edge'
    : /chrome|crios/i.test(ua)
      ? 'Chrome'
      : /firefox|fxios/i.test(ua)
        ? 'Firefox'
        : /safari/i.test(ua)
          ? 'Safari'
          : 'Browser'
  const os = /android/i.test(ua)
    ? 'Android'
    : /iphone|ipad|ipod/i.test(ua)
      ? 'iOS'
      : /mac os/i.test(ua)
        ? 'macOS'
        : /windows/i.test(ua)
          ? 'Windows'
          : /linux/i.test(ua)
            ? 'Linux'
            : ''
  return os ? `${browser} on ${os}` : browser
}
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle class="flex items-center gap-2">
        <BellRing class="size-5" />
        Push notifications
      </CardTitle>
      <CardDescription>
        Receive alerts on your devices, even when Arrflix isn't open. Push is enabled per device.
      </CardDescription>
    </CardHeader>

    <CardContent class="flex flex-col gap-4">
      <!-- This-device enablement / capability state -->
      <div
        v-if="needsInstall"
        class="rounded-lg border bg-muted/40 p-3 text-sm text-muted-foreground"
      >
        To enable push on iOS, add Arrflix to your Home Screen first, then open it from there.
      </div>
      <div
        v-else-if="!supported"
        class="rounded-lg border bg-muted/40 p-3 text-sm text-muted-foreground"
      >
        This browser doesn't support push notifications.
      </div>
      <div
        v-else-if="!secureContext"
        class="rounded-lg border bg-muted/40 p-3 text-sm text-muted-foreground"
      >
        Push notifications need a secure connection (HTTPS, or localhost during development).
      </div>

      <div
        v-else-if="isThisDeviceSubscribed"
        class="flex items-center gap-2 rounded-lg border border-primary/30 bg-primary/5 p-3 text-sm"
      >
        <BellRing class="size-4 text-primary" />
        This device is set up to receive push notifications.
      </div>

      <div
        v-else
        class="flex flex-col items-start gap-3 rounded-lg border p-3 sm:flex-row sm:items-center sm:justify-between"
      >
        <p class="text-sm text-muted-foreground">This device isn't set up for push yet.</p>
        <Button size="sm" @click="dialogOpen = true">Enable on this device</Button>
      </div>

      <!-- Registered devices -->
      <div class="flex flex-col gap-2">
        <p class="text-xs font-medium tracking-wide text-muted-foreground uppercase">
          Your devices
        </p>

        <div v-if="isLoading" class="space-y-2">
          <Skeleton class="h-14 w-full rounded-lg" />
          <Skeleton class="h-14 w-full rounded-lg" />
        </div>

        <p v-else-if="isError" class="text-sm text-destructive">Couldn't load your devices.</p>

        <p v-else-if="devices.length === 0" class="text-sm text-muted-foreground">
          No devices are registered for push yet.
        </p>

        <ul v-else class="flex flex-col gap-2">
          <li
            v-for="device in devices"
            :key="device.id"
            class="flex items-center gap-3 rounded-lg border p-3"
          >
            <component
              :is="isCurrentDevice(device) ? Laptop : MonitorSmartphone"
              class="size-5 shrink-0 text-muted-foreground"
            />
            <div class="min-w-0 flex-1">
              <p class="flex items-center gap-2 text-sm font-medium">
                {{ deviceLabel(device.userAgent) }}
                <span
                  v-if="isCurrentDevice(device)"
                  class="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-normal text-primary"
                >
                  This device
                </span>
              </p>
              <p class="text-xs text-muted-foreground">
                Subscribed {{ timeAgo(device.createdAt) }}
              </p>
            </div>
            <Button
              variant="ghost"
              size="sm"
              :disabled="testingId === device.id"
              @click="test(device)"
            >
              <Send class="size-4" />
              <span class="sr-only sm:not-sr-only">Test</span>
            </Button>
            <Button
              variant="ghost"
              size="sm"
              class="text-muted-foreground hover:text-destructive"
              :disabled="removingId === device.id"
              @click="remove(device)"
            >
              <Trash2 class="size-4" />
              <span class="sr-only">Remove</span>
            </Button>
          </li>
        </ul>
      </div>
    </CardContent>
  </Card>

  <EnablePushDialog
    :open="dialogOpen"
    :loading="isEnabling"
    :error="enableError"
    @update:open="dialogOpen = $event"
    @confirm="confirmEnable"
  />
</template>
