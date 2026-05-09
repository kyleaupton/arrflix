<template>
  <div class="name-template-reference">
    <div v-if="isLoading" class="inline-flex items-center gap-1 text-muted-color">
      <i class="pi pi-spin pi-spinner text-sm"></i>
      <span class="text-sm">Loading...</span>
    </div>
    <div v-else-if="error" class="inline-flex items-center gap-1 text-red-400">
      <i class="pi pi-exclamation-triangle text-sm"></i>
      <span class="text-sm">Error</span>
    </div>
    <div v-else-if="nameTemplate" class="inline-flex items-center gap-2">
      <span class="font-semibold">{{ nameTemplate.name }}</span>
      <Badge
        :value="
          nameTemplate.type === 'movie' ? 'Movie' : nameTemplate.type === 'series' ? 'Series' : nameTemplate.type
        "
        severity="secondary"
        size="small"
      />
      <Badge v-if="nameTemplate.default" value="Default" severity="info" size="small" />
    </div>
    <span v-else class="text-muted-color text-sm">Unknown</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import Badge from 'primevue/badge'
import { nameTemplatesGetOptions } from '@/client/@tanstack/vue-query.gen'

const props = defineProps<{
  nameTemplateId: string
}>()

const {
  data: nameTemplate,
  isLoading,
  error,
} = useQuery(
  computed(() =>
    nameTemplatesGetOptions({
      path: { id: props.nameTemplateId },
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any),
  ),
)
</script>

<style scoped>
.name-template-reference {
  display: inline-flex;
  align-items: center;
}
</style>

