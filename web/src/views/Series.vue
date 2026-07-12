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
            <Poster
              :item="data"
              size="large"
              responsive
              :clickable="false"
              :is-downloading="isDownloading"
            />
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

        <div :class="isImmersive ? 'px-6' : ''">
          <div class="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px] lg:gap-8">
            <div class="flex min-w-0 flex-col gap-6">
              <SeriesStatusCard
                :tracking="tracking?.tracking ?? null"
                :my-request="tracking?.myRequest ?? null"
                :ongoing="hasOngoing"
                :available-count="availableEpisodeCount"
                :total-count="totalEpisodeCount"
                :aired-total-count="airedEpisodeCount"
                :active-download-count="activeJobsForSeries.length"
                :episodes-downloading="episodesDownloading"
              />
              <AttentionCard v-if="auth.canManageJobs" :tmdb-id="id" type="series" />
              <NextEpisodeBanner v-if="data.nextEpisodeToAir" :episode="data.nextEpisodeToAir" />
              <div v-if="data.seasons?.length" class="space-y-4">
                <h2 class="text-xl font-semibold">Seasons</h2>

                <div class="divide-y overflow-hidden rounded-lg border">
                  <Collapsible
                    v-for="season in sortedSeasons"
                    :key="season.seasonNumber"
                    :open="isSeasonOpen(season.seasonNumber)"
                    @update:open="(o: boolean) => setSeasonOpen(season.seasonNumber, o)"
                  >
                    <!-- Collapsed header. The left region (chevron → availability) is
                     the toggle; the season action sits outside it so downloading
                     doesn't also expand the row. -->
                    <div class="flex items-center gap-3 p-3">
                      <CollapsibleTrigger class="flex min-w-0 flex-1 items-center gap-3 text-left">
                        <ChevronRight
                          class="size-4 shrink-0 text-muted-foreground transition-transform"
                          :class="{ 'rotate-90': isSeasonOpen(season.seasonNumber) }"
                        />
                        <img
                          v-if="season.posterPath"
                          :src="`https://image.tmdb.org/t/p/w185${season.posterPath}`"
                          :alt="`Season ${season.seasonNumber} poster`"
                          class="h-14 w-10 shrink-0 rounded bg-muted object-cover"
                        />
                        <div v-else class="h-14 w-10 shrink-0 rounded bg-muted" />

                        <div class="min-w-0 flex-1">
                          <div class="flex items-center gap-2">
                            <span class="truncate font-medium">
                              {{
                                season.seasonNumber === 0
                                  ? 'Specials'
                                  : `Season ${season.seasonNumber}`
                              }}
                            </span>
                            <span
                              v-if="seasonYear(season)"
                              class="shrink-0 text-sm text-muted-foreground"
                            >
                              · {{ seasonYear(season) }}
                            </span>
                          </div>
                          <p class="mt-0.5 truncate text-xs text-muted-foreground">
                            {{ seasonStatus(season) }}
                          </p>
                        </div>

                        <div class="hidden w-40 shrink-0 items-center gap-2 sm:flex">
                          <Progress :model-value="seasonPct(season)" class="h-1.5 flex-1" />
                          <span class="w-12 text-right text-xs tabular-nums text-muted-foreground">
                            {{ seasonAvailable(season) }}/{{ seasonTotal(season) }}
                          </span>
                        </div>
                      </CollapsibleTrigger>

                      <!-- Season-level action: pack progress / episode-download hint /
                       manual download. Absent once the season is complete. -->
                      <div class="flex shrink-0 justify-end">
                        <template v-if="getSeasonPackJob(season.seasonNumber)">
                          <CircularProgress
                            :state="getSeasonProgressState(season.seasonNumber)"
                            :value="getSeasonProgressValue(season.seasonNumber)"
                            size="sm"
                          />
                        </template>
                        <template v-else-if="hasActiveEpisodeDownloads(season)">
                          <TooltipProvider>
                            <Tooltip>
                              <TooltipTrigger as-child>
                                <span class="flex items-center">
                                  <CircularProgress state="indeterminate" size="sm" />
                                </span>
                              </TooltipTrigger>
                              <TooltipContent>
                                {{ getActiveEpisodeCount(season) }} episode(s) downloading
                              </TooltipContent>
                            </Tooltip>
                          </TooltipProvider>
                        </template>
                        <!-- Manual season search is an operator action; requesters
                         see only the progress states above. -->
                        <template v-else-if="!seasonComplete(season) && auth.canManageJobs">
                          <Button
                            size="sm"
                            variant="outline"
                            @click="searchForSeasonCandidates(season.seasonNumber)"
                          >
                            <Download class="mr-2 size-4" />
                            Get
                          </Button>
                        </template>
                      </div>
                    </div>

                    <!-- Expanded: compact episode list rows. -->
                    <CollapsibleContent>
                      <div class="border-t bg-muted/20">
                        <p v-if="season.overview" class="px-3 pt-3 text-sm text-muted-foreground">
                          {{ season.overview }}
                        </p>
                        <ul class="divide-y">
                          <li
                            v-for="episode in season.episodes"
                            :key="episode.episodeNumber"
                            class="flex items-center gap-3 px-3 py-2"
                          >
                            <span class="w-8 shrink-0 font-mono text-xs text-muted-foreground">
                              E{{ episode.episodeNumber.toString().padStart(2, '0') }}
                            </span>
                            <p class="min-w-0 flex-1 truncate text-sm">
                              {{ episode.title || 'Episode ' + episode.episodeNumber }}
                            </p>
                            <span
                              v-if="episode.airDate"
                              class="hidden w-20 shrink-0 text-right text-xs text-muted-foreground sm:block"
                            >
                              {{ formatShortDate(episode.airDate) }}
                            </span>

                            <!-- Status/action cell — one of five states. -->
                            <div class="flex min-w-[7rem] shrink-0 justify-end">
                              <!-- Available and idle. -->
                              <template
                                v-if="
                                  episode.available &&
                                  !getEpisodeJob(season.seasonNumber, episode.episodeNumber) &&
                                  !isPartOfSeasonPack(season.seasonNumber)
                                "
                              >
                                <Badge
                                  variant="secondary"
                                  class="flex h-7 items-center gap-1 px-2 text-xs"
                                >
                                  <Check class="size-3" />
                                  Available
                                </Badge>
                              </template>
                              <!-- A live want drives this episode: surface its
                               lifecycle state (with download progress). A held
                               want offers the same manual Download as an untracked
                               episode so the user can fulfill it. -->
                              <template v-else-if="getEpisodeWant(episode.episodeId)">
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
                                  <Button
                                    v-if="
                                      getEpisodeWant(episode.episodeId)!.hold === 'needs_pick' &&
                                      auth.canManageJobs
                                    "
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
                                    <Download class="mr-1.5 size-3" />
                                    Download
                                  </Button>
                                </div>
                              </template>
                              <!-- Downloading individually. -->
                              <template
                                v-else-if="
                                  getEpisodeJob(season.seasonNumber, episode.episodeNumber)
                                "
                              >
                                <CircularProgress
                                  :state="
                                    getEpisodeProgressState(
                                      season.seasonNumber,
                                      episode.episodeNumber,
                                    )
                                  "
                                  :value="
                                    getEpisodeProgressValue(
                                      season.seasonNumber,
                                      episode.episodeNumber,
                                    )
                                  "
                                  size="sm"
                                />
                              </template>
                              <!-- Part of an active season-pack download. -->
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
                              <!-- Not available: operators get a manual download;
                               requesters see an empty cell (no manual action). -->
                              <template v-else-if="auth.canManageJobs">
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
                                  <Download class="mr-1.5 size-3" />
                                  Download
                                </Button>
                              </template>
                            </div>
                          </li>
                        </ul>
                      </div>
                    </CollapsibleContent>
                  </Collapsible>
                </div>
              </div>
            </div>

            <DetailsRail
              :facts="seriesFacts"
              :watch-providers="data.watchProviders"
              :automation="automationSummary"
            />
          </div>

          <div class="mt-10 flex flex-col gap-10">
            <RailCast v-if="data.credits?.cast?.length" title="Cast" :cast="data.credits.cast" />
            <RailVideos v-if="data.videos?.length" title="Videos" :videos="data.videos" />
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { Download, Check, ChevronRight } from 'lucide-vue-next'
import { mediaGetSeriesOptions, trackingByTmdbOptions } from '@/client/@tanstack/vue-query.gen'
import type { SeasonDetail, Want } from '@/client/types.gen'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Progress } from '@/components/ui/progress'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import CircularProgress from '@/components/ui/progress/CircularProgress.vue'
import type { CircularProgressState } from '@/components/ui/progress/CircularProgress.vue'
import MediaHero from '@/components/media/MediaHero.vue'
import MediaHeroSkeleton from '@/components/media/MediaHeroSkeleton.vue'
import RatingBadge from '@/components/media/RatingBadge.vue'
import Poster from '@/components/poster/Poster.vue'
import RailCast from '@/components/rails/RailCast.vue'
import RailVideos from '@/components/rails/RailVideos.vue'
import DetailsRail, { type Fact } from '@/components/media/DetailsRail.vue'
import NextEpisodeBanner from '@/components/media/NextEpisodeBanner.vue'
import { useModal } from '@/composables/useModal'
import { buildMetadataSubtitle, formatRuntime } from '@/lib/utils'
import { statusLabel } from '@/lib/mediaStatus'
import { useDownloadJobs, isJobActive, type DownloadJob } from '@/composables/useDownloadJobs'
import DownloadCandidatesDialog from '@/components/download-candidates/DownloadCandidatesDialog.vue'
import SeriesAcquisitionControl from '@/components/acquisition/SeriesAcquisitionControl.vue'
import SeriesStatusCard from '@/components/acquisition/SeriesStatusCard.vue'
import AttentionCard from '@/components/acquisition/AttentionCard.vue'
import WantStatusPill from '@/components/acquisition/WantStatusPill.vue'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const isImmersive = computed(() => route.meta.layout === 'immersive')
const modal = useModal()
const auth = useAuthStore()
const { jobsById } = useDownloadJobs()

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

