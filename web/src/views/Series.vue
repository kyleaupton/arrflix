<template>
  <div class="flex flex-col gap-6">
    <Transition name="fade" mode="out-in">
      <MediaHeroSkeleton v-if="isLoading" key="loading" />
      <div
        v-else-if="isError"
        key="error"
        class="flex flex-col items-center justify-center py-12 text-center"
      >
        <p class="text-destructive">Failed to load series</p>
        <p class="text-sm text-muted-foreground mt-2">Please try again later</p>
      </div>
      <div v-else-if="data" key="content" class="flex flex-col gap-6">
        <MediaHero
          :title="data.title"
          :tagline="data.tagline"
          :subtitle="seriesSubTitle"
          :credits="creatorCredits"
          :overview="data.overview"
          :backdrop-url="backdropUrl"
          :chips="seriesChips"
          :full-bleed="isImmersive"
        >
          <template #poster>
            <Poster :item="data" size="large" :clickable="false" :is-downloading="isDownloading" />
          </template>
          <template v-if="data.voteAverage" #ratings>
            <RatingBadge source="tmdb" :score="data.voteAverage" :vote-count="data.voteCount" />
          </template>
          <template #actions>
            <SeriesAcquisitionControl
              :tmdb-id="id"
              :title="data.title"
              :available-count="availableEpisodeCount"
              :total-count="totalEpisodeCount"
              :aired-episode-count="airedEpisodeCount"
              :season-count="seasonCount"
              :has-ongoing="hasOngoing"
            />
          </template>
        </MediaHero>

        <div :class="isImmersive ? 'px-6 space-y-6' : 'space-y-6'">
          <NextEpisodeBanner v-if="data.nextEpisodeToAir" :episode="data.nextEpisodeToAir" />
          <div v-if="data.seasons?.length" class="space-y-4">
            <h2 class="text-xl font-semibold">Seasons</h2>

            <!-- Season pill tabs + episode card grid -->
            <Tabs v-model="selectedSeason">
              <div class="flex items-center gap-3">
                <ScrollArea class="min-w-0">
                  <TabsList class="inline-flex w-max">
                    <TabsTrigger
                      v-for="season in sortedSeasons"
                      :key="season.seasonNumber"
                      :value="String(season.seasonNumber)"
                    >
                      S{{ season.seasonNumber }}
                    </TabsTrigger>
                  </TabsList>
                  <ScrollBar orientation="horizontal" />
                </ScrollArea>

                <!-- Season-level action: download / progress -->
                <div v-if="currentSeason" class="shrink-0">
                  <template v-if="getSeasonPackJob(currentSeason.seasonNumber)">
                    <CircularProgress
                      :state="getSeasonProgressState(currentSeason.seasonNumber)"
                      :value="getSeasonProgressValue(currentSeason.seasonNumber)"
                      size="sm"
                    />
                  </template>
                  <template v-else-if="hasActiveEpisodeDownloads(currentSeason)">
                    <TooltipProvider>
                      <Tooltip>
                        <TooltipTrigger as-child>
                          <span class="flex items-center">
                            <CircularProgress state="indeterminate" size="sm" />
                          </span>
                        </TooltipTrigger>
                        <TooltipContent>
                          {{ getActiveEpisodeCount(currentSeason) }} episode(s) downloading
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  </template>
                  <template v-else>
                    <Button
                      size="sm"
                      variant="outline"
                      @click="searchForSeasonCandidates(currentSeason.seasonNumber)"
                    >
                      <Download class="size-4 mr-2" />
                      Download S{{ currentSeason.seasonNumber }}
                    </Button>
                  </template>
                </div>
              </div>

              <TabsContent
                v-for="season in sortedSeasons"
                :key="season.seasonNumber"
                :value="String(season.seasonNumber)"
                class="mt-4 space-y-4"
              >
                <p v-if="season.overview" class="text-sm text-muted-foreground">
                  {{ season.overview }}
                </p>
                <div class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
                  <div
                    v-for="episode in season.episodes"
                    :key="episode.episodeNumber"
                    class="rounded-lg border bg-card overflow-hidden"
                  >
                    <!-- Episode still image -->
                    <img
                      v-if="episode.stillPath"
                      :src="`https://image.tmdb.org/t/p/w500${episode.stillPath}`"
                      :alt="episode.title"
                      class="w-full aspect-video object-cover bg-muted"
                    />
                    <div v-else class="w-full aspect-video bg-muted" />

                    <!-- Episode info -->
                    <div class="p-3 space-y-2">
                      <div class="flex items-start justify-between gap-2">
                        <div class="min-w-0">
                          <div class="flex items-center gap-2 mb-0.5">
                            <span class="text-xs font-mono text-muted-foreground">
                              E{{ episode.episodeNumber.toString().padStart(2, '0') }}
                            </span>
                            <h4 class="font-medium text-sm truncate">
                              {{ episode.title || 'Episode ' + episode.episodeNumber }}
                            </h4>
                          </div>
                          <p v-if="episode.airDate" class="text-xs text-muted-foreground">
                            {{ episode.airDate }}
                          </p>
                        </div>
                        <!-- Episode action/status area -->
                        <div class="shrink-0">
                          <!-- Episode is available and not downloading -->
                          <template
                            v-if="
                              episode.available &&
                              !getEpisodeJob(season.seasonNumber, episode.episodeNumber) &&
                              !isPartOfSeasonPack(season.seasonNumber)
                            "
                          >
                            <Badge
                              variant="secondary"
                              class="h-7 flex items-center gap-1 text-xs px-2"
                            >
                              <Check class="size-3" />
                              Available
                            </Badge>
                          </template>
                          <!-- A live want drives this episode: surface its
                               lifecycle state (with download progress) in place
                               of the manual flow. -->
                          <template v-else-if="getEpisodeWant(episode.episodeId)">
                            <div class="flex flex-col items-end gap-1.5">
                              <div class="flex items-center gap-2">
                                <WantStatusPill
                                  :status="getEpisodeWant(episode.episodeId)!.status"
                                  :attempt-count="getEpisodeWant(episode.episodeId)!.attemptCount"
                                  :last-error="getEpisodeWant(episode.episodeId)!.lastError"
                                  :progress="
                                    getEpisodeWantProgress(
                                      season.seasonNumber,
                                      episode.episodeNumber,
                                    )
                                  "
                                  :hold="getEpisodeWant(episode.episodeId)!.hold"
                                />
                                <!-- A held want is never auto-searched; offer the same
                                     manual Download flow as an untracked episode so the
                                     user can fulfill it themselves. -->
                                <Button
                                  v-if="getEpisodeWant(episode.episodeId)!.hold === 'needs_pick'"
                                  size="sm"
                                  variant="outline"
                                  class="h-7 text-xs"
                                  @click="
                                    searchForEpisodeCandidates(
                                      season.seasonNumber,
                                      episode.episodeNumber,
                                    )
                                  "
                                >
                                  <Download class="size-3 mr-1.5" />
                                  Download
                                </Button>
                              </div>
                              <!-- Proposed: a release was picked and parked for
                                   confirmation. Approve grabs it; Decline excludes it
                                   and re-searches. Both act on the one proposal.id, so
                                   acting from any episode a pack covers resolves it. -->
                              <div
                                v-if="
                                  getEpisodeWant(episode.episodeId)!.hold === 'proposed' &&
                                  getEpisodeProposal(episode.episodeId)
                                "
                                class="flex flex-col items-end gap-1"
                              >
                                <p
                                  class="max-w-[16rem] truncate text-xs text-muted-foreground"
                                  :title="getEpisodeProposal(episode.episodeId)!.candidateTitle"
                                >
                                  {{ getEpisodeProposal(episode.episodeId)!.candidateTitle }}
                                </p>
                                <p class="text-[11px] text-muted-foreground">
                                  {{ formatBytes(getEpisodeProposal(episode.episodeId)!.size) }} ·
                                  {{ getEpisodeProposal(episode.episodeId)!.seeders }} seeders
                                  <span v-if="getEpisodeProposal(episode.episodeId)!.isPack">
                                    · covers
                                    {{
                                      getEpisodeProposal(episode.episodeId)!.coveredEpisodeIds
                                        ?.length ?? 0
                                    }}
                                    episodes
                                  </span>
                                </p>
                                <div class="flex items-center gap-1.5">
                                  <Button
                                    size="sm"
                                    class="h-7 text-xs"
                                    :disabled="proposalPending"
                                    @click="
                                      approveProposal.mutate({
                                        path: { id: getEpisodeProposal(episode.episodeId)!.id },
                                      })
                                    "
                                  >
                                    <Sparkles class="size-3 mr-1.5" />
                                    Approve
                                  </Button>
                                  <Button
                                    size="sm"
                                    variant="outline"
                                    class="h-7 text-xs"
                                    :disabled="proposalPending"
                                    @click="
                                      declineProposal.mutate({
                                        path: { id: getEpisodeProposal(episode.episodeId)!.id },
                                      })
                                    "
                                  >
                                    <X class="size-3 mr-1.5" />
                                    Decline
                                  </Button>
                                </div>
                              </div>
                            </div>
                          </template>
                          <!-- Episode is downloading individually -->
                          <template
                            v-else-if="getEpisodeJob(season.seasonNumber, episode.episodeNumber)"
                          >
                            <CircularProgress
                              :state="
                                getEpisodeProgressState(season.seasonNumber, episode.episodeNumber)
                              "
                              :value="
                                getEpisodeProgressValue(season.seasonNumber, episode.episodeNumber)
                              "
                              size="sm"
                            />
                          </template>
                          <!-- Episode is part of an active season pack download -->
                          <template v-else-if="isPartOfSeasonPack(season.seasonNumber)">
                            <TooltipProvider>
                              <Tooltip>
                                <TooltipTrigger as-child>
                                  <span class="flex items-center">
                                    <CircularProgress state="indeterminate" size="sm" />
                                  </span>
                                </TooltipTrigger>
                                <TooltipContent> Downloading as season pack </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </template>
                          <!-- Not available: show Snag button -->
                          <template v-else>
                            <Button
                              size="sm"
                              variant="outline"
                              class="h-7 text-xs"
                              @click="
                                searchForEpisodeCandidates(
                                  season.seasonNumber,
                                  episode.episodeNumber,
                                )
                              "
                            >
                              <Download class="size-3 mr-1.5" />
                              Download
                            </Button>
                          </template>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </TabsContent>
            </Tabs>
          </div>

          <RailCast v-if="data.credits?.cast?.length" title="Cast" :cast="data.credits.cast" />
          <RailVideos v-if="data.videos?.length" title="Videos" :videos="data.videos" />

          <WatchProviders :providers="data.watchProviders" />
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { toast } from 'vue-sonner'
import { Download, Check, Sparkles, X } from 'lucide-vue-next'
import {
  mediaGetSeriesOptions,
  trackingByTmdbOptions,
  trackingByTmdbQueryKey,
  trackingProposalsOptions,
  trackingProposalsQueryKey,
  proposalsApproveMutation,
  proposalsDeclineMutation,
} from '@/client/@tanstack/vue-query.gen'
import type { SeasonDetail, Want, ProposalView } from '@/client/types.gen'
import { formatBytes } from '@/lib/format'
import { problemMessage } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { ScrollArea, ScrollBar } from '@/components/ui/scroll-area'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import CircularProgress from '@/components/ui/progress/CircularProgress.vue'
import type { CircularProgressState } from '@/components/ui/progress/CircularProgress.vue'
import MediaHero from '@/components/media/MediaHero.vue'
import MediaHeroSkeleton from '@/components/media/MediaHeroSkeleton.vue'
import RatingBadge from '@/components/media/RatingBadge.vue'
import Poster from '@/components/poster/Poster.vue'
import RailCast from '@/components/rails/RailCast.vue'
import RailVideos from '@/components/rails/RailVideos.vue'
import WatchProviders from '@/components/media/WatchProviders.vue'
import NextEpisodeBanner from '@/components/media/NextEpisodeBanner.vue'
import { useModal } from '@/composables/useModal'
import { buildMetadataSubtitle } from '@/lib/utils'
import { statusLabel } from '@/lib/mediaStatus'
import { useDownloadJobs, type DownloadJob } from '@/composables/useDownloadJobs'
import DownloadCandidatesDialog from '@/components/download-candidates/DownloadCandidatesDialog.vue'
import SeriesAcquisitionControl from '@/components/acquisition/SeriesAcquisitionControl.vue'
import WantStatusPill from '@/components/acquisition/WantStatusPill.vue'

