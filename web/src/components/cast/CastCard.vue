<template>
  <router-link
    :to="{ path: `/person/${member.tmdbId}` }"
    class="cast-card group flex flex-col items-center gap-3 w-28 sm:w-32"
  >
    <div
      class="relative size-24 sm:size-28 rounded-full overflow-hidden bg-muted ring-2 ring-transparent transition-all duration-300 group-hover:ring-primary/50 group-hover:shadow-lg"
    >
      <img
        v-if="profileImageUrl"
        :src="profileImageUrl"
        :alt="member.name"
        class="w-full h-full object-cover object-[center_20%] transition-transform duration-300 group-hover:scale-110"
        loading="lazy"
      />
      <div v-else class="w-full h-full flex items-center justify-center text-muted-foreground">
        <User class="size-10" />
      </div>
    </div>
    <div class="text-center w-full">
      <p class="font-medium text-sm truncate">{{ member.name }}</p>
      <p class="text-xs text-muted-foreground truncate">{{ member.character }}</p>
    </div>
  </router-link>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { User } from 'lucide-vue-next'
import { type ModelCastMember } from '@/client/types.gen'

const props = defineProps<{
  member: ModelCastMember
}>()

const profileImageUrl = computed(() => {
  if (!props.member.profilePath) return undefined
  return `https://image.tmdb.org/t/p/w185/${props.member.profilePath}`
})
</script>
