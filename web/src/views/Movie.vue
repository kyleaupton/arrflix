<template>
  <div class="flex flex-col gap-6">
    <Transition name="fade" mode="out-in">
      <MediaHeroSkeleton v-if="isLoading" key="loading" />
      <div
        v-else-if="isError"
        key="error"
        class="flex flex-col items-center justify-center py-12 text-center"
      >
        <p class="text-destructive">Failed to load movie</p>
        <p class="text-sm text-muted-foreground mt-2">Please try again later</p>
      </div>
      <div v-else-if="data" key="content" class="flex flex-col gap-6">
        <MediaHero
          :title="data.title"
          :tagline="data.tagline"
          :subtitle="movieSubtitle"
          :credits="directorCredits"
          :overview="data.overview"
          :backdrop-url="backdropUrl"
          :chips="movieChips"
          :trailer-url="trailerUrl"
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
            <AcquisitionControl :tmdb-id="id" />
          </template>
        </MediaHero>

        <div :class="isImmersive ? 'px-6' : ''">
          <div class="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px] lg:gap-8">
            <div class="flex min-w-0 flex-col gap-10">
              <MovieStatusCard :movie="data" />

              <AttentionCard v-if="auth.canManageJobs" :tmdb-id="id" type="movie" />

              <div v-if="data.files?.length" class="bg-card rounded-lg border p-4 sm:p-6 space-y-4">
                <h2 class="text-xl font-semibold">Local Files</h2>
                <DataTable
                  :data="filesWithProgress"
                  :columns="movieFilesColumns"
                  :loading="false"
                  empty-message="No files found"
                  :searchable="false"
                  search-placeholder="Search files..."
                  paginator
                  :rows="10"
                >
                  <template #empty-icon>
                    <File class="size-5" />
                  </template>
                </DataTable>
              </div>
            </div>

            <DetailsRail :facts="movieFacts" :watch-providers="data.watchProviders" />
          </div>

          <div class="mt-10 flex flex-col gap-10">
            <RailVideos v-if="nonTrailerVideos.length" title="Videos" :videos="nonTrailerVideos" />

            <RailCast v-if="data.credits?.cast?.length" title="Cast" :cast="data.credits.cast" />

            <RailMovie
              v-if="data.recommendations?.length"
              :rail="{
                id: 'related-movies',
                title: 'Related Movies',
                type: 'movie',
                movies: data.recommendations,
                series: [],
              }"
            />
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

import { useQuery } from '@tanstack/vue-query'
import { File } from 'lucide-vue-next'
import { mediaGetMovieOptions } from '@/client/@tanstack/vue-query.gen'
import MediaHero from '@/components/media/MediaHero.vue'
import MediaHeroSkeleton from '@/components/media/MediaHeroSkeleton.vue'
import RatingBadge from '@/components/media/RatingBadge.vue'
import Poster from '@/components/poster/Poster.vue'
import RailCast from '@/components/rails/RailCast.vue'
import RailVideos from '@/components/rails/RailVideos.vue'
import RailMovie from '@/components/rails/RailMovie.vue'
import DetailsRail, { type Fact } from '@/components/media/DetailsRail.vue'
import DataTable from '@/components/tables/DataTable.vue'
import { movieFilesColumns } from '@/components/tables/configs/movieFilesTableConfig'
import { buildMetadataSubtitle, formatRuntime } from '@/lib/utils'
import { statusLabel } from '@/lib/mediaStatus'
import { useDownloadJobs, isJobActive } from '@/composables/useDownloadJobs'
import AcquisitionControl from '@/components/acquisition/AcquisitionControl.vue'
import AttentionCard from '@/components/acquisition/AttentionCard.vue'
import MovieStatusCard from '@/components/acquisition/MovieStatusCard.vue'
import { useAuthStore } from '@/stores/auth'
import type { FileInfo } from '@/client/types.gen'

const route = useRoute()
const isImmersive = computed(() => route.meta.layout === 'immersive')
const auth = useAuthStore()
const { getJobById } = useDownloadJobs()

const id = computed(() => {
  const castAttept = Number(Array.isArray(route.params.id) ? route.params.id[0] : route.params.id)
  if (isNaN(castAttept)) {
    throw new Error('Invalid movie ID')
  }

  return castAttept
})