const route = useRoute()
const isImmersive = computed(() => route.meta.layout === 'immersive')
const modal = useModal()
const { jobsById } = useDownloadJobs()

const selectedSeason = ref<string>('')

const id = computed(() => {
  const castAttept = Number(Array.isArray(route.params.id) ? route.params.id[0] : route.params.id)
  if (isNaN(castAttept)) {
    throw new Error('Invalid series ID')
  }

  return castAttept
})

const { isLoading, isError, data } = useQuery(
  computed(() => mediaGetSeriesOptions({ path: { id: id.value } })),
)

// Per-episode acquisition state. A 404 means untracked (no wants), read as an
// empty map below — don't burn retries on it. The generic want_updated SSE
// binding invalidates every trackingByTmdb query, keeping this live.
const { data: tracking } = useQuery(
  computed(() => ({
    ...trackingByTmdbOptions({ path: { tmdbId: id.value }, query: { type: 'series' } }),
    retry: false,
  })),
)

// Wants keyed by episodeId — the join the grid needs, since wants reference
// episodeId while the episode rows key on (season, episode) numbers.
const wantByEpisode = computed(() => {
  const map = new Map<string, Want>()
  for (const w of tracking.value?.wants ?? []) {
    if (w.episodeId) map.set(w.episodeId, w)
  }
  return map
})

