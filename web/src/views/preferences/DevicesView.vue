<script setup lang="ts">
import { computed, ref } from 'vue'
import { toast } from 'vue-sonner'
import { Laptop, MonitorSmartphone, Send, BellRing, BellOff, LogOut } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import EnablePushDialog from '@/components/notifications/EnablePushDialog.vue'
import { useDevices } from '@/composables/useDevices'
import { deviceLabel } from '@/lib/deviceLabel'
import { timeAgo } from '@/lib/timeAgo'
import { problemMessage } from '@/lib/api'
import type { SessionView } from '@/client/types.gen'

const {
  devices,
  isLoading,
  isError,
  supported,
  secureContext,
  needsInstall,
  isEnabling,
  enableError,
  enableOnThisDevice,
  revokeDevice,
  revokingId,
  revokeOthers,
  isRevokingOthers,
  testDevice,
  testingId,
  disablePush,
  disablingId,
} = useDevices()

const enableOpen = ref(false)
const confirmLogoutOthersOpen = ref(false)

// Current device pinned first, then most-recently-active. lastUsedAt advances on
// each refresh, so it tracks real activity better than createdAt.
const sortedDevices = computed(() =>
  [...devices.value].sort((a, b) => {
    if (a.isCurrent !== b.isCurrent) return a.isCurrent ? -1 : 1
    return new Date(b.lastUsedAt).getTime() - new Date(a.lastUsedAt).getTime()
  }),
)
const otherCount = computed(() => devices.value.filter((d) => !d.isCurrent).length)

async function confirmEnable() {
  const ok = await enableOnThisDevice()
  if (ok) {
    enableOpen.value = false
    toast.success('Push notifications enabled on this device')
  }
  // On failure the dialog stays open; the reason renders inline (enableError).
}

async function test(session: SessionView) {
  if (!session.pushSubscriptionId) return
  try {
    await testDevice(session.pushSubscriptionId)
    toast.success('Test notification sent')
  } catch (err) {
    toast.error(problemMessage(err, 'Test failed'))
  }
}

async function turnOffPush(session: SessionView) {
  try {
    await disablePush(session)
    toast.success('Push turned off for this device')
  } catch (err) {
    toast.error(problemMessage(err, 'Could not turn off push'))
  }
}

async function logOut(session: SessionView) {
  try {
    await revokeDevice(session.id)
    toast.success('Device logged out')
  } catch (err) {
    toast.error(problemMessage(err, 'Could not log out device'))
  }
}

async function logOutOthers() {
  try {
    await revokeOthers()
    confirmLogoutOthersOpen.value = false
    toast.success('Other devices logged out')
  } catch (err) {
    toast.error(problemMessage(err, 'Could not log out other devices'))
  }
}
</script>

