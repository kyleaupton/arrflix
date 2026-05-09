<template>
  <div class="library-reference">
    <div v-if="isLoading" class="inline-flex items-center gap-1 text-muted-foreground">
      <Loader2 class="size-4 animate-spin" />
      <span class="text-sm">Loading...</span>
    </div>
    <div v-else-if="error" class="inline-flex items-center gap-1 text-destructive">
      <AlertTriangle class="size-4" />
      <span class="text-sm">Error</span>
    </div>
    <div v-else-if="library" class="inline-flex items-center gap-2">
      <span class="font-semibold">{{ library.name }}</span>
      <Badge variant="secondary">
        {{ library.type === 'movie' ? 'Movie' : library.type === 'series' ? 'Series' : library.type }}
      </Badge>
      <Badge v-if="library.default" variant="default">Default</Badge>
    </div>
    <span v-else class="text-muted-foreground text-sm">Unknown</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { Loader2, AlertTriangle } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { librariesGetOptions } from '@/client/@tanstack/vue-query.gen'

const props = defineProps<{
  libraryId: string
}>()

const {
  data: library,
  isLoading,
  error,
} = useQuery(
  computed(() =>
    librariesGetOptions({
      path: { id: props.libraryId },
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any),
  ),
)
</script>

<style scoped>
.library-reference {
  display: inline-flex;
  align-items: center;
}
</style>