// Per-episode acquisition state. Untracked is a normal 200 with no wants, read as
// an empty map below. The want_updated SSE binding patches this query's cache in
// place (matched by trackingId), so a fulfilling series stays live without refetch.
const { data: tracking } = useQuery(
  computed(() => trackingByTmdbOptions({ path: { tmdbId: id.value }, query: { type: 'series' } })),
)

// Wants keyed by episodeId — the join the rows need, since wants reference
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

// Episodes in flight, counted from the wants rather than the download jobs: one
// grabbed season pack is many episodes, and a requester thinks in episodes, not
// jobs. Want-derived, so it's requester-safe and stays live via want_updated.
// 'grabbed' is the handed-to-downloader state; import ('imported') reads as
// available-imminent, so it's excluded from the "downloading" count.
const episodesDownloading = computed(() => {
  let n = 0
  for (const w of wantByEpisode.value.values()) {
    if (w.status === 'grabbed') n++
  }
  return n
})

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

// Right-rail facts, derived from the detail payload. Empty values are dropped so
// the rail only shows what this series actually exposes.
const seriesFacts = computed<Fact[]>(() => {
  if (!data.value) return []
  const facts: Fact[] = []
  const status = statusLabel(data.value.status)
  if (status) facts.push({ label: 'Status', value: status })
  if (data.value.firstAirDate)
    facts.push({ label: 'First aired', value: formatFullDate(data.value.firstAirDate) })
  if (data.value.lastAirDate && data.value.lastAirDate !== data.value.firstAirDate)
    facts.push({ label: 'Last aired', value: formatFullDate(data.value.lastAirDate) })
  const runtime = formatRuntime(data.value.episodeRuntime)
  if (runtime) facts.push({ label: 'Episode', value: runtime })
  if (data.value.genres?.length)
    facts.push({
      label: data.value.genres.length > 1 ? 'Genres' : 'Genre',
      value: data.value.genres.map((g) => g.name).join(', '),
    })
  return facts
})