// The want that should drive an episode's action area: a live one mid-flight or
// failed. 'available' shows via the file indicator; 'canceled' falls through to
// the manual Download so the episode can be re-grabbed.
function getEpisodeWant(episodeId?: string): Want | undefined {
  if (!episodeId) return undefined
  const want = wantByEpisode.value.get(episodeId)
  if (!want || want.status === 'available' || want.status === 'canceled') return undefined
  return want
}

const queryClient = useQueryClient()
const trackingId = computed(() => tracking.value?.tracking?.id)

// Open proposals for this tracking — the picks parked for one-tap approval when a
// segment's autonomy is 'propose'. Dependent on the resolved tracking id; the
// proposal_updated SSE binding invalidates this key to keep it live.
const { data: proposals } = useQuery(
  computed(() => ({
    ...trackingProposalsOptions({ path: { id: trackingId.value! } }),
    enabled: !!trackingId.value,
  })),
)

// Proposals keyed by episodeId. A pack proposal covers several episodes, so each
// of its coveredEpisodeIds maps to the same proposal object — Approve/Decline from
// any covered episode acts on that one proposal.id.
const proposalByEpisode = computed(() => {
  const map = new Map<string, ProposalView>()
  for (const p of proposals.value ?? []) {
    for (const epId of p.coveredEpisodeIds ?? []) map.set(epId, p)
  }
  return map
})

