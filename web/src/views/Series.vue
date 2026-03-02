<template>
  <div class="flex flex-col gap-6">
    <div v-if="isLoading" class="space-y-4">
      <Skeleton class="h-96 w-full rounded-lg" />
    </div>
    <div v-else-if="isError" class="flex flex-col items-center justify-center py-12 text-center">
      <p class="text-destructive">Failed to load series</p>
      <p class="text-sm text-muted-foreground mt-2">Please try again later</p>
    </div>
    <template v-else-if="data">
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
          <RatingBadge
            source="tmdb"
            :score="data.voteAverage"
            :vote-count="data.voteCount"
          />
        </template>
      </MediaHero>

      <div :class="isImmersive ? 'px-6 space-y-6' : 'space-y-6'">
      <div v-if="data.seasons?.length" class="space-y-4">
        <h2 class="text-xl font-semibold">Seasons</h2>
        <div class="space-y-2">
          <Collapsible
            v-for="season in sortedSeasons"
            :key="season.seasonNumber"
            v-model:open="openSeasons[season.seasonNumber]"
            class="border rounded-lg overflow-hidden"
          >
            <CollapsibleTrigger as-child>
              <div
                class="flex items-center justify-between p-4 bg-muted/30 hover:bg-muted/50 transition-colors cursor-pointer"
              >
                <div class="flex items-center gap-4 flex-1 text-left">
                  <ChevronRight
                    :class="[
                      'size-4 transition-transform',
                      openSeasons[season.seasonNumber] ? 'rotate-90' : '',
                    ]"
                  />
                  <div class="flex items-center gap-2">
                    <h3 class="font-medium">Season {{ season.seasonNumber }}</h3>
                    <Badge
                      v-if="getSeasonStatus(season) === 'available'"
                      class="bg-green-500/90 text-white hover:bg-green-500"
                    >
                      Available
                    </Badge>
                    <Badge
                      v-else-if="getSeasonStatus(season) === 'partial'"
                      class="bg-amber-500/90 text-white hover:bg-amber-500"
                    >
                      Partial
                    </Badge>
                    <Badge
                      v-else-if="getSeasonStatus(season) === 'downloading'"
                      class="bg-blue-500/90 text-white hover:bg-blue-500"
                    >
                      Downloading
                    </Badge>
                    <Badge
                      v-else-if="getSeasonStatus(season) === 'importing'"
                      class="bg-blue-500/90 text-white hover:bg-blue-500"
                    >
                      Importing
                    </Badge>
                  </div>
                  <p v-if="season.airDate" class="text-xs text-muted-foreground ml-2">
                    {{ season.airDate }}
                  </p>
                </div>
                <div class="flex items-center gap-2">
                <!-- Season pack downloading -->
                <template v-if="getSeasonPackJob(season.seasonNumber)">
                  <CircularProgress
                    :state="getSeasonProgressState(season.seasonNumber)"
                    :value="getSeasonProgressValue(season.seasonNumber)"
                    size="sm"
                  />
                </template>
                <!-- Individual episodes downloading (no season pack) -->
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
                <!-- No active downloads: show search button -->
                <template v-else>
                  <Button
                    size="sm"
                    variant="ghost"
                    @click.stop="searchForSeasonCandidates(season.seasonNumber)"
                  >
                    <Download class="size-4 mr-2" />
                    Search
                  </Button>
                </template>
              </div>
              </div>
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div class="p-4 bg-background border-t space-y-4">
                <p v-if="season.overview" class="text-sm text-muted-foreground">
                  {{ season.overview }}
                </p>
                <div class="space-y-3">
                  <div
                    v-for="episode in season.episodes"
                    :key="episode.episodeNumber"
                    class="flex items-start gap-4 p-3 rounded-md hover:bg-muted/20 border border-transparent hover:border-border transition-all"
                  >
                    <div class="shrink-0 hidden sm:block">
                      <img
                        v-if="episode.stillPath"
                        :src="`https://image.tmdb.org/t/p/w300${episode.stillPath}`"
                        :alt="episode.title"
                        class="w-[120px] aspect-video rounded object-cover bg-muted"
                      />
                      <div v-else class="w-[120px] aspect-video rounded bg-muted" />
                    </div>
                    <div class="flex-1 min-w-0">
                      <div class="flex items-center gap-2 mb-1">
                        <span class="text-xs font-mono text-muted-foreground">
                          E{{ episode.episodeNumber.toString().padStart(2, '0') }}
                        </span>
                        <h4 class="font-medium text-sm truncate">
                          {{ episode.title || 'Episode ' + episode.episodeNumber }}
                        </h4>
                      </div>
                      <p v-if="episode.overview" class="text-xs text-muted-foreground line-clamp-2">
                        {{ episode.overview }}
                      </p>
                      <p v-if="episode.airDate" class="text-[10px] text-muted-foreground/60 mt-1">
                        Aired: {{ episode.airDate }}
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
                          class="h-8 flex items-center gap-1 text-xs px-2.5"
                        >
                          <Check class="size-3" />
                          Available
                        </Badge>
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
                          class="h-8 text-xs"
                          @click="
                            searchForEpisodeCandidates(season.seasonNumber, episode.episodeNumber)
                          "
                        >
                          <Search class="size-3 mr-1.5" />
                          Snag
                        </Button>
                      </template>
                    </div>
                  </div>
                </div>
              </div>
            </CollapsibleContent>
          </Collapsible>
        </div>
      </div>

      <RailCast v-if="data.credits?.cast?.length" title="Cast" :cast="data.credits.cast" />
      <RailVideos v-if="data.videos?.length" title="Videos" :videos="data.videos" />

      <WatchProviders :providers="data.watchProviders" />
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { Download, ChevronRight, Search, Check } from 'lucide-vue-next'
import { getV1SeriesByIdOptions } from '@/client/@tanstack/vue-query.gen'
import type { ModelSeasonDetail } from '@/client/types.gen'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import CircularProgress from '@/components/ui/progress/CircularProgress.vue'
import type { CircularProgressState } from '@/components/ui/progress/CircularProgress.vue'
import MediaHero from '@/components/media/MediaHero.vue'
import RatingBadge from '@/components/media/RatingBadge.vue'
import Poster from '@/components/poster/Poster.vue'
import RailCast from '@/components/rails/RailCast.vue'
import RailVideos from '@/components/rails/RailVideos.vue'
import WatchProviders from '@/components/media/WatchProviders.vue'
import { useModal } from '@/composables/useModal'
import { buildMetadataSubtitle } from '@/lib/utils'
import { useDownloadJobsStore, type DownloadJob } from '@/stores/downloadJobs'
import DownloadCandidatesDialog from '@/components/download-candidates/DownloadCandidatesDialog.vue'

