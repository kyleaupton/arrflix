<script setup lang="ts">
import { watch, ref, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { Search, ArrowRight } from 'lucide-vue-next'
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from '@/components/ui/dialog'
import Input from '@/components/ui/input/Input.vue'
import { Skeleton } from '@/components/ui/skeleton'
import SearchResultItem from './SearchResultItem.vue'
import { useSearch } from '@/composables/useSearch'

const open = defineModel<boolean>('open', { default: false })

const router = useRouter()
const { query, results, totalResults, isLoading, clear } = useSearch()

const inputRef = ref<InstanceType<typeof Input> | null>(null)

// Focus input when dialog opens
watch(open, async (isOpen) => {
  if (isOpen) {
    await nextTick()
    const el = inputRef.value?.$el as HTMLElement | undefined
    const input = el?.querySelector('input') ?? el
    input?.focus()
  } else {
    clear()
  }
})

// Close on navigation
watch(
  () => router.currentRoute.value.fullPath,
  () => {
    open.value = false
  },
)

const onKeydown = (e: KeyboardEvent) => {
  if (e.key === 'Enter' && query.value.length >= 2) {
    router.push({ path: '/search', query: { q: query.value } })
    open.value = false
  }
}

const onSelect = () => {
  open.value = false
}

const seeAll = () => {
  router.push({ path: '/search', query: { q: query.value } })
  open.value = false
}
</script>

<template>
  <Dialog v-model:open="open">
    <DialogContent class="p-0 gap-0 max-w-lg top-[20%] translate-y-0">
      <DialogTitle class="sr-only">Search</DialogTitle>

      <!-- Search input -->
      <div class="flex items-center border-b px-3">
        <Search class="size-4 shrink-0 opacity-50" />
        <Input
          ref="inputRef"
          v-model="query"
          placeholder="Search movies, series, people..."
          class="flex h-11 border-0 bg-transparent dark:bg-transparent shadow-none focus-visible:ring-0 focus-visible:ring-offset-0"
          @keydown="onKeydown"
        />
      </div>

      <!-- Results area -->
      <div class="max-h-[300px] overflow-y-auto">
        <!-- Loading -->
        <div v-if="isLoading && query.length >= 2" class="p-2 space-y-2">
          <div v-for="i in 3" :key="i" class="flex items-center gap-3 px-2 py-2">
            <Skeleton class="w-10 h-15 rounded" />
            <div class="flex-1 space-y-2">
              <Skeleton class="h-4 w-3/4" />
              <Skeleton class="h-3 w-1/2" />
            </div>
          </div>
        </div>

        <!-- Results -->
        <div v-else-if="results.length > 0" class="p-1">
          <SearchResultItem
            v-for="result in results"
            :key="`${result.mediaType}-${result.id}`"
            :result="result"
            @select="onSelect"
          />

          <button
            v-if="totalResults > results.length"
            class="flex items-center justify-center gap-2 w-full px-2 py-2 mt-1 text-sm text-muted-foreground hover:text-foreground hover:bg-accent rounded-md"
            @click="seeAll"
          >
            <span>See all {{ totalResults }} results</span>
            <ArrowRight class="h-4 w-4" />
          </button>
        </div>

        <!-- Empty -->
        <div v-else-if="query.length >= 2 && !isLoading" class="p-4 text-center text-muted-foreground">
          <p>No results found for "{{ query }}"</p>
        </div>

        <!-- Hint -->
        <div v-else class="p-4 text-center text-muted-foreground text-sm">
          <p>Search for movies, series, and people</p>
        </div>
      </div>
    </DialogContent>
  </Dialog>
</template>