const trailerUrl = computed(() => {
  const trailer = data.value?.videos?.find((v) => v.isOfficialTrailer)
  if (!trailer) return undefined

  switch (trailer.site) {
    case 'YouTube':
      return `https://www.youtube.com/watch?v=${trailer.key}`
    case 'Vimeo':
      return `https://www.vimeo.com/watch?v=${trailer.key}`
    default:
      console.warn(`Unknown trailer site: ${trailer.site}`)
      return undefined
  }
})

const { isLoading, isError, data } = useQuery(
  computed(() => mediaGetMovieOptions({ path: { id: id.value } })),
)

const nonTrailerVideos = computed(() => {
  return data.value?.videos?.filter((v) => !v.isOfficialTrailer) ?? []
})

// Right-rail facts, derived from the detail payload. Empty values are dropped so
// the rail only shows what this movie actually exposes.
const movieFacts = computed<Fact[]>(() => {
  if (!data.value) return []
  const facts: Fact[] = []
  const status = statusLabel(data.value.status)
  if (status) facts.push({ label: 'Status', value: status })
  // Per-type dates supersede the single release date: the primary date becomes
  // the theatrical row, and digital/physical get their own rows when present.
  const rd = data.value.releaseDates
  const theatrical = rd?.theatrical ?? data.value.releaseDate
  if (theatrical)
    facts.push({ label: rd ? 'Theatrical' : 'Released', value: formatDate(theatrical) })
  if (rd?.digital) facts.push({ label: 'Digital', value: formatDate(rd.digital) })
  if (rd?.physical) facts.push({ label: 'Physical', value: formatDate(rd.physical) })
  const runtime = formatRuntime(data.value.runtime)
  if (runtime) facts.push({ label: 'Runtime', value: runtime })
  if (data.value.genres?.length)
    facts.push({
      label: data.value.genres.length > 1 ? 'Genres' : 'Genre',
      value: data.value.genres.map((g) => g.name).join(', '),
    })
  return facts
})

function formatDate(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso.includes('T') ? iso : iso + 'T00:00:00')
  if (isNaN(d.getTime())) return iso
  return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' })
}

const movieSubtitle = computed(() => {
  if (!data.value) return ''
  const year = data.value.releaseDate ? new Date(data.value.releaseDate).getFullYear() : undefined
  return buildMetadataSubtitle({
    mediaType: 'movie',
    year,
    certification: data.value.certification,
    runtime: data.value.runtime,
  })
})

const backdropUrl = computed(() =>
  data.value?.backdropPath
    ? `https://image.tmdb.org/t/p/w1280/${data.value.backdropPath}`
    : undefined,
)

const directorCredits = computed(() => {
  const directors = data.value?.credits?.crew?.filter((c) => c.job === 'Director')
  if (!directors?.length) return undefined
  return `Directed by ${directors.map((d) => d.name).join(', ')}`
})

const movieChips = computed(() => {
  const chips: string[] = []
  if (data.value?.genres?.length) {
    chips.push(...data.value.genres.slice(0, 4).map((g) => g.name))
  }
  return chips
})

// Merge API files with real-time download job updates
const filesWithProgress = computed(() => {
  if (!data.value?.files) return []

  return data.value.files.map((file): FileInfo => {
    // If file has downloadJobId, get latest progress from the live jobs cache
    if (file.downloadJobId) {
      const job = getJobById(file.downloadJobId)
      if (job) {
        return {
          ...file,
          progress: job.progress ?? file.progress,
          status: mapJobStatusToFileStatus(job.status),
        }
      }
    }
    return file
  })
})

// Check if movie has any active downloads
const isDownloading = computed(() => {
  if (!data.value?.files) return false

  return data.value.files.some((file) => {
    if (!file.downloadJobId) return false
    const job = getJobById(file.downloadJobId)
    return job ? isJobActive(job) : false
  })
})

// Map download job status to file status
function mapJobStatusToFileStatus(jobStatus: string): string {
  switch (jobStatus) {
    case 'created':
    case 'enqueued':
    case 'downloading':
      return 'downloading'
    case 'importing':
      return 'importing'
    default:
      return 'downloading' // fallback
  }
}
</script>

<style scoped></style>