// A tracked series' current per-segment autonomy, shown read-only in the rail;
// the kebab's Automation dialog is where it's changed. Undefined when untracked
// so the rail omits the section.
const AUTONOMY_LABELS: Record<string, string> = {
  auto: 'Automatic',
  propose: 'Suggested',
  manual: 'Manual',
}
const automationSummary = computed(() => {
  const t = tracking.value?.tracking
  if (!t) return undefined
  return {
    backfill: AUTONOMY_LABELS[t.autonomyBackfill] ?? t.autonomyBackfill,
    ongoing: AUTONOMY_LABELS[t.autonomyOngoing] ?? t.autonomyOngoing,
  }
})

const sortedSeasons = computed(() => {
  if (!data.value?.seasons) return []
  return [...data.value.seasons].sort((a, b) => b.seasonNumber - a.seasonNumber)
})

// Season of the next episode still to air, if any — the season the page should
// open to and label as "Airing".
const airingSeasonNumber = computed(() => data.value?.nextEpisodeToAir?.seasonNumber ?? null)

// Which seasons are expanded. Multiple may be open at once; reassigned (not
// mutated) so the ref reacts.
const openSeasons = ref<Set<number>>(new Set())
const hasSeededOpen = ref(false)

function isSeasonOpen(seasonNumber: number): boolean {
  return openSeasons.value.has(seasonNumber)
}