const route = useRoute()
const isImmersive = computed(() => route.meta.layout === 'immersive')
const modal = useModal()
const downloadJobs = useDownloadJobsStore()

const openSeasons = ref<Record<number, boolean>>({})

const id = computed(() => {
  const castAttept = Number(Array.isArray(route.params.id) ? route.params.id[0] : route.params.id)
  if (isNaN(castAttept)) {
    throw new Error('Invalid series ID')
  }

  return castAttept
})

const { isLoading, isError, data } = useQuery(
  computed(() => getV1SeriesByIdOptions({ path: { id: id.value } })),
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
  if (data.value?.status) {
    chips.push(data.value.status)
  }
  return chips
})

const sortedSeasons = computed(() => {
  if (!data.value?.seasons) return []
  return [...data.value.seasons].sort((a, b) => b.seasonNumber - a.seasonNumber)
})

// Get all active download jobs for this series
const activeJobsForSeries = computed(() => {
  if (!data.value?.tmdbId) return []
  return Object.values(downloadJobs.jobsById).filter(
    (job) =>
      job.media_type === 'series' &&
      job.tmdb_id === data.value?.tmdbId &&
      isJobActive(job),
  )
})

// Check if a job is considered "active" (not in a terminal state)
function isJobActive(job: DownloadJob): boolean {
  // Active download states
  const activeDownloadStates = ['created', 'enqueued', 'downloading']
  if (activeDownloadStates.includes(job.status)) return true
  // Active import states (download completed but still importing)
  const activeImportStates = ['awaiting_import', 'importing']
  if (activeImportStates.includes(job.import_status)) return true
  return false
}

