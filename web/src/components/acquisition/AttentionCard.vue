<script setup lang="ts">
import { computed } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { toast } from 'vue-sonner'
import { Sparkles, Check, X } from 'lucide-vue-next'
import {
  trackingByTmdbOptions,
  trackingProposalsOptions,
  trackingProposalsQueryKey,
  proposalsApproveMutation,
  proposalsDeclineMutation,
} from '@/client/@tanstack/vue-query.gen'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { formatBytes } from '@/lib/format'
import { renderBinKey } from '@/composables/useQualityBins'
import { problemMessage } from '@/lib/api'

// What the tracking needs from an admin, in one place. Today that is release
// suggestions (proposals parked by a 'propose'-mode segment); pending requests
// will join the same card. Self-contained: it resolves the tracking id from the
// tmdb id and owns the approve/decline mutations, so a page only has to drop it
// under the hero. Renders nothing when there is nothing to act on.
const props = defineProps<{
  tmdbId: number
  type: 'movie' | 'series'
}>()

const queryClient = useQueryClient()

// Match each page's own trackingByTmdb args exactly so this shares their cache
// entry rather than issuing a second fetch: series keys on type, movie does not.
const trackingArg = computed(() =>
  props.type === 'series'
    ? { path: { tmdbId: props.tmdbId }, query: { type: 'series' as const } }
    : { path: { tmdbId: props.tmdbId } },
)

const { data: tracking } = useQuery(
  computed(() => ({ ...trackingByTmdbOptions(trackingArg.value), retry: false })),
)

const trackingId = computed(() => tracking.value?.tracking?.id)

const { data: proposals } = useQuery(
  computed(() => ({
    ...trackingProposalsOptions({ path: { id: trackingId.value! } }),
    enabled: !!trackingId.value,
  })),
)

// A pack proposal covers several episodes but is one decision, so the proposals
// list already carries it once — no per-episode fan-out here.
const rows = computed(() => proposals.value ?? [])

function invalidate() {
  queryClient.invalidateQueries({
    queryKey: trackingProposalsQueryKey({ path: { id: trackingId.value! } }),
  })
  queryClient.invalidateQueries({ queryKey: [{ _id: 'trackingByTmdb' }] })
}

const approve = useMutation({
  ...proposalsApproveMutation(),
  onSuccess: () => {
    toast.success('Approved — grabbing now')
    invalidate()
  },
  onError: (err) => toast.error(problemMessage(err, 'Failed to approve')),
})

const decline = useMutation({
  ...proposalsDeclineMutation(),
  onSuccess: () => {
    toast.success('Declined — searching for a different release')
    invalidate()
  },
  onError: (err) => toast.error(problemMessage(err, 'Failed to decline')),
})

const pending = computed(() => approve.isPending.value || decline.isPending.value)
</script>

<template>
  <Card v-if="rows.length">
    <CardHeader>
      <CardTitle class="text-base">Needs your attention</CardTitle>
    </CardHeader>
    <CardContent class="flex flex-col gap-3">
      <div
        v-for="p in rows"
        :key="p.id"
        class="flex flex-col gap-3 rounded-lg border p-3 sm:flex-row sm:items-center sm:justify-between"
      >
        <div class="min-w-0 space-y-1">
          <div class="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
            <Sparkles class="size-3.5" />
            Suggested download
          </div>
          <p class="truncate text-sm font-medium" :title="p.candidateTitle">
            {{ p.candidateTitle }}
          </p>
          <div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <Badge variant="secondary">{{ renderBinKey(p.bin, []) }}</Badge>
            <span>{{ formatBytes(p.size) }}</span>
            <span>{{ p.seeders }} seeders</span>
            <span v-if="p.isPack"> covers {{ p.coveredEpisodeIds?.length ?? 0 }} episodes </span>
          </div>
        </div>
        <div class="flex shrink-0 items-center gap-2">
          <Button size="sm" :disabled="pending" @click="approve.mutate({ path: { id: p.id } })">
            <Check class="mr-1.5 size-4" />
            Approve
          </Button>
          <Button
            size="sm"
            variant="outline"
            :disabled="pending"
            @click="decline.mutate({ path: { id: p.id } })"
          >
            <X class="mr-1.5 size-4" />
            Decline
          </Button>
        </div>
      </div>
    </CardContent>
  </Card>
</template>