function getEpisodeProposal(episodeId?: string): ProposalView | undefined {
  if (!episodeId) return undefined
  return proposalByEpisode.value.get(episodeId)
}

function invalidateProposalState() {
  queryClient.invalidateQueries({
    queryKey: trackingProposalsQueryKey({ path: { id: trackingId.value! } }),
  })
  queryClient.invalidateQueries({
    queryKey: trackingByTmdbQueryKey({ path: { tmdbId: id.value }, query: { type: 'series' } }),
  })
}

const approveProposal = useMutation({
  ...proposalsApproveMutation(),
  onSuccess: () => {
    toast.success('Approved — grabbing now')
    invalidateProposalState()
  },
  onError: (err) => toast.error(problemMessage(err, 'Failed to approve')),
})

const declineProposal = useMutation({
  ...proposalsDeclineMutation(),
  onSuccess: () => {
    toast.success('Declined — searching for a different release')
    invalidateProposalState()
  },
  onError: (err) => toast.error(problemMessage(err, 'Failed to decline')),
})

const proposalPending = computed(
  () => approveProposal.isPending.value || declineProposal.isPending.value,
)

// Download progress (0-100) for a want-driven episode, from its live job.
function getEpisodeWantProgress(seasonNumber: number, episodeNumber: number): number | null {
  const job = getEpisodeJob(seasonNumber, episodeNumber)
  if (!job) return null
  return Math.round((job.progress ?? 0) * 100)
}

