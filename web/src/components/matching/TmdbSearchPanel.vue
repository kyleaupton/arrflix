<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { Loader2, Film, Tv, CheckCircle2 } from 'lucide-vue-next'
import { Input } from '@/components/ui/input'
import { useSearch } from '@/composables/useSearch'
import type { SearchResult } from '@/client/types.gen'

const props = defineProps<{ initialQuery?: string; disabled?: boolean }>()
const emit = defineEmits<{ match: [{ source: 'tmdb'; externalId: string; type?: string }] }>()

const { query, results, isLoading } = useSearch()

// People aren't matchable identities for a media file.
const matchable = computed(() => results.value.filter((r) => r.mediaType !== 'person'))

onMounted(() => {
  if (props.initialQuery) query.value = props.initialQuery
})

const posterUrl = (path?: string) => (path ? `https://image.tmdb.org/t/p/w92${path}` : '')

function pick(r: SearchResult) {
  emit('match', { source: 'tmdb', externalId: String(r.id), type: r.mediaType })
}
</script>

<template>
  <div class="space-y-2">
    <Input v-model="query" placeholder="Search TMDB…" />

    <div v-if="isLoading" class="flex items-center gap-2 text-xs text-muted-foreground">
      <Loader2 class="size-3 animate-spin" /> Searching…
    </div>

    <div
      v-else-if="query.length >= 2 && matchable.length === 0"
      class="text-xs text-muted-foreground"
    >
      No results.
    </div>

    <div v-else class="space-y-1">
      <button
        v-for="r in matchable"
        :key="r.mediaType + r.id"
        type="button"
        class="flex w-full items-center gap-2 rounded-md border p-2 text-left hover:bg-muted/60 disabled:opacity-50"
        :disabled="disabled"
        @click="pick(r)"
      >
        <div class="h-12 w-8 shrink-0 overflow-hidden rounded bg-muted">
          <img
            v-if="r.posterPath"
            :src="posterUrl(r.posterPath)"
            :alt="r.title"
            class="h-full w-full object-cover"
          />
          <div v-else class="flex h-full w-full items-center justify-center">
            <component
              :is="r.mediaType === 'tv' ? Tv : Film"
              class="size-3.5 text-muted-foreground"
            />
          </div>
        </div>
        <div class="min-w-0 flex-1">
          <span class="block truncate text-sm font-medium">{{ r.title }}</span>
          <span class="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span v-if="r.year">{{ r.year }}</span>
            <span>· {{ r.mediaType === 'tv' ? 'Series' : 'Movie' }}</span>
            <span v-if="r.isInLibrary" class="flex items-center gap-0.5 text-emerald-500">
              <CheckCircle2 class="size-3" /> in library
            </span>
          </span>
        </div>
      </button>
    </div>
  </div>
</template>
