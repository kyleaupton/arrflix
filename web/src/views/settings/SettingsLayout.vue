<template>
  <div class="flex flex-col gap-4">
    <Tabs v-model="activeTab">
      <TabsList>
        <TabsTrigger value="general">General</TabsTrigger>
        <TabsTrigger value="routing">Routing</TabsTrigger>
        <TabsTrigger value="libraries">Libraries</TabsTrigger>
        <TabsTrigger value="indexers">Indexers</TabsTrigger>
        <TabsTrigger value="downloaders">Downloaders</TabsTrigger>
        <TabsTrigger value="name-templates">Name Templates</TabsTrigger>
      </TabsList>
    </Tabs>

    <RouterView />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

const route = useRoute()
const router = useRouter()

const activeTab = computed({
  get: () => {
    return route.path.split('/').pop()
  },
  set: (value) => {
    router.push(`/settings/${value}`)
  },
})
</script>
