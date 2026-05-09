<template>
  <TooltipProvider>
    <Tooltip>
      <TooltipTrigger as-child>
        <img
          :src="logoUrl"
          :alt="provider.providerName"
          class="size-10 rounded-lg shadow-sm hover:scale-105 transition-transform cursor-pointer"
        />
      </TooltipTrigger>
      <TooltipContent>
        {{ provider.providerName }}
      </TooltipContent>
    </Tooltip>
  </TooltipProvider>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { WatchProvider } from '@/client/types.gen'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'

const props = defineProps<{
  provider: WatchProvider
}>()

const logoUrl = computed(() => {
  if (!props.provider.logoPath) return ''
  return `https://image.tmdb.org/t/p/w92${props.provider.logoPath}`
})
</script>
