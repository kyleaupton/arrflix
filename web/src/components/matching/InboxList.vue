<script setup lang="ts">
import { Inbox } from 'lucide-vue-next'
import { Skeleton } from '@/components/ui/skeleton'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationNext,
  PaginationPrevious,
  PaginationEllipsis,
} from '@/components/ui/pagination'
import InboxRow from './InboxRow.vue'
import type { InboxItem, Pagination as PaginationMeta } from '@/client/types.gen'

defineProps<{
  items: InboxItem[]
  pagination: PaginationMeta | undefined
  selectedId: string | undefined
  isLoading: boolean
  isError: boolean
  errorMessage: string
}>()

const emit = defineEmits<{
  select: [string]
  'update:page': [number]
}>()
</script>

<template>
  <div class="flex h-full flex-col">
    <div class="flex-1 overflow-y-auto pr-1">
      <div v-if="isLoading" class="space-y-2">
        <Skeleton v-for="i in 8" :key="i" class="h-16 w-full" />
      </div>

      <div v-else-if="isError" class="p-4 text-sm text-destructive">{{ errorMessage }}</div>

      <Empty v-else-if="items.length === 0">
        <EmptyHeader>
          <EmptyMedia variant="icon"><Inbox /></EmptyMedia>
          <EmptyTitle>Inbox is clear</EmptyTitle>
          <EmptyDescription>Nothing here needs review right now.</EmptyDescription>
        </EmptyHeader>
      </Empty>

      <div v-else class="space-y-1">
        <InboxRow
          v-for="item in items"
          :key="item.id"
          :item="item"
          :active="item.id === selectedId"
          @select="emit('select', item.id)"
        />
      </div>
    </div>

    <Pagination
      v-if="pagination && pagination.totalPages > 1"
      class="mt-3 shrink-0"
      :items-per-page="pagination.pageSize"
      :total="pagination.total"
      :page="pagination.page"
      :sibling-count="1"
      show-edges
      @update:page="(p: number) => emit('update:page', p)"
    >
      <PaginationContent v-slot="{ items: pageItems }">
        <PaginationPrevious />
        <template v-for="(pageItem, idx) in pageItems" :key="idx">
          <PaginationItem
            v-if="pageItem.type === 'page'"
            :value="pageItem.value"
            :is-active="pageItem.value === pagination!.page"
          >
            {{ pageItem.value }}
          </PaginationItem>
          <PaginationEllipsis v-else :index="idx" />
        </template>
        <PaginationNext />
      </PaginationContent>
    </Pagination>
  </div>
</template>
