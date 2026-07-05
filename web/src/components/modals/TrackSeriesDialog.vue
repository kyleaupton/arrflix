<script setup lang="ts">
import { ref, computed, inject } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { requestsCreateMutation, trackingByTmdbQueryKey } from '@/client/@tanstack/vue-query.gen'
import { toast } from 'vue-sonner'
import { Check } from 'lucide-vue-next'
import BaseDialog from './BaseDialog.vue'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import AutonomySegmentedControl from '@/components/acquisition/AutonomySegmentedControl.vue'
import { cn } from '@/lib/utils'
import { problemMessage } from '@/lib/api'

type Autonomy = 'auto' | 'propose' | 'manual'
type ScopeRule = 'all' | 'future_only'

// Two personas share this dialog. A requester sees at most one question —
// scope, phrased as watch intent — and their submission may go to a pending
// request. An operator (`isOperator`) additionally gets quality and per-segment
// autonomy, and their submission spawns a tracking immediately. Quality and
// autonomy are operator policy, so requesters never pick them; the backend
// defaults both. `airedEpisodeCount` / `hasOngoing` decide whether the scope
// question is even meaningful: with no back-catalog or no upcoming episodes
// there is only one sensible answer, so the question is skipped entirely.
const props = withDefaults(
  defineProps<{
    tmdbId: number
    title: string
    airedEpisodeCount: number
    seasonCount: number
    hasOngoing: boolean
    isOperator: boolean
    defaultTier?: 'HD' | '4K'
    defaultScope?: ScopeRule
    defaultBackfill?: Autonomy
    defaultOngoing?: Autonomy
  }>(),
  {
    defaultTier: 'HD',
    defaultScope: 'all',
    defaultBackfill: 'auto',
    defaultOngoing: 'auto',
  },
)

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const queryClient = useQueryClient()

const tier = ref<'HD' | '4K'>(props.defaultTier)
const scopeRule = ref<ScopeRule>(props.defaultScope)
const backfill = ref<Autonomy>(props.defaultBackfill)
const ongoing = ref<Autonomy>(props.defaultOngoing)
const error = ref<string | null>(null)

// The scope question only distinguishes anything when the series has both a
// back-catalog and episodes still to come. Otherwise both answers collapse to
// the same want set, so ask nothing and submit 'all'.
const askScope = computed(() => props.airedEpisodeCount > 0 && props.hasOngoing)
const effectiveScope = computed<ScopeRule>(() => (askScope.value ? scopeRule.value : 'all'))

const scopeOptions = computed(() => [
  {
    value: 'all' as ScopeRule,
    title: 'Start from the beginning',
    subtitle: `All ${props.airedEpisodeCount} aired episode${props.airedEpisodeCount === 1 ? '' : 's'} · ${props.seasonCount} season${props.seasonCount === 1 ? '' : 's'}, plus new ones`,
  },
  {
    value: 'future_only' as ScopeRule,
    title: 'Just new episodes',
    subtitle: 'Only episodes that air from now on',
  },
])

// Back-catalog autonomy only applies when backfill wants will exist: some
// episodes aired and the chosen scope keeps them.
const showBackfill = computed(
  () => props.isOperator && effectiveScope.value === 'all' && props.airedEpisodeCount > 0,
)
const showOngoing = computed(() => props.isOperator && props.hasOngoing)

const trackingKey = computed(() =>
  trackingByTmdbQueryKey({ path: { tmdbId: props.tmdbId }, query: { type: 'series' } }),
)

const createRequest = useMutation({
  ...requestsCreateMutation(),
  onSuccess: (req) => {
    // A spawned tracking means the request auto-approved and is already acting on
    // the chosen config; otherwise it awaits operator approval.
    if (req.spawnedTrackingId) {
      toast.success('Tracking — acting on your choices now')
    } else {
      toast.success('Requested — pending approval')
    }
    queryClient.invalidateQueries({ queryKey: trackingKey.value })
    dialogRef.value.close({ saved: true })
  },
  onError: (err) => {
    error.value = problemMessage(err, 'Failed to submit request')
  },
})

