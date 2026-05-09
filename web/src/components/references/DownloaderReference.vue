<template>
  <div class="inline-flex items-center">
    <div v-if="isLoading" class="inline-flex items-center gap-1 text-muted-foreground">
      <Loader2 class="size-4 animate-spin" />
      <span class="text-sm">Loading...</span>
    </div>
    <div v-else-if="error" class="inline-flex items-center gap-1 text-destructive">
      <AlertTriangle class="size-4" />
      <span class="text-sm">Error</span>
    </div>
    <div v-else-if="downloader" class="inline-flex items-center gap-2">
      <span class="font-semibold">{{ downloader.name }}</span>
      <Badge variant="secondary">{{ downloader.type }}</Badge>
      <Badge variant="outline">{{ downloader.protocol }}</Badge>
      <Badge v-if="downloader.default" variant="default">Default</Badge>
    </div>
    <span v-else class="text-muted-foreground text-sm">Unknown</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { Loader2, AlertTriangle } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { downloadersGetOptions } from '@/client/@tanstack/vue-query.gen'

const props = defineProps<{
  downloaderId: string
}>()

const {
  data: downloader,
  isLoading,
  error,
} = useQuery(
  computed(() =>
    downloadersGetOptions({
      path: { id: props.downloaderId },
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any),
  ),
)
</script>
