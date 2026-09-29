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
                :tmdb-id="id"
                :tracking="tracking?.tracking ?? null"
                :ongoing="hasOngoing"
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

                      <!-- Season-level action. A season already moving says so and
                       offers nothing; manual search is an operator action on a
                       season that is neither complete nor in flight. -->
                      <div class="flex shrink-0 justify-end">
                        <TitleStatusPill v-if="seasonActive(season)" state="downloading" />
                        <Button
                          v-else-if="!seasonComplete(season) && auth.canManageJobs"
                          size="sm"
                          variant="outline"
                          @click="searchForSeasonCandidates(season.seasonNumber)"
                        >
                          <Download class="mr-2 size-4" />
                          Get
                        </Button>
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

                            <!-- Status/action cell. The state is the server's
                             answer; the only choice made here is whether an
                             operator is additionally offered a hand-grab. -->
                            <div class="flex min-w-[7rem] shrink-0 items-center justify-end gap-2">
                              <TitleStatusPill
                                v-if="episodeState(episode.episodeId)"
                                :state="episodeState(episode.episodeId)"
                              />
                              <Button
                                v-if="
                                  auth.canManageJobs && canHandGrab(episodeState(episode.episodeId))
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
import { Download, ChevronRight } from 'lucide-vue-next'
import { mediaGetSeriesOptions, trackingByTmdbOptions } from '@/client/@tanstack/vue-query.gen'
import type { SeasonDetail } from '@/client/types.gen'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Progress } from '@/components/ui/progress'
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
import { useTitleStatus } from '@/composables/useTitleStatus'
import DownloadCandidatesDialog from '@/components/download-candidates/DownloadCandidatesDialog.vue'
import SeriesAcquisitionControl from '@/components/acquisition/SeriesAcquisitionControl.vue'
import SeriesStatusCard from '@/components/acquisition/SeriesStatusCard.vue'
import AttentionCard from '@/components/acquisition/AttentionCard.vue'
import TitleStatusPill from '@/components/acquisition/TitleStatusPill.vue'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const isImmersive = computed(() => route.meta.layout === 'immersive')
const modal = useModal()
const auth = useAuthStore()

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

// Automation configuration — what happens to episodes that don't exist yet.
// That is a setting rather than a state, so no amount of current acquisition
// data answers it and it stays on its own query.
const { data: tracking } = useQuery(
  computed(() => trackingByTmdbOptions({ path: { tmdbId: id.value }, query: { type: 'series' } })),
)

// The acquisition read for this series: the headline state, the counts, and one
// derived state per episode. The season grid and the title chip come out of the
// same call, which is what stops them disagreeing.
const { status: titleStatus } = useTitleStatus('series', id)

// Episode states keyed by episode id. Deprecated episodes are absent — the
// projection drops them, since an episode pulled upstream is neither acquirable
// nor something a grid should account for.
const episodeStateById = computed(() => {
  const map = new Map<string, string>()
  for (const e of titleStatus.value?.episodes ?? []) map.set(e.episodeId, e.state)
  return map
})

function episodeState(episodeId?: string): string | null {
  if (!episodeId) return null
  return episodeStateById.value.get(episodeId) ?? null
}

// Only in-scope episodes count toward the title's totals: a series carries every
// episode for its grid, including specials nobody asked for, and those must not
// hold the headline back. The projection has already applied that split.
const availableEpisodeCount = computed(() => titleStatus.value?.counts.available ?? 0)
const totalEpisodeCount = computed(() => titleStatus.value?.counts.total ?? 0)

// Operators may hand-grab anything not already on disk and not already moving.
function canHandGrab(state: string | null): boolean {
  return state !== 'available' && state !== 'downloading' && state !== 'importing'
}

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

// Season rollups read the projection rather than the detail payload's file flag,
// so the bar, the "Missing N" line, and the per-episode pills are three views of
// one answer instead of three reads of two sources.
function seasonAvailable(season: SeasonDetail): number {
  return season.episodes?.filter((e) => episodeState(e.episodeId) === 'available').length ?? 0
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

// A season is moving when any of its episodes is. Read off the projection, so a
// season pack (one job, many episodes) and per-episode grabs look the same here —
// the distinction is a transfer detail the grid has no reason to model.
function seasonActive(season: SeasonDetail): boolean {
  return (
    season.episodes?.some((e) => {
      const st = episodeState(e.episodeId)
      return st === 'downloading' || st === 'importing'
    }) ?? false
  )
}

const isDownloading = computed(() => titleStatus.value?.phase === 'downloading')

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
