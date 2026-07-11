<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { toast } from 'vue-sonner'
import { Inbox, ClipboardCheck } from 'lucide-vue-next'
import {
  requestsListOptions,
  requestsListQueryKey,
  requestsApproveMutation,
  requestsCancelMutation,
} from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import RequestCard from '@/components/acquisition/RequestCard.vue'
import DenyRequestDialog from '@/components/modals/DenyRequestDialog.vue'
import { useModal } from '@/composables/useModal'
import { useAuthStore } from '@/stores/auth'
import { problemMessage } from '@/lib/api'
import type { Request } from '@/client/types.gen'

const auth = useAuthStore()
const modal = useModal()
const queryClient = useQueryClient()

const activeTab = ref('mine')

// My Requests reads the whole list and filters to the caller: a view.any holder
// gets everyone's requests from this endpoint, so the self-filter is what makes
// the tab "mine". A view.own requester already only sees their own.
const { data: myList, isLoading: myLoading } = useQuery(requestsListOptions({}))
const myRequests = computed(() =>
  (myList.value ?? []).filter((r) => r.requestedBy === auth.user?.sub),
)

// Review Queue is the pending-only slice, fetched only for approvers.
const { data: queueList, isLoading: queueLoading } = useQuery(
  computed(() => ({
    ...requestsListOptions({ query: { status: 'pending' } }),
    enabled: auth.canReviewRequests,
  })),
)
const queueRequests = computed(() => queueList.value ?? [])

function invalidateRequests() {
  queryClient.invalidateQueries({ queryKey: requestsListQueryKey() })
}

// A request is withdrawable while it hasn't reached a terminal decision.
const CANCELABLE = ['pending', 'approved', 'spawned']
function isCancelable(status: string): boolean {
  return CANCELABLE.includes(status)
}

const cancelRequest = useMutation({
  ...requestsCancelMutation(),
  onSuccess: () => {
    toast.success('Request canceled')
    invalidateRequests()
  },
  onError: (err) => toast.error(problemMessage(err, 'Failed to cancel request')),
})

const approveRequest = useMutation({
  ...requestsApproveMutation(),
  onSuccess: () => {
    toast.success('Request approved')
    invalidateRequests()
  },
  // Covers the concurrent-approver race: a 409 "not pending" renders here.
  onError: (err) => toast.error(problemMessage(err, 'Failed to approve request')),
})

async function handleCancel(req: Request) {
  const ok = await modal.confirm({
    title: 'Cancel request',
    message: 'Withdraw this request?',
    confirmLabel: 'Withdraw',
    severity: 'danger',
  })
  if (!ok) return
  cancelRequest.mutate({ path: { id: req.id } })
}

async function handleApprove(req: Request) {
  const ok = await modal.confirm({
    title: 'Approve request',
    message: 'Approve this request and start tracking the title?',
    confirmLabel: 'Approve',
    severity: 'success',
  })
  if (!ok) return
  approveRequest.mutate({ path: { id: req.id } })
}

function handleDeny(req: Request) {
  modal.open(DenyRequestDialog, {
    props: { requestId: req.id, type: req.type as 'movie' | 'series' },
  })
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <div>
      <h1 class="text-2xl font-semibold">Requests</h1>
      <p class="text-sm text-muted-foreground">Track your requests and review the queue</p>
    </div>

    <Tabs v-model="activeTab">
      <TabsList>
        <TabsTrigger value="mine">My Requests</TabsTrigger>
        <TabsTrigger v-if="auth.canReviewRequests" value="queue">Review Queue</TabsTrigger>
      </TabsList>

      <!-- My Requests -->
      <TabsContent value="mine" class="pt-4">
        <div v-if="myLoading" class="space-y-3">
          <Skeleton class="h-20 w-full rounded-lg" />
          <Skeleton class="h-20 w-full rounded-lg" />
        </div>
        <div
          v-else-if="!myRequests.length"
          class="flex flex-col items-center justify-center py-16 text-muted-foreground"
        >
          <Inbox class="mb-3 size-10 opacity-50" />
          <p class="text-sm">You haven't requested anything yet</p>
        </div>
        <div v-else class="space-y-3">
          <RequestCard v-for="req in myRequests" :key="req.id" :request="req">
            <template #actions>
              <Button
                v-if="auth.canCancelRequests && isCancelable(req.status)"
                variant="outline"
                size="sm"
                :disabled="cancelRequest.isPending.value"
                @click="handleCancel(req)"
              >
                Cancel
              </Button>
            </template>
          </RequestCard>
        </div>
      </TabsContent>

      <!-- Review Queue -->
      <TabsContent v-if="auth.canReviewRequests" value="queue" class="pt-4">
        <div v-if="queueLoading" class="space-y-3">
          <Skeleton class="h-20 w-full rounded-lg" />
          <Skeleton class="h-20 w-full rounded-lg" />
        </div>
        <div
          v-else-if="!queueRequests.length"
          class="flex flex-col items-center justify-center py-16 text-muted-foreground"
        >
          <ClipboardCheck class="mb-3 size-10 opacity-50" />
          <p class="text-sm">Nothing awaiting approval</p>
        </div>
        <div v-else class="space-y-3">
          <RequestCard v-for="req in queueRequests" :key="req.id" :request="req" show-requester>
            <template #actions>
              <Button
                v-if="auth.canApprove(req.type as 'movie' | 'series')"
                size="sm"
                :disabled="approveRequest.isPending.value"
                @click="handleApprove(req)"
              >
                Approve
              </Button>
              <Button
                v-if="auth.canDeny(req.type as 'movie' | 'series')"
                variant="outline"
                size="sm"
                @click="handleDeny(req)"
              >
                Deny
              </Button>
            </template>
          </RequestCard>
        </div>
      </TabsContent>
    </Tabs>
  </div>
</template>
