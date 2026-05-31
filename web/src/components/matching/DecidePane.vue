<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { toast } from 'vue-sonner'
import { MousePointerClick, Link2Off, EyeOff, ChevronRight } from 'lucide-vue-next'
import {
  filesMatchDecisionOptions,
  filesMatchMutation,
  filesUnmatchMutation,
  filesDetachMutation,
} from '@/client/@tanstack/vue-query.gen'
import { problemMessage } from '@/lib/api'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import SuggestionCard from './SuggestionCard.vue'
import ResolverSummary from './ResolverSummary.vue'
import EvidenceView from './EvidenceView.vue'
import TmdbSearchPanel from './TmdbSearchPanel.vue'
import MatchByIdForm from './MatchByIdForm.vue'
import { outcomeLabel, outcomeVariant } from './outcome'
import { asResolverList } from './decision-types'
import type { EpisodeRefDto } from '@/client/types.gen'

const props = defineProps<{ fileId: string | undefined }>()
const emit = defineEmits<{ decided: [] }>()

const queryClient = useQueryClient()
const actionError = ref<string | null>(null)

const decisionQuery = useQuery(
  computed(() => ({
    ...filesMatchDecisionOptions({ path: { fileId: props.fileId! } }),
    enabled: !!props.fileId,
  })),
)

const decision = computed(() => decisionQuery.data.value?.current)
const snapshot = computed(() => decision.value?.parsedSnapshot)
const candidates = computed(() => decision.value?.rankedCandidates ?? [])
const resolvers = computed(() => asResolverList(decision.value?.resolversConsulted))

// The currently-chosen identity (set for low_confidence / confident_review).
// Confirming or unmatching only makes sense when one exists.
const chosenKey = computed(() => {
  const chosen = decision.value?.chosenRef
  return chosen ? `${chosen.source}:${chosen.externalId}` : null
})
const hasIdentity = computed(() => chosenKey.value !== null)

// Episode context for series matches, carried from the parsed snapshot.
const snapshotEpisode = computed<EpisodeRefDto | undefined>(() => {
  const s = snapshot.value
  if (s?.type === 'series' && s.season != null && s.episode != null) {
    return { season: s.season, episode: s.episode }
  }
  return undefined
})

function afterDecision(message: string) {
  return () => {
    toast.success(message)
    actionError.value = null
    // Partial-match keys: refresh every inbox page + the count badge, and any
    // open match-decision (the supersede chain changes after a decision).
    queryClient.invalidateQueries({ queryKey: [{ _id: 'unmatchedFilesList' }] })
    queryClient.invalidateQueries({ queryKey: [{ _id: 'filesMatchDecision' }] })
    emit('decided')
  }
}
function onError(err: unknown) {
  actionError.value = problemMessage(err, 'Action failed')
}

const matchMut = useMutation({
  ...filesMatchMutation(),
  onSuccess: afterDecision('Matched'),
  onError,
})
const unmatchMut = useMutation({
  ...filesUnmatchMutation(),
  onSuccess: afterDecision('Identity cleared'),
  onError,
})
const detachMut = useMutation({
  ...filesDetachMutation(),
  onSuccess: afterDecision('Removed from inbox'),
  onError,
})

const pending = computed(
  () => matchMut.isPending.value || unmatchMut.isPending.value || detachMut.isPending.value,
)

function applyMatch(part: {
  source: string
  externalId: string
  episode?: EpisodeRefDto
  edition?: string
  type?: string
}) {
  if (!props.fileId) return
  const episode = part.episode ?? snapshotEpisode.value
  matchMut.mutate({
    path: { fileId: props.fileId },
    body: {
      external: { source: part.source as 'tmdb' | 'imdb' | 'tvdb', externalId: part.externalId },
      ...(episode ? { episode } : {}),
      ...(part.edition ? { edition: part.edition } : {}),
    },
  })
}
function matchCandidate(ref: { source: string; externalId: string }) {
  applyMatch({ source: ref.source, externalId: ref.externalId })
}
function unmatch() {
  if (props.fileId) unmatchMut.mutate({ path: { fileId: props.fileId } })
}
function detach() {
  if (props.fileId) detachMut.mutate({ path: { fileId: props.fileId }, body: {} })
}

const loadError = computed(() =>
  problemMessage(decisionQuery.error.value, 'Failed to load decision'),
)
</script>