const availableEpisodeCount = computed(
  () =>
    data.value?.seasons?.reduce(
      (sum, s) => sum + (s.episodes?.filter((e) => e.available).length ?? 0),
      0,
    ) ?? 0,
)
const totalEpisodeCount = computed(
  () => data.value?.seasons?.reduce((sum, s) => sum + (s.episodes?.length ?? 0), 0) ?? 0,
)

// Aired episodes are the back-catalog the tracking would backfill. Counting them
// (air date on-or-before now, specials excluded — scope presets never select
// season 0) lets the track dialog show the stakes and skip the scope question
// for an upcoming series with nothing aired.
const airedEpisodeCount = computed(() => {
  const now = Date.now()
  return (
    data.value?.seasons?.reduce(
      (sum, s) =>
        s.seasonNumber >= 1
          ? sum +
            (s.episodes?.filter((e) => e.airDate && new Date(e.airDate).getTime() <= now).length ??
              0)
          : sum,
      0,
    ) ?? 0
  )
})

// Regular seasons only, for the scope card's "N episodes · M seasons" line.
const seasonCount = computed(
  () => data.value?.seasons?.filter((s) => s.seasonNumber >= 1).length ?? 0,
)

// A series still has new episodes to come unless it's definitively over. Fails
// open on 'unknown' (unmapped provider status) — wrongly hiding the "new
// episodes" choice locks the user out, wrongly showing it is harmless.
const hasOngoing = computed(
  () => data.value?.status !== 'ended' && data.value?.status !== 'canceled',
)

const firstAirYear = computed(() =>
  data.value?.firstAirDate ? new Date(data.value.firstAirDate).getFullYear().toString() : '',
)
const lastAirYear = computed(() =>
  data.value?.lastAirDate ? new Date(data.value.lastAirDate).getFullYear().toString() : '',
)
const seriesSubTitle = computed(() => {
  if (!data.value) return ''
  const first = firstAirYear.value
  const last = lastAirYear.value
  let yearDisplay: string | undefined
  if (first && last && first !== last) {
    yearDisplay = `${first} - ${last}`
  } else if (first) {
    yearDisplay = first
  }
  return buildMetadataSubtitle({
    mediaType: 'series',
    year: yearDisplay,
    certification: data.value.certification,
    runtime: data.value.episodeRuntime,
  })
})

const backdropUrl = computed(() =>
  data.value?.backdropPath
    ? `https://image.tmdb.org/t/p/w1280/${data.value.backdropPath}`
    : undefined,
)

const creatorCredits = computed(() => {
  const creators = data.value?.credits?.crew?.filter(
    (c) => c.job === 'Creator' || c.department === 'Creator',
  )
  if (!creators?.length) return undefined
  return `Created by ${creators.map((c) => c.name).join(', ')}`
})

const seriesChips = computed(() => {
  const chips: string[] = []
  if (data.value?.genres?.length) {
    chips.push(...data.value.genres.slice(0, 3).map((g) => g.name))
  }
  const l = statusLabel(data.value?.status)
  if (l) chips.push(l)
  return chips
})

const sortedSeasons = computed(() => {
  if (!data.value?.seasons) return []
  return [...data.value.seasons].sort((a, b) => b.seasonNumber - a.seasonNumber)
})

// Default-select the latest season when data loads
watch(
  sortedSeasons,
  (seasons) => {
    const first = seasons[0]
    if (first && !selectedSeason.value) {
      selectedSeason.value = String(first.seasonNumber)
    }
  },
  { immediate: true },
)

const currentSeason = computed(() =>
  sortedSeasons.value.find((s) => String(s.seasonNumber) === selectedSeason.value),
)

// Get all active download jobs for this series
const activeJobsForSeries = computed(() => {
  if (!data.value?.tmdbId) return []
  return Object.values(jobsById.value).filter(
    (job) => job.mediaType === 'series' && job.tmdbId === data.value?.tmdbId && isJobActive(job),
  )
})

