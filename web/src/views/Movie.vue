<template>
  <div class="flex flex-col gap-6">
    <div v-if="isLoading" class="space-y-4">
      <Skeleton class="h-96 w-full rounded-lg" />
    </div>
    <div v-else-if="isError" class="flex flex-col items-center justify-center py-12 text-center">
      <p class="text-destructive">Failed to load movie</p>
      <p class="text-sm text-muted-foreground mt-2">Please try again later</p>
    </div>
    <template v-else-if="data">
      <MediaHero
        class="mb-1"
        :title="data.title"
        :tagline="data.tagline"
        :subtitle="movieSubtitle"
        :credits="directorCredits"
        :overview="data.overview"
        :backdrop-url="backdropUrl"
        :chips="movieChips"
        :trailer-url="trailerUrl"
      >
        <template #poster>
          <Poster :item="data" size="large" :clickable="false" :is-downloading="isDownloading" />
        </template>
        <template #actions>
          <Button @click="searchForDownloadCandidates">
            <Download class="mr-2 size-4" />
              Download
          </Button>
        </template>
      </MediaHero>

      <div v-if="data.files?.length" class="space-y-4">
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

      <RailCast v-if="data.credits?.cast?.length" title="Cast" :cast="data.credits.cast" />
      <RailVideos v-if="data.videos?.length" title="Videos" :videos="data.videos" />
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

      <WatchProviders :providers="data.watchProviders" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { Download, File } from 'lucide-vue-next'
import { getV1MovieByIdOptions } from '@/client/@tanstack/vue-query.gen'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import MediaHero from '@/components/media/MediaHero.vue'
import Poster from '@/components/poster/Poster.vue'
import RailCast from '@/components/rails/RailCast.vue'
import RailVideos from '@/components/rails/RailVideos.vue'
import RailMovie from '@/components/rails/RailMovie.vue'
import WatchProviders from '@/components/media/WatchProviders.vue'
import DataTable from '@/components/tables/DataTable.vue'
import { movieFilesColumns } from '@/components/tables/configs/movieFilesTableConfig'
import { useModal } from '@/composables/useModal'
import { buildMetadataSubtitle } from '@/lib/utils'
import { useDownloadJobsStore } from '@/stores/downloadJobs'
import DownloadCandidatesDialog from '@/components/download-candidates/DownloadCandidatesDialog.vue'
import type { ModelFileInfo } from '@/client/types.gen'

const route = useRoute()
const modal = useModal()
const downloadJobs = useDownloadJobsStore()

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
  computed(() => getV1MovieByIdOptions({ path: { id: id.value } })),
)

const movieSubtitle = computed(() => {
  if (!data.value) return ''
  const year = data.value.releaseDate
    ? new Date(data.value.releaseDate).getFullYear()
    : undefined
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

  return data.value.files.map((file): ModelFileInfo => {
    // If file has downloadJobId, get latest progress from store
    if (file.downloadJobId) {
      const job = downloadJobs.getJobById(file.downloadJobId)
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
    const job = downloadJobs.getJobById(file.downloadJobId)
    return job ? downloadJobs.isJobActive(job) : false
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

// Connect to live updates on mount
onMounted(() => {
  downloadJobs.connectLive()
})

const searchForDownloadCandidates = () => {
  modal.open(DownloadCandidatesDialog, {
    props: {
      class: 'max-w-[90vw] sm:max-w-4xl lg:max-w-6xl',
      movieId: id.value,
    },
  })
}
</script>

<style scoped></style>
