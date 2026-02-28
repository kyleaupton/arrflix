<script setup lang="ts">
import { ref, inject, computed, watch } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { getV1FilesystemBrowseOptions } from '@/client/@tanstack/vue-query.gen'
import BaseDialog from './BaseDialog.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Folder, Loader2, AlertCircle, ChevronUp } from 'lucide-vue-next'

interface Props {
  initialPath?: string
}

const props = withDefaults(defineProps<Props>(), {
  initialPath: '/',
})

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }

const currentPath = ref(props.initialPath || '/')
const manualPath = ref(currentPath.value)

const { data, isLoading, isError, error } = useQuery(
  computed(() =>
    getV1FilesystemBrowseOptions({
      query: { path: currentPath.value },
    }),
  ),
)

// Sync manual path input when navigating
watch(currentPath, (val) => {
  manualPath.value = val
})

const pathSegments = computed(() => {
  const p = data.value?.current_path || currentPath.value
  if (p === '/') return [{ name: '/', path: '/' }]
  const parts = p.split('/').filter(Boolean)
  return [
    { name: '/', path: '/' },
    ...parts.map((part, i) => ({
      name: part,
      path: '/' + parts.slice(0, i + 1).join('/'),
    })),
  ]
})

const navigateTo = (path: string) => {
  currentPath.value = path
}

const handleGo = () => {
  const trimmed = manualPath.value.trim()
  if (trimmed) {
    currentPath.value = trimmed
  }
}

const handleSelect = () => {
  dialogRef.value.close({ selectedPath: data.value?.current_path || currentPath.value })
}

const handleCancel = () => {
  dialogRef.value.close()
}
</script>

<template>
  <BaseDialog title="Browse Directory">
    <div class="flex flex-col gap-3 h-[400px]">
      <!-- Manual path input -->
      <div class="flex gap-2 shrink-0">
        <Input
          v-model="manualPath"
          placeholder="/"
          class="font-mono text-sm"
          @keydown.enter="handleGo"
        />
        <Button variant="outline" size="sm" @click="handleGo">Go</Button>
      </div>

      <!-- Breadcrumb -->
      <Breadcrumb class="shrink-0">
        <BreadcrumbList>
          <template v-for="(segment, i) in pathSegments" :key="segment.path">
            <BreadcrumbSeparator v-if="i > 0" />
            <BreadcrumbItem>
              <BreadcrumbLink
                v-if="i < pathSegments.length - 1"
                class="cursor-pointer"
                @click="navigateTo(segment.path)"
              >
                {{ segment.name }}
              </BreadcrumbLink>
              <BreadcrumbPage v-else>{{ segment.name }}</BreadcrumbPage>
            </BreadcrumbItem>
          </template>
        </BreadcrumbList>
      </Breadcrumb>

      <!-- Directory listing -->
      <ScrollArea class="flex-1 min-h-0 rounded-md border">
        <div v-if="isLoading" class="flex items-center justify-center py-8 text-muted-foreground">
          <Loader2 class="h-5 w-5 animate-spin mr-2" />
          Loading...
        </div>

        <div v-else-if="isError" class="flex items-center gap-2 py-8 px-4 text-destructive text-sm">
          <AlertCircle class="h-5 w-5 shrink-0" />
          {{ (error as Error)?.message || 'Failed to browse directory' }}
        </div>

        <div v-else class="flex flex-col">
          <!-- Up directory -->
          <button
            v-if="data?.parent"
            class="flex items-center gap-2 px-3 py-2 text-sm hover:bg-accent text-left text-muted-foreground"
            @click="navigateTo(data.parent!)"
          >
            <ChevronUp class="h-4 w-4" />
            ..
          </button>

          <!-- Directory entries -->
          <button
            v-for="dir in data?.directories"
            :key="dir.path"
            class="flex items-center gap-2 px-3 py-2 text-sm hover:bg-accent text-left"
            @click="navigateTo(dir.path!)"
          >
            <Folder class="h-4 w-4 text-muted-foreground shrink-0" />
            {{ dir.name }}
          </button>

          <!-- Empty state -->
          <div
            v-if="data?.directories?.length === 0"
            class="py-8 text-center text-sm text-muted-foreground"
          >
            No subdirectories
          </div>
        </div>
      </ScrollArea>
    </div>

    <template #footer>
      <Button variant="outline" @click="handleCancel">Cancel</Button>
      <Button @click="handleSelect">Select</Button>
    </template>
  </BaseDialog>
</template>
