<script setup lang="ts">
import { ref, computed } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { EpisodeRefDto } from '@/client/types.gen'

const props = defineProps<{ isSeries: boolean; disabled?: boolean }>()
const emit = defineEmits<{
  match: [
    {
      source: 'tmdb' | 'imdb' | 'tvdb'
      externalId: string
      episode?: EpisodeRefDto
      edition?: string
    },
  ]
}>()

const source = ref('tmdb')
const externalId = ref('')
const season = ref('')
const episode = ref('')
const edition = ref('')

const canSubmit = computed(() => externalId.value.trim().length > 0)

function submit() {
  if (!canSubmit.value) return
  const ep =
    props.isSeries && season.value && episode.value
      ? { season: Number(season.value), episode: Number(episode.value) }
      : undefined
  emit('match', {
    source: source.value as 'tmdb' | 'imdb' | 'tvdb',
    externalId: externalId.value.trim(),
    episode: ep,
    edition: edition.value.trim() || undefined,
  })
}
</script>

<template>
  <div class="space-y-3">
    <div class="grid grid-cols-[110px_1fr] gap-2">
      <div class="space-y-1">
        <Label class="text-xs">Source</Label>
        <Select v-model="source">
          <SelectTrigger><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="tmdb">TMDB</SelectItem>
            <SelectItem value="imdb">IMDb</SelectItem>
            <SelectItem value="tvdb">TVDB</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div class="space-y-1">
        <Label class="text-xs">ID</Label>
        <Input v-model="externalId" placeholder="e.g. 333371 or tt1179933" />
      </div>
    </div>

    <div v-if="isSeries" class="grid grid-cols-2 gap-2">
      <div class="space-y-1">
        <Label class="text-xs">Season</Label>
        <Input v-model="season" type="number" min="0" />
      </div>
      <div class="space-y-1">
        <Label class="text-xs">Episode</Label>
        <Input v-model="episode" type="number" min="0" />
      </div>
    </div>
    <div v-else class="space-y-1">
      <Label class="text-xs">Edition (optional)</Label>
      <Input v-model="edition" placeholder="e.g. directors_cut" />
    </div>

    <Button size="sm" class="w-full" :disabled="!canSubmit || disabled" @click="submit">
      Match by ID
    </Button>
  </div>
</template>