<template>
  <Empty v-if="!fileId" class="h-full">
    <EmptyHeader>
      <EmptyMedia variant="icon"><MousePointerClick /></EmptyMedia>
      <EmptyTitle>Select a file</EmptyTitle>
      <EmptyDescription>Pick an item from the inbox to review and match it.</EmptyDescription>
    </EmptyHeader>
  </Empty>

  <div v-else-if="decisionQuery.isLoading.value" class="space-y-3">
    <Skeleton class="h-6 w-2/3" />
    <Skeleton class="h-4 w-full" />
    <Skeleton class="h-24 w-full" />
  </div>

  <div v-else-if="decisionQuery.isError.value" class="p-4 text-sm text-destructive">
    {{ loadError }}
  </div>

  <div v-else-if="decision" class="space-y-4">
    <!-- Header: parsed identity + banding -->
    <div class="space-y-1.5">
      <div class="flex items-center gap-2">
        <h2 class="text-lg font-semibold">{{ snapshot?.title || 'Unidentified file' }}</h2>
        <span v-if="snapshot?.year" class="text-sm text-muted-foreground">{{ snapshot.year }}</span>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <Badge :variant="outcomeVariant(decision.outcome)">{{
          outcomeLabel(decision.outcome)
        }}</Badge>
        <Badge v-if="snapshot?.type" variant="outline" class="text-[10px]">{{
          snapshot.type
        }}</Badge>
        <span class="text-xs text-muted-foreground"
          >{{ Math.round(decision.confidence * 100) }}% confidence</span
        >
      </div>
    </div>

    <div v-if="actionError" class="rounded-md bg-destructive/10 p-2 text-sm text-destructive">
      {{ actionError }}
    </div>

    <!-- Ranked suggestions -->
    <div class="space-y-2">
      <h3 class="text-sm font-medium">Suggestions</h3>
      <template v-if="candidates.length">
        <SuggestionCard
          v-for="c in candidates"
          :key="c.externalRef.source + c.externalRef.externalId"
          :candidate="c"
          :is-current="chosenKey === `${c.externalRef.source}:${c.externalRef.externalId}`"
          :disabled="pending"
          @match="matchCandidate(c.externalRef)"
        />
      </template>
      <p v-else class="text-sm text-muted-foreground">
        No suggestions — search TMDB or enter an ID below.
      </p>
    </div>

    <!-- Why this result? — resolver evidence, tucked away for the few who want it -->
    <Collapsible class="group rounded-lg border bg-muted/30">
      <CollapsibleTrigger
        class="flex w-full items-center gap-1 px-3 py-2 text-xs text-muted-foreground hover:text-foreground"
      >
        <ChevronRight class="size-3 transition-transform group-data-[state=open]:rotate-90" />
        Why this result?
      </CollapsibleTrigger>
      <CollapsibleContent class="space-y-2 px-3 pb-3">
        <ResolverSummary :resolvers="resolvers" />
        <EvidenceView :evidence="decision.evidence" :truncated="decision.evidenceTruncated" />
      </CollapsibleContent>
    </Collapsible>

    <Separator />

    <!-- Manual fallback -->
    <div class="space-y-2">
      <h3 class="text-sm font-medium">Can't find it?</h3>
      <Tabs default-value="search">
        <TabsList class="grid w-full grid-cols-2">
          <TabsTrigger value="search">Search TMDB</TabsTrigger>
          <TabsTrigger value="id">Match by ID</TabsTrigger>
        </TabsList>
        <TabsContent value="search" class="pt-2">
          <TmdbSearchPanel
            :initial-query="snapshot?.title"
            :disabled="pending"
            @match="applyMatch"
          />
        </TabsContent>
        <TabsContent value="id" class="pt-2">
          <MatchByIdForm
            :is-series="snapshot?.type === 'series'"
            :disabled="pending"
            @match="applyMatch"
          />
        </TabsContent>
      </Tabs>
    </div>

    <Separator />

    <!-- Rejections -->
    <div class="flex flex-wrap gap-2">
      <Button v-if="hasIdentity" variant="outline" size="sm" :disabled="pending" @click="unmatch">
        <Link2Off class="mr-1.5 size-3.5" /> Clear identity
      </Button>
      <Button
        variant="ghost"
        size="sm"
        class="text-muted-foreground"
        :disabled="pending"
        @click="detach"
      >
        <EyeOff class="mr-1.5 size-3.5" /> Not a media file
      </Button>
    </div>
  </div>
</template>
