<template>
  <div class="download-candidates-container flex flex-col h-full">
    <div class="flex-1 overflow-auto">
      <DownloadCandidateList
        v-if="!selectedCandidate"
        :movie-id="movieId"
        :series-id="seriesId"
        :season="season"
        :episode="episode"
        @enqueue="handlePreview"
      />
      <DownloadCandidatePreview
        v-else
        :movie-id="movieId"
        :series-id="seriesId"
        :season="season"
        :episode="episode"
        :candidate="selectedCandidate"
      />
    </div>

    <div v-if="selectedCandidate" class="flex flex-col">
      <Separator class="my-4" />

      <div class="flex justify-end gap-2">
        <Button variant="secondary" @click="handleCancel"> Cancel </Button>
        <Button
          :disabled="enqueueMovieMutation.isPending.value || enqueueSeriesMutation.isPending.value"
          @click="handleEnqueue"
        >
          {{
            enqueueMovieMutation.isPending.value || enqueueSeriesMutation.isPending.value
              ? 'Enqueuing...'
              : 'Enqueue'
          }}
        </Button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { toast } from 'vue-sonner'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import {
  downloadCandidatesDownloadMovieMutation,
  downloadCandidatesDownloadSeriesMutation,
  trackingByTmdbQueryKey,
} from '@/client/@tanstack/vue-query.gen'
import { type DownloadCandidate } from '@/client/types.gen'
import DownloadCandidateList from './DownloadCandidatesList.vue'
import DownloadCandidatePreview from './DownloadCandidatePreview.vue'

const props = defineProps<{
  movieId?: number
  seriesId?: number
  season?: number
  episode?: number
}>()

const emit = defineEmits<{
  (e: 'download-enqueued'): void
}>()

const queryClient = useQueryClient()

const selectedCandidate = ref<DownloadCandidate | null>(null)

const handlePreview = (candidate: DownloadCandidate) => {
  selectedCandidate.value = candidate
}

const handleCancel = () => {
  selectedCandidate.value = null
}

// Enqueue movie mutation
const enqueueMovieMutation = useMutation({
  ...downloadCandidatesDownloadMovieMutation(),
  onSuccess: () => {
    toast.success('Download enqueued successfully')
    // A manual grab enters the want spine but emits no SSE; invalidating the
    // movie's tracking query surfaces the new want's pill on the acting client
    // immediately (movieId doubles as the tmdbId the tracking endpoint is keyed
    // on). Mirrors the requestsCreate/wantsCancel invalidation in AcquisitionControl.
    if (props.movieId) {
      queryClient.invalidateQueries({
        queryKey: trackingByTmdbQueryKey({ path: { tmdbId: props.movieId } }),
      })
    }
    emit('download-enqueued')
  },
  onError: (error) => {
    toast.error(error?.detail || error?.title || 'Failed to enqueue download candidate')
  },
})

// Enqueue series mutation
const enqueueSeriesMutation = useMutation({
  ...downloadCandidatesDownloadSeriesMutation(),
  onSuccess: () => {
    toast.success('Download enqueued successfully')
    emit('download-enqueued')
  },
  onError: (error) => {
    toast.error(error?.detail || error?.title || 'Failed to enqueue download candidate')
  },
})

const handleEnqueue = () => {
  if (!selectedCandidate.value) return

  if (props.movieId) {
    enqueueMovieMutation.mutate({
      path: { id: props.movieId },
      body: {
        indexerId: selectedCandidate.value.indexerId,
        guid: selectedCandidate.value.guid,
      },
    })
  } else if (props.seriesId) {
    enqueueSeriesMutation.mutate({
      path: { id: props.seriesId },
      body: {
        indexerId: selectedCandidate.value.indexerId,
        guid: selectedCandidate.value.guid,
        season: props.season,
        episode: props.episode,
      },
    })
  }
}
</script>

<style scoped>
.download-candidates-container {
  min-height: 500px;
}
</style>

<style scoped></style>
