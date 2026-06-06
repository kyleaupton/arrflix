<script setup lang="ts">
import { computed, ref } from 'vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { CheckCircle2, CircleHelp, FlaskConical, Plus, Route, XCircle } from 'lucide-vue-next'
import {
  routingListRulesOptions,
  routingListRulesQueryKey,
  routingGetFieldsOptions,
  routingReorderRulesMutation,
  routingEvaluateMutation,
  librariesListOptions,
  nameTemplatesListOptions,
  downloadersListOptions,
} from '@/client/@tanstack/vue-query.gen'
import type { Evaluation } from '@/client/types.gen'
import RoutingRuleCard from '@/components/settings/RoutingRuleCard.vue'
import { problemMessage } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'

const queryClient = useQueryClient()

// Data queries
const { data: rules, isLoading: rulesLoading } = useQuery(routingListRulesOptions())
const { data: fields, isLoading: fieldsLoading } = useQuery(routingGetFieldsOptions())
const { data: libraries } = useQuery(librariesListOptions())
const { data: nameTemplates } = useQuery(nameTemplatesListOptions())
const { data: downloaders } = useQuery(downloadersListOptions())

// New rule cards — RoutingRuleCard self-invalidates on save/delete; parent
// only tracks the "new" cards array so it can close them on save/cancel.
const newRules = ref<null[]>([])

const handleAddRule = () => {
  newRules.value.push(null)
}

const handleNewSaved = (index: number) => {
  newRules.value.splice(index, 1)
}

const removeNew = (index: number) => {
  newRules.value.splice(index, 1)
}

// Reorder — swap adjacent positions and send the full ordered id list.
const reorderError = ref<string | null>(null)
const reorderMutation = useMutation({
  ...routingReorderRulesMutation(),
  onSuccess: () => {
    reorderError.value = null
    queryClient.invalidateQueries({ queryKey: routingListRulesQueryKey() })
  },
  onError: (err) => {
    reorderError.value = problemMessage(err, 'Failed to reorder rules')
  },
})

const moveRule = (index: number, delta: number) => {
  const list = rules.value
  if (!list) return
  const target = index + delta
  if (target < 0 || target >= list.length) return
  const ids = list.map((r) => r.id)
  ;[ids[index], ids[target]] = [ids[target]!, ids[index]!]
  reorderMutation.mutate({ body: { ids } })
}

// Dry-run panel — evaluate a release title against the rules at grab.
const dryRunTitle = ref('')
const dryRunMediaType = ref<'movie' | 'series'>('movie')
const dryRunResult = ref<Evaluation | null>(null)
const dryRunError = ref<string | null>(null)

const dryRunMutation = useMutation({
  ...routingEvaluateMutation(),
  onSuccess: (data) => {
    dryRunResult.value = data
    dryRunError.value = null
  },
  onError: (err) => {
    dryRunError.value = problemMessage(err, 'Failed to evaluate rules')
    dryRunResult.value = null
  },
})

const handleDryRun = () => {
  if (!dryRunTitle.value.trim()) return
  dryRunMutation.mutate({
    body: {
      mediaType: dryRunMediaType.value,
      candidate: {
        title: dryRunTitle.value,
        filename: '',
        link: '',
        guid: '',
        indexer: '',
        indexerId: 0,
        indexerFlags: [],
        categories: [],
        protocol: 'torrent',
        publishDate: new Date().toISOString(),
        size: 0,
        seeders: 0,
        peers: 0,
        age: 0,
        ageHours: 0,
        grabs: 0,
      },
    },
  })
}

// The dispatch record names each rule; resolve action ids for display.
const resolveName = (
  id: string | undefined,
  source: { id: string; name: string }[] | undefined | null,
) => {
  if (!id) return null
  return source?.find((x) => x.id === id)?.name ?? id
}

const finalSlots = computed(() => {
  const r = dryRunResult.value
  if (!r) return []
  return [
    {
      label: 'Downloader',
      name: resolveName(r.final.downloaderId, downloaders.value),
      fellBack: r.fellBack.downloader,
    },
    {
      label: 'Library',
      name: resolveName(r.final.libraryId, libraries.value),
      fellBack: r.fellBack.library,
    },
    {
      label: 'Name Template',
      name: resolveName(r.final.nameTemplateId, nameTemplates.value),
      fellBack: r.fellBack.nameTemplate,
    },
  ]
})
</script>