function setSeasonOpen(seasonNumber: number, open: boolean) {
  const next = new Set(openSeasons.value)
  if (open) next.add(seasonNumber)
  else next.delete(seasonNumber)
  openSeasons.value = next
}

// Auto-expand once when seasons first load: the airing season if the payload
// names one and it's present, else the most recent.
watch(
  sortedSeasons,
  (seasons) => {
    const first = seasons[0]
    if (hasSeededOpen.value || !first) return
    const airing = airingSeasonNumber.value
    const target =
      airing != null && seasons.some((s) => s.seasonNumber === airing) ? airing : first.seasonNumber
    openSeasons.value = new Set([target])
    hasSeededOpen.value = true
  },
  { immediate: true },
)

function seasonAvailable(season: SeasonDetail): number {
  return season.episodes?.filter((e) => e.available).length ?? 0
}

function seasonTotal(season: SeasonDetail): number {
  return season.episodes?.length ?? 0
}

function seasonPct(season: SeasonDetail): number {
  const total = seasonTotal(season)
  return total ? Math.round((seasonAvailable(season) / total) * 100) : 0
}

// Complete means every episode (aired or not) is on disk — the state that hides
// the "Get" action.
function seasonComplete(season: SeasonDetail): boolean {
  const total = seasonTotal(season)
  return total > 0 && seasonAvailable(season) === total
}

function seasonYear(season: SeasonDetail): string {
  const d = season.airDate ?? season.episodes?.find((e) => e.airDate)?.airDate
  if (!d) return ''
  const year = new Date(d + 'T00:00:00').getFullYear()
  return isNaN(year) ? '' : String(year)
}

function seasonStatus(season: SeasonDetail): string {
  const total = seasonTotal(season)
  if (total === 0) return ''
  if (airingSeasonNumber.value === season.seasonNumber) {
    const next = data.value?.nextEpisodeToAir?.airDate
    return next ? `Airing — next ep ${formatShortDate(next)}` : 'Airing'
  }
  const available = seasonAvailable(season)
  if (available === total) return 'Complete'
  return `Missing ${total - available}`
}

function formatShortDate(airDate?: string): string {
  if (!airDate) return ''
  const d = new Date(airDate + 'T00:00:00')
  if (isNaN(d.getTime())) return airDate
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

function formatFullDate(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso.includes('T') ? iso : iso + 'T00:00:00')
  if (isNaN(d.getTime())) return iso
  return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' })
}

// Get all active download jobs for this series
const activeJobsForSeries = computed(() => {
  if (!data.value?.tmdbId) return []
  return Object.values(jobsById.value).filter(
    (job) => job.mediaType === 'series' && job.tmdbId === data.value?.tmdbId && isJobActive(job),
  )
})

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