<template>
  <div class="mx-auto flex max-w-2xl flex-col gap-6">
    <header class="flex flex-wrap items-start justify-between gap-3">
      <div class="flex flex-col gap-1">
        <h1 class="text-2xl font-semibold">Devices</h1>
        <p class="text-muted-foreground">
          Where you're signed in, and where push notifications are delivered.
        </p>
      </div>
      <Button
        v-if="otherCount > 0"
        variant="outline"
        size="sm"
        :disabled="isRevokingOthers"
        @click="confirmLogoutOthersOpen = true"
      >
        <LogOut class="size-4" />
        Log out other devices
      </Button>
    </header>

    <div v-if="isLoading" class="space-y-2">
      <Skeleton class="h-16 w-full rounded-lg" />
      <Skeleton class="h-16 w-full rounded-lg" />
    </div>

    <p v-else-if="isError" class="text-sm text-destructive">Couldn't load your devices.</p>

    <ul v-else class="flex flex-col gap-2">
      <li
        v-for="device in sortedDevices"
        :key="device.id"
        class="flex flex-wrap items-center gap-3 rounded-lg border p-3"
      >
        <component
          :is="device.isCurrent ? Laptop : MonitorSmartphone"
          class="size-5 shrink-0 text-muted-foreground"
        />

        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-medium">
            <span>{{ deviceLabel(device.userAgent) }}</span>
            <Badge
              v-if="device.isCurrent"
              variant="secondary"
              class="bg-primary/10 font-normal text-primary"
            >
              This device
            </Badge>
          </div>
          <p class="text-xs text-muted-foreground">Last active {{ timeAgo(device.lastUsedAt) }}</p>
        </div>

        <!-- Push cluster: enable is only possible on the current device (needs this
             browser's permission + PushManager); test and turn-off work anywhere. -->
        <div class="flex items-center gap-1.5">
          <template v-if="device.pushEnabled">
            <Badge variant="secondary" class="gap-1 font-normal">
              <BellRing class="size-3" />
              Push on
            </Badge>
            <Button
              variant="ghost"
              size="sm"
              :disabled="testingId === device.pushSubscriptionId"
              @click="test(device)"
            >
              <Send class="size-4" />
              <span class="sr-only sm:not-sr-only">Test</span>
            </Button>
            <Button
              variant="ghost"
              size="sm"
              class="text-muted-foreground hover:text-destructive"
              :disabled="disablingId === device.pushSubscriptionId"
              @click="turnOffPush(device)"
            >
              <BellOff class="size-4" />
              <span class="sr-only">Turn off push</span>
            </Button>
          </template>

          <template v-else-if="device.isCurrent">
            <span v-if="needsInstall" class="text-xs text-muted-foreground">
              Add to Home Screen to enable push
            </span>
            <span v-else-if="!supported" class="text-xs text-muted-foreground">
              Push unsupported here
            </span>
            <span v-else-if="!secureContext" class="text-xs text-muted-foreground">
              Needs a secure connection
            </span>
            <Button v-else variant="outline" size="sm" @click="enableOpen = true">
              <BellRing class="size-4" />
              Enable push
            </Button>
          </template>

          <span v-else class="text-xs text-muted-foreground">Push off</span>
        </div>

        <!-- Log out: the current device can't be logged out from here (use the
             account menu); "Log out other devices" covers the rest in bulk. -->
        <Tooltip v-if="device.isCurrent">
          <TooltipTrigger as-child>
            <span tabindex="-1">
              <Button variant="ghost" size="sm" disabled class="text-muted-foreground">
                <LogOut class="size-4" />
                <span class="sr-only">Log out</span>
              </Button>
            </span>
          </TooltipTrigger>
          <TooltipContent>This is the device you're using now</TooltipContent>
        </Tooltip>
        <Button
          v-else
          variant="ghost"
          size="sm"
          class="text-muted-foreground hover:text-destructive"
          :disabled="revokingId === device.id"
          @click="logOut(device)"
        >
          <LogOut class="size-4" />
          <span class="sr-only sm:not-sr-only">Log out</span>
        </Button>
      </li>
    </ul>
  </div>

  <EnablePushDialog
    :open="enableOpen"
    :loading="isEnabling"
    :error="enableError"
    @update:open="enableOpen = $event"
    @confirm="confirmEnable"
  />

  <Dialog :open="confirmLogoutOthersOpen" @update:open="confirmLogoutOthersOpen = $event">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>Log out other devices?</DialogTitle>
        <DialogDescription>
          Signs out every device except this one. They'll need to log in again, and push
          notifications on them stop until re-enabled.
        </DialogDescription>
      </DialogHeader>
      <DialogFooter class="gap-2 sm:justify-end">
        <Button variant="ghost" :disabled="isRevokingOthers" @click="confirmLogoutOthersOpen = false">
          Cancel
        </Button>
        <Button variant="destructive" :disabled="isRevokingOthers" @click="logOutOthers">
          {{
            isRevokingOthers
              ? 'Logging out…'
              : `Log out ${otherCount} ${otherCount === 1 ? 'device' : 'devices'}`
          }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