<template>
  <div class="flex flex-col gap-6">
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <div>
            <CardTitle class="text-xl font-semibold mb-2">Routing</CardTitle>
            <p class="text-sm text-muted-foreground">
              Routing rules decide where each grab goes: which downloader runs it, which library
              receives it, and how it's named. Rules evaluate top to bottom — the first match wins.
            </p>
          </div>
          <Button @click="handleAddRule">
            <Plus class="mr-2 size-4" />
            Add Rule
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <div v-if="rulesLoading || fieldsLoading" class="space-y-3">
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
          <Skeleton class="h-12 w-full" />
        </div>

        <template v-else>
          <p v-if="reorderError" class="text-sm text-destructive mb-3">{{ reorderError }}</p>

          <div v-if="(!rules || rules.length === 0) && newRules.length === 0" class="py-8">
            <Empty>
              <EmptyMedia variant="icon">
                <Route class="size-5" />
              </EmptyMedia>
              <EmptyContent>
                <EmptyHeader>
                  <EmptyTitle>No routing rules configured</EmptyTitle>
                </EmptyHeader>
                <EmptyDescription>
                  Grabs use the default downloader, library, and name template. Click "Add Rule" to
                  route specific releases differently.
                </EmptyDescription>
              </EmptyContent>
            </Empty>
          </div>

          <div v-else class="space-y-3">
            <!-- Existing rules — RoutingRuleCard self-invalidates on save/delete -->
            <RoutingRuleCard
              v-for="(rule, idx) in rules"
              :key="rule.id"
              :rule="rule"
              :fields="fields || []"
              :libraries="libraries || []"
              :name-templates="nameTemplates || []"
              :downloaders="downloaders || []"
              :can-move-up="idx > 0"
              :can-move-down="idx < (rules?.length ?? 0) - 1"
              @move-up="moveRule(idx, -1)"
              @move-down="moveRule(idx, 1)"
            />

            <!-- New rule cards — parent removes the slot on save/cancel/delete -->
            <RoutingRuleCard
              v-for="(_, idx) in newRules"
              :key="`new-${idx}`"
              :rule="null"
              :fields="fields || []"
              :libraries="libraries || []"
              :name-templates="nameTemplates || []"
              :downloaders="downloaders || []"
              :is-new="true"
              @saved="handleNewSaved(idx)"
              @cancelled="removeNew(idx)"
              @deleted="removeNew(idx)"
            />
          </div>
        </template>
      </CardContent>
    </Card>

    <!-- Dry run -->
    <Card>
      <CardHeader>
        <CardTitle class="text-xl font-semibold mb-2 flex items-center gap-2">
          <FlaskConical class="size-5" />
          Test rules
        </CardTitle>
        <p class="text-sm text-muted-foreground">
          Dry-run the rules against a release title. Evaluation happens at grab — conditions on
          fields that are only knowable later (e.g. mediainfo) show as indeterminate.
        </p>
      </CardHeader>
      <CardContent>
        <div class="flex flex-col sm:flex-row gap-3 mb-4">
          <div class="flex-1 flex flex-col gap-2">
            <Label>Release title</Label>
            <Input
              v-model="dryRunTitle"
              placeholder="e.g. Dune.2021.2160p.BluRay.REMUX-FraMeSToR"
              @keydown.enter="handleDryRun"
            />
          </div>
          <div class="flex flex-col gap-2">
            <Label>Media type</Label>
            <Select v-model="dryRunMediaType">
              <SelectTrigger class="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="movie">Movie</SelectItem>
                <SelectItem value="series">Series</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="flex items-end">
            <Button
              :disabled="dryRunMutation.isPending.value || !dryRunTitle.trim()"
              @click="handleDryRun"
            >
              Evaluate
            </Button>
          </div>
        </div>

        <p v-if="dryRunError" class="text-sm text-destructive">{{ dryRunError }}</p>

        <div v-if="dryRunResult" class="space-y-4">
          <!-- Per-rule tri-state results -->
          <div class="space-y-2">
            <div
              v-for="re in dryRunResult.rules"
              :key="re.id"
              class="flex items-center gap-2 rounded-md border px-3 py-2 text-sm"
              :class="{
                'bg-green-500/10 border-green-500/30': re.fired,
                'opacity-60': re.skipped,
              }"
            >
              <CheckCircle2 v-if="re.result === 'true'" class="size-4 text-green-500 shrink-0" />
              <CircleHelp
                v-else-if="re.result === 'unknown'"
                class="size-4 text-yellow-500 shrink-0"
              />
              <XCircle v-else class="size-4 text-muted-foreground shrink-0" />
              <span class="font-medium">{{ re.name }}</span>
              <Badge v-if="re.fired" variant="default" class="text-xs">Fired</Badge>
              <Badge v-if="re.skipped" variant="secondary" class="text-xs">Skipped</Badge>
              <Badge v-if="re.result === 'unknown'" variant="outline" class="text-xs">
                Not yet evaluable
              </Badge>
              <Badge v-if="re.incomplete" variant="outline" class="text-xs">Incomplete</Badge>
            </div>
            <div
              v-if="(dryRunResult.rules?.length ?? 0) === 0"
              class="text-sm text-muted-foreground"
            >
              No rules evaluated — everything falls back to the defaults.
            </div>
          </div>

          <!-- Final action set -->
          <div>
            <span class="text-sm font-medium block mb-2">Resulting dispatch</span>
            <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div v-for="slot in finalSlots" :key="slot.label" class="rounded-md border px-3 py-2">
                <div class="text-xs text-muted-foreground">{{ slot.label }}</div>
                <div class="text-sm font-medium truncate">
                  {{ slot.name ?? 'Not set' }}
                  <Badge v-if="slot.fellBack" variant="secondary" class="text-xs ml-1">
                    default
                  </Badge>
                </div>
              </div>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
