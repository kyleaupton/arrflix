<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import { unmatchedFilesListOptions } from '@/client/@tanstack/vue-query.gen'
import { problemMessage } from '@/lib/api'
import { useInboxCount } from '@/composables/useInboxCount'
import OutcomeFilter from '@/components/matching/OutcomeFilter.vue'
import InboxList from '@/components/matching/InboxList.vue'
import DecidePane from '@/components/matching/DecidePane.vue'
import { INBOX_OUTCOMES, type InboxOutcome } from '@/components/matching/outcome'

const PAGE_SIZE = 25

const route = useRoute()
const router = useRouter()

// State lives in the URL (?outcome=&page=&file=) so the back button, refresh,
// and deep links all restore the exact triage position.
const VALID = new Set<InboxOutcome>(INBOX_OUTCOMES)

const outcome = computed<InboxOutcome | undefined>(() => {
  const v = route.query.outcome
  return typeof v === 'string' && VALID.has(v as InboxOutcome) ? (v as InboxOutcome) : undefined
})
const page = computed(() => {
  const n = Number(route.query.page)
  return Number.isInteger(n) && n > 0 ? n : 1
})
const selectedFileId = computed(() =>
  typeof route.query.file === 'string' ? route.query.file : undefined,
)

function patchQuery(patch: Record<string, string | number | undefined>) {
  const next: LocationQueryRaw = {}
  for (const [k, v] of Object.entries(route.query)) {
    if (typeof v === 'string') next[k] = v
  }
  for (const [k, v] of Object.entries(patch)) {
    if (v === undefined || v === '') delete next[k]
    else next[k] = String(v)
  }
  router.push({ query: next })
}

// Changing the band resets paging and clears the open file (it may not be in the
// new band). Page 1 and no-file are the defaults, so we drop them from the URL.
function setOutcome(v: InboxOutcome | undefined) {
  patchQuery({ outcome: v, page: undefined, file: undefined })
}
function setPage(p: number) {
  patchQuery({ page: p === 1 ? undefined : p })
}
function selectFile(id: string) {
  patchQuery({ file: id })
}

// After a decision the file leaves the inbox; advance to the next item (or the
// previous one if it was last) so triage flows without a manual re-select. Read
// from the current page synchronously — invalidation refetches right after.
function onDecided() {
  const list = items.value
  const idx = list.findIndex((i) => i.id === selectedFileId.value)
  const next = idx >= 0 ? (list[idx + 1] ?? list[idx - 1]) : undefined
  patchQuery({ file: next?.id })
}

// Unfiltered band breakdown for the filter tabs + header total; shared with the
// nav badge via the same query key.
const { count, counts } = useInboxCount()

// The paged/filtered row list.
const listQuery = useQuery(
  computed(() =>
    unmatchedFilesListOptions({
      query: {
        page: page.value,
        pageSize: PAGE_SIZE,
        ...(outcome.value ? { outcome: outcome.value } : {}),
      },
    }),
  ),
)

const items = computed(() => listQuery.data.value?.data ?? [])
const pagination = computed(() => listQuery.data.value?.pagination)
const listError = computed(() => problemMessage(listQuery.error.value, 'Failed to load inbox'))
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <h1 class="text-xl font-semibold">Matching</h1>
      <p class="text-sm text-muted-foreground">
        Review and identify files the matcher couldn't confidently match.
      </p>
    </div>

    <OutcomeFilter
      :model-value="outcome"
      :counts="counts"
      :total="count"
      @update:model-value="setOutcome"
    />

    <div class="grid gap-4 md:grid-cols-[minmax(320px,420px)_1fr]">
      <div class="h-[calc(100vh-16rem)] min-h-[24rem]">
        <InboxList
          :items="items"
          :pagination="pagination"
          :selected-id="selectedFileId"
          :is-loading="listQuery.isLoading.value"
          :is-error="listQuery.isError.value"
          :error-message="listError"
          @select="selectFile"
          @update:page="setPage"
        />
      </div>

      <div
        class="h-[calc(100vh-16rem)] min-h-[24rem] overflow-y-auto rounded-lg border bg-card p-4"
      >
        <DecidePane :file-id="selectedFileId" @decided="onDecided" />
      </div>
    </div>
  </div>
</template>