// Check if a job is considered "active" (not in a terminal state)
function isJobActive(job: DownloadJob): boolean {
  // Active download states
  const activeDownloadStates = ['created', 'enqueued', 'downloading']
  if (activeDownloadStates.includes(job.status)) return true
  // Active import states (download completed but still importing)
  const activeImportStates = ['awaiting_import', 'importing']
  if (activeImportStates.includes(job.importStatus)) return true
  return false
}

// Get season pack job (if any) for a season - season packs have no episode_id
function getSeasonPackJob(seasonNumber: number): DownloadJob | undefined {
  return activeJobsForSeries.value.find(
    (job) => job.seasonNumber === seasonNumber && !job.episodeId,
  )
}

// Get episode job (if any) for a specific episode
function getEpisodeJob(seasonNumber: number, episodeNumber: number): DownloadJob | undefined {
  return activeJobsForSeries.value.find(
    (job) => job.seasonNumber === seasonNumber && job.episodeNumber === episodeNumber,
  )
}

// Check if season has any individual episode downloads active
function hasActiveEpisodeDownloads(season: SeasonDetail): boolean {
  return (
    season.episodes?.some((ep) => getEpisodeJob(season.seasonNumber, ep.episodeNumber)) ?? false
  )
}

// Get count of active episode downloads for a season
function getActiveEpisodeCount(season: SeasonDetail): number {
  return (
    season.episodes?.filter((ep) => getEpisodeJob(season.seasonNumber, ep.episodeNumber)).length ??
    0
  )
}

// Check if an episode is part of an active season pack download
function isPartOfSeasonPack(seasonNumber: number): boolean {
  return !!getSeasonPackJob(seasonNumber)
}

// Get progress state for season pack
function getSeasonProgressState(seasonNumber: number): CircularProgressState {
  const job = getSeasonPackJob(seasonNumber)
  if (!job) return 'indeterminate'

  // Downloading phase
  if (['created', 'enqueued', 'downloading'].includes(job.status)) {
    return (job.progress ?? 0) > 0 ? 'progress' : 'indeterminate'
  }
  // Import phase
  if (['awaiting_import', 'importing'].includes(job.importStatus)) {
    return 'indeterminate'
  }
  return 'indeterminate'
}

// Get progress value for season pack (0-100)
function getSeasonProgressValue(seasonNumber: number): number {
  const job = getSeasonPackJob(seasonNumber)
  if (!job) return 0
  return Math.round((job.progress ?? 0) * 100)
}

// Get progress state for individual episode
function getEpisodeProgressState(
  seasonNumber: number,
  episodeNumber: number,
): CircularProgressState {
  const job = getEpisodeJob(seasonNumber, episodeNumber)
  if (!job) return 'indeterminate'

  // Downloading phase
  if (['created', 'enqueued', 'downloading'].includes(job.status)) {
    return (job.progress ?? 0) > 0 ? 'progress' : 'indeterminate'
  }
  // Import phase
  if (['awaiting_import', 'importing'].includes(job.importStatus)) {
    return 'indeterminate'
  }
  return 'indeterminate'
}

// Get progress value for individual episode (0-100)
function getEpisodeProgressValue(seasonNumber: number, episodeNumber: number): number {
  const job = getEpisodeJob(seasonNumber, episodeNumber)
  if (!job) return 0
  return Math.round((job.progress ?? 0) * 100)
}

const isDownloading = computed(() => {
  // Check if any active downloads exist for this series
  if (activeJobsForSeries.value.length > 0) return true
  // Fallback: check file status from API response
  return (
    data.value?.seasons?.some((s) => s.episodes?.some((e) => e.file?.status === 'downloading')) ??
    false
  )
})

const searchForSeasonCandidates = (seasonNumber: number) => {
  modal.open(DownloadCandidatesDialog, {
    props: {
      class: 'max-w-[90vw] sm:max-w-4xl lg:max-w-6xl',
      seriesId: id.value,
      season: seasonNumber,
    },
  })
}

const searchForEpisodeCandidates = (seasonNumber: number, episodeNumber: number) => {
  modal.open(DownloadCandidatesDialog, {
    props: {
      class: 'max-w-[90vw] sm:max-w-4xl lg:max-w-6xl',
      seriesId: id.value,
      season: seasonNumber,
      episode: episodeNumber,
    },
  })
}
</script>
