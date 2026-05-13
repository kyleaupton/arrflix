<template>
  <div class="flex flex-col gap-6">
    <Transition name="fade" mode="out-in">
      <div v-if="isLoading" key="loading" class="space-y-4">
        <Skeleton class="h-96 w-full rounded-lg" />
      </div>
      <div
        v-else-if="isError"
        key="error"
        class="flex flex-col items-center justify-center py-12 text-center"
      >
        <p class="text-destructive">Failed to load person</p>
        <p class="text-sm text-muted-foreground mt-2">Please try again later</p>
      </div>
      <div v-else-if="data" key="content" class="flex flex-col gap-6">
        <MediaHero
          class="mb-1"
          :title="data.name"
          :subtitle="personSubtitle"
          :overview="data.biography"
          :chips="personChips"
          :full-bleed="isImmersive"
        >
          <template #poster>
            <div
              class="relative w-64 aspect-[2/3] rounded-lg overflow-hidden bg-muted flex-shrink-0"
            >
              <img
                v-if="profileImageUrl"
                :src="profileImageUrl"
                :alt="data.name"
                class="w-full h-full object-cover"
                loading="eager"
              />
              <div
                v-else
                class="w-full h-full flex items-center justify-center text-muted-foreground"
              >
                <User class="size-24" />
              </div>
            </div>
          </template>
        </MediaHero>

        <div :class="isImmersive ? 'px-6' : ''">
          <div v-if="hasAdditionalInfo" class="space-y-4">
            <div v-if="data.alsoKnownAs?.length" class="space-y-2">
              <h2 class="text-lg font-semibold">Also Known As</h2>
              <div class="flex flex-wrap gap-2">
                <Badge v-for="(name, index) in data.alsoKnownAs" :key="index" variant="secondary">
                  {{ name }}
                </Badge>
              </div>
            </div>

            <div v-if="hasExternalLinks" class="space-y-2">
              <h2 class="text-lg font-semibold">External Links</h2>
              <div class="flex flex-wrap gap-2">
                <Button
                  v-if="data.homepage"
                  variant="outline"
                  size="sm"
                  @click="openLink(data.homepage)"
                >
                  <ExternalLink class="mr-2 size-4" />
                  Homepage
                </Button>
                <Button
                  v-if="data.imdbId"
                  variant="outline"
                  size="sm"
                  @click="openLink(`https://www.imdb.com/name/${data.imdbId}`)"
                >
                  <ExternalLink class="mr-2 size-4" />
                  IMDb
                </Button>
              </div>
            </div>
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
import { User, ExternalLink } from 'lucide-vue-next'
import { mediaGetPersonOptions } from '@/client/@tanstack/vue-query.gen'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import MediaHero from '@/components/media/MediaHero.vue'

const route = useRoute()
const isImmersive = computed(() => route.meta.layout === 'immersive')

const id = computed(() => {
  const attempt = Number(Array.isArray(route.params.id) ? route.params.id[0] : route.params.id)
  if (isNaN(attempt)) {
    throw new Error('Invalid person ID')
  }
  return attempt
})

const { isLoading, isError, data } = useQuery(
  computed(() => mediaGetPersonOptions({ path: { id: id.value } })),
)

const profileImageUrl = computed(() => {
  if (!data.value?.profilePath) return undefined
  return `https://image.tmdb.org/t/p/w500/${data.value.profilePath}`
})

const formatDate = (dateStr: string | undefined): string => {
  if (!dateStr) return ''
  try {
    const date = new Date(dateStr)
    return date.toLocaleDateString('en-US', { year: 'numeric', month: 'long', day: 'numeric' })
  } catch {
    return dateStr
  }
}

const personSubtitle = computed(() => {
  if (!data.value) return ''
  const birthday = data.value.birthday ? formatDate(data.value.birthday) : null
  const deathday = data.value.deathday ? formatDate(data.value.deathday) : null

  if (birthday && deathday) {
    return `Born: ${birthday} • Died: ${deathday}`
  } else if (birthday) {
    return `Born: ${birthday}`
  }
  return ''
})

const personChips = computed(() => {
  const chips: string[] = []
  if (data.value?.knownForDepartment) {
    chips.push(data.value.knownForDepartment)
  }
  if (data.value?.placeOfBirth) {
    chips.push(data.value.placeOfBirth)
  }
  return chips
})

const hasAdditionalInfo = computed(() => {
  return (
    (data.value?.alsoKnownAs && data.value.alsoKnownAs.length > 0) ||
    data.value?.homepage ||
    data.value?.imdbId
  )
})

const hasExternalLinks = computed(() => {
  return !!(data.value?.homepage || data.value?.imdbId)
})

const openLink = (url: string) => {
  window.open(url, '_blank')
}
</script>

<style scoped></style>