function handleSubmit() {
  createRequest.mutate({
    body: {
      tmdbId: props.tmdbId,
      type: 'series',
      scopeRule: effectiveScope.value,
      // Quality and autonomy are operator policy; requesters omit them and the
      // backend applies its defaults (HD, auto/auto).
      ...(props.isOperator
        ? { tier: tier.value, backfillAutonomy: backfill.value, ongoingAutonomy: ongoing.value }
        : {}),
    },
  })
}
</script>

<template>
  <BaseDialog :title="`${isOperator ? 'Track' : 'Request'} ${title}`">
    <div class="flex flex-col gap-5">
      <div
        v-if="error"
        class="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
      >
        {{ error }}
      </div>

      <div
        v-if="askScope"
        role="radiogroup"
        aria-label="Which episodes"
        class="flex flex-col gap-2"
      >
        <button
          v-for="opt in scopeOptions"
          :key="opt.value"
          type="button"
          role="radio"
          :aria-checked="scopeRule === opt.value"
          :class="
            cn(
              'rounded-lg border p-3 text-left transition-colors',
              scopeRule === opt.value
                ? 'border-primary bg-primary/5'
                : 'border-border hover:border-muted-foreground/40',
            )
          "
          @click="scopeRule = opt.value"
        >
          <div class="flex items-center justify-between gap-2">
            <span class="text-sm font-medium">{{ opt.title }}</span>
            <Check v-if="scopeRule === opt.value" class="size-4 shrink-0 text-primary" />
          </div>
          <p class="mt-0.5 text-xs text-muted-foreground">{{ opt.subtitle }}</p>
        </button>
      </div>
      <!-- Degenerate cases (nothing aired yet, or an ended series) skip the
           question; say what the request covers instead of asking. -->
      <p v-else class="text-sm text-muted-foreground">
        {{
          airedEpisodeCount > 0
            ? `All ${airedEpisodeCount} episode${airedEpisodeCount === 1 ? '' : 's'} · ${seasonCount} season${seasonCount === 1 ? '' : 's'}`
            : 'Episodes will be grabbed as they air'
        }}
      </p>

      <div v-if="isOperator" class="flex items-center justify-between gap-4">
        <Label>Quality</Label>
        <Select v-model="tier">
          <SelectTrigger class="w-40" aria-label="Quality tier">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="HD">HD</SelectItem>
            <SelectItem value="4K">4K</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div v-if="showBackfill || showOngoing" class="flex flex-col gap-3 border-t pt-4">
        <p class="text-sm font-medium">Acquisition</p>

        <div v-if="showBackfill" class="flex items-center justify-between gap-4">
          <Label class="font-normal text-muted-foreground">
            Back-catalog
            <span class="text-xs">({{ airedEpisodeCount }} aired)</span>
          </Label>
          <AutonomySegmentedControl v-model="backfill" label="Back-catalog autonomy" />
        </div>

        <div v-if="showOngoing" class="flex items-center justify-between gap-4">
          <Label class="font-normal text-muted-foreground">New episodes</Label>
          <AutonomySegmentedControl v-model="ongoing" label="New episodes autonomy" />
        </div>
      </div>
    </div>

    <template #footer>
      <Button
        variant="outline"
        :disabled="createRequest.isPending.value"
        @click="dialogRef.value.close()"
      >
        Cancel
      </Button>
      <Button :disabled="createRequest.isPending.value" @click="handleSubmit">
        {{
          createRequest.isPending.value
            ? isOperator
              ? 'Tracking…'
              : 'Requesting…'
            : isOperator
              ? 'Track series'
              : 'Request'
        }}
      </Button>
    </template>
  </BaseDialog>
</template>