// Get season pack job (if any) for a season - season packs have no episode_id
function getSeasonPackJob(seasonNumber: number): DownloadJob | undefined {
  return activeJobsForSeries.value.find(
    (job) => job.season_number === seasonNumber && !job.episode_id,
  )
}

// Get episode job (if any) for a specific episode
function getEpisodeJob(seasonNumber: number, episodeNumber: number): DownloadJob | undefined {
  return activeJobsForSeries.value.find(
    (job) => job.season_number === seasonNumber && job.episode_number === episodeNumber,
  )
}

// Check if season has any individual episode downloads active
function hasActiveEpisodeDownloads(season: ModelSeasonDetail): boolean {
  return (
    season.episodes?.some((ep) => getEpisodeJob(season.seasonNumber, ep.episodeNumber)) ?? false
  )
}

// Get count of active episode downloads for a season
function getActiveEpisodeCount(season: ModelSeasonDetail): number {
  return (
    season.episodes?.filter((ep) => getEpisodeJob(season.seasonNumber, ep.episodeNumber)).length ??
    0
  )
}

// Check if an episode is part of an active season pack download
function isPartOfSeasonPack(seasonNumber: number): boolean {
  return !!getSeasonPackJob(seasonNumber)
}

type SeasonStatus = 'available' | 'partial' | 'downloading' | 'importing' | null

function getSeasonStatus(season: ModelSeasonDetail): SeasonStatus {
  // Check for season pack download first
  const packJob = getSeasonPackJob(season.seasonNumber)
  if (packJob) {
    if (['created', 'enqueued', 'downloading'].includes(packJob.status)) return 'downloading'
    if (['awaiting_import', 'importing'].includes(packJob.import_status)) return 'importing'
  }

  // Check for individual episode downloads
  if (hasActiveEpisodeDownloads(season)) return 'downloading'

  // Check availability
  const available = season.episodes?.filter((e) => e.available).length ?? 0
  const total = season.episodes?.length ?? 0
  if (available === total && total > 0) return 'available'
  if (available > 0) return 'partial'
  return null
}

// Get progress state for season pack
function getSeasonProgressState(seasonNumber: number): CircularProgressState {
  const job = getSeasonPackJob(seasonNumber)
  if (!job) return 'indeterminate'

  // Downloading phase
  if (['created', 'enqueued', 'downloading'].includes(job.status)) {
    return job.progress > 0 ? 'progress' : 'indeterminate'
  }
  // Import phase
  if (['awaiting_import', 'importing'].includes(job.import_status)) {
    return 'indeterminate'
  }
  return 'indeterminate'
}

// Get progress value for season pack (0-100)
function getSeasonProgressValue(seasonNumber: number): number {
  const job = getSeasonPackJob(seasonNumber)
  if (!job) return 0
  return Math.round(job.progress * 100)
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
    return job.progress > 0 ? 'progress' : 'indeterminate'
  }
  // Import phase
  if (['awaiting_import', 'importing'].includes(job.import_status)) {
    return 'indeterminate'
  }
  return 'indeterminate'
}

// Get progress value for individual episode (0-100)
function getEpisodeProgressValue(seasonNumber: number, episodeNumber: number): number {
  const job = getEpisodeJob(seasonNumber, episodeNumber)
  if (!job) return 0
  return Math.round(job.progress * 100)
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

onMounted(() => {
  downloadJobs.connectLive()
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

<style scoped></style>
