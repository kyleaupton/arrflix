<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import {
  qualityGetProfileOptions,
  qualityGetProfileQueryKey,
  qualityListProfilesQueryKey,
  qualityUpdateProfileMutation,
  qualityDeleteProfileMutation,
  qualityTestProfileMutation,
  qualityListSizeDefaultsOptions,
  qualityListCustomFormatsOptions,
} from '@/client/@tanstack/vue-query.gen'
import type {
  QualityProfile,
  BinKey,
  QualityBinSize,
  ProfileFormat,
  QualityEvaluation,
  Trace,
  FieldDefinition,
} from '@/client/types.gen'
import { useQualityBins, binKeyEquals } from '@/composables/useQualityBins'
import { useModal } from '@/composables/useModal'
import { problemMessage } from '@/lib/api'
import ConditionBuilder from '@/components/conditions/ConditionBuilder.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  FlaskConical,
  Plus,
  Trash2,
  X,
  XCircle,
} from 'lucide-vue-next'

interface Props {
  profile: QualityProfile
  fields: FieldDefinition[]
}

const props = defineProps<Props>()

const modal = useModal()
const queryClient = useQueryClient()

const domain = computed(() => props.profile.domain as 'movie' | 'series')
const { label: binLabel, available: availableBins } = useQualityBins(domain)

const isExpanded = ref(false)

// Detail (carries the scored-format assignments the list row omits) loads only
// while the card is open.
const { data: detail } = useQuery(
  computed(() => ({
    ...qualityGetProfileOptions({ path: { id: props.profile.id } }),
    enabled: isExpanded.value,
  })),
)

const { data: sizeDefaults } = useQuery(
  computed(() => qualityListSizeDefaultsOptions({ query: { domain: domain.value } })),
)
const { data: customFormats } = useQuery(qualityListCustomFormatsOptions())
const domainFormats = computed(() =>
  (customFormats.value ?? []).filter((cf) => cf.domain === props.profile.domain),
)

// ----- Editable form state -----

const name = ref('')
const minSeeders = ref(0)
const bins = ref<BinKey[]>([])
const cutoff = ref<BinKey | null>(null)
// Per-bin size band edits, keyed by bin identity; empty string = unbounded/unset
// for that side. Only bins with a non-empty field emit a QualityBinSize.
const sizeEdits = ref<Record<string, { min: string; preferred: string; max: string }>>({})
const gates = ref<{ name: string; tree: unknown }[]>([])
// Per-custom-format weight, keyed by id; empty string = not assigned.
const formatWeights = ref<Record<string, string>>({})

const saveError = ref<string | null>(null)
const addBinValue = ref('')

const binId = (b: BinKey) => `${b.Source}|${b.Resolution}|${b.Modifier}`

function resetForm() {
  saveError.value = null
  addBinValue.value = ''
  name.value = props.profile.name
  minSeeders.value = props.profile.minSeeders
  bins.value = (props.profile.bins ?? []).map((b) => ({ ...b }))
  cutoff.value = props.profile.cutoff ? { ...props.profile.cutoff } : null

  const edits: Record<string, { min: string; preferred: string; max: string }> = {}
  for (const so of props.profile.sizeOverrides ?? []) {
    edits[binId(so.bin)] = {
      min: so.min ? String(so.min) : '',
      preferred: so.preferred ? String(so.preferred) : '',
      max: so.max ? String(so.max) : '',
    }
  }
  sizeEdits.value = edits

  // gates is the canonical [{name, tree}] array carried opaquely on the row.
  const rawGates = props.profile.gates
  gates.value = Array.isArray(rawGates)
    ? rawGates.map((g) => ({ name: String(g?.name ?? ''), tree: g?.tree ?? null }))
    : []
}

watch(() => props.profile, resetForm, { immediate: true, deep: true })

// Formats arrive with the detail fetch; seed the weight inputs once.
watch(
  () => detail.value,
  (d) => {
    if (!d) return
    const weights: Record<string, string> = {}
    for (const f of d.formats ?? []) {
      weights[f.customFormatId] = String(f.weight)
    }
    formatWeights.value = weights
  },
  { immediate: true },
)

// Keep the cutoff valid: if the ranked list drops the current cutoff, fall back
// to the best (index 0) bin so the engine invariant cutoff ∈ bins holds.
watch(
  bins,
  (list) => {
    if (!list.length) {
      cutoff.value = null
      return
    }
    if (!cutoff.value || !list.some((b) => binKeyEquals(b, cutoff.value as BinKey))) {
      cutoff.value = { ...list[0]! }
    }
  },
  { deep: true },
)

// ----- Bin editing -----

const moveBin = (index: number, delta: number) => {
  const target = index + delta
  if (target < 0 || target >= bins.value.length) return
  const list = bins.value
  ;[list[index], list[target]] = [list[target]!, list[index]!]
}

const removeBin = (index: number) => {
  bins.value.splice(index, 1)
}

const addBinOptions = computed(() => availableBins(bins.value))

const handleAddBin = (value: unknown) => {
  const id = String(value ?? '')
  const opt = addBinOptions.value.find((o) => binId(o.bin) === id)
  if (opt) bins.value.push({ ...opt.bin })
  addBinValue.value = ''
}

const cutoffId = computed<string>({
  get: () => (cutoff.value ? binId(cutoff.value) : ''),
  set: (id) => {
    const found = bins.value.find((b) => binId(b) === id)
    cutoff.value = found ? { ...found } : null
  },
})

// ----- Size override helpers -----

const editFor = (bin: BinKey) => {
  const id = binId(bin)
  if (!sizeEdits.value[id]) sizeEdits.value[id] = { min: '', preferred: '', max: '' }
  return sizeEdits.value[id]!
}

const sizeDefaultFor = (bin: BinKey) =>
  (sizeDefaults.value ?? []).find((d) => binKeyEquals(d.bin, bin))

const placeholder = (bin: BinKey, side: 'min' | 'preferred' | 'max') => {
  const d = sizeDefaultFor(bin)
  if (!d) return 'unbounded'
  const v = d[side]
  return v ? String(v) : 'unbounded'
}

// ----- Gate editing -----

const addGate = () => {
  gates.value.push({ name: '', tree: null })
}

const removeGate = (index: number) => {
  gates.value.splice(index, 1)
}

// ----- Build & persist -----

function buildBody() {
  const sizeOverrides: QualityBinSize[] = []
  for (const bin of bins.value) {
    const e = sizeEdits.value[binId(bin)]
    if (!e) continue
    if (e.min === '' && e.preferred === '' && e.max === '') continue
    sizeOverrides.push({
      bin,
      min: Number(e.min) || 0,
      preferred: Number(e.preferred) || 0,
      max: Number(e.max) || 0,
    })
  }

  const formats: ProfileFormat[] = []
  for (const cf of domainFormats.value) {
    const w = formatWeights.value[cf.id]
    if (w === undefined || w === '') continue
    formats.push({ customFormatId: cf.id, weight: Number(w) || 0 })
  }

  // Drop gates the user hasn't authored conditions for yet.
  const builtGates = gates.value.filter((g) => g.tree != null)

  return {
    name: name.value,
    domain: domain.value,
    bins: bins.value,
    cutoff: cutoff.value ?? ({ Source: '', Resolution: '', Modifier: '' } as BinKey),
    minSeeders: Number(minSeeders.value) || 0,
    // Indexer scoping is preserved untouched — there is no UUID-keyed indexer
    // source to drive a picker yet (see indexers spec, open question #9).
    indexers: props.profile.indexers ?? [],
    gates: builtGates,
    sizeOverrides,
    formats,
  }
}

const updateMutation = useMutation({
  ...qualityUpdateProfileMutation(),
  onSuccess: () => {
    queryClient.invalidateQueries({ queryKey: qualityListProfilesQueryKey() })
    queryClient.invalidateQueries({
      queryKey: qualityGetProfileQueryKey({ path: { id: props.profile.id } }),
    })
    isExpanded.value = false
  },
  onError: (err) => {
    saveError.value = problemMessage(err, 'Failed to save quality profile')
  },
})

const deleteMutation = useMutation({
  ...qualityDeleteProfileMutation(),
  onSuccess: () => {
    queryClient.invalidateQueries({ queryKey: qualityListProfilesQueryKey() })
  },
  onError: (err) => {
    saveError.value = problemMessage(err, 'Failed to delete quality profile')
  },
})

const handleSave = () => {
  saveError.value = null
  if (!name.value.trim()) {
    saveError.value = 'Name is required'
    return
  }
  if (bins.value.length === 0) {
    saveError.value = 'Add at least one quality bin'
    return
  }
  if (!cutoff.value || !bins.value.some((b) => binKeyEquals(b, cutoff.value as BinKey))) {
    saveError.value = 'Cutoff must be one of the ranked bins'
    return
  }
  updateMutation.mutate({ path: { id: props.profile.id }, body: buildBody() })
}

const handleDelete = async () => {
  const confirmed = await modal.confirm({
    title: 'Delete Quality Profile',
    message: `Are you sure you want to delete "${props.profile.name}"?`,
    severity: 'danger',
  })
  if (!confirmed) return
  deleteMutation.mutate({ path: { id: props.profile.id } })
}

const toggleExpand = () => {
  if (isExpanded.value) resetForm()
  isExpanded.value = !isExpanded.value
}

// ----- Test panel -----

const testTitle = ref('')
const testResult = ref<QualityEvaluation | null>(null)
const testError = ref<string | null>(null)

const testMutation = useMutation({
  ...qualityTestProfileMutation(),
  onSuccess: (data) => {
    testResult.value = data
    testError.value = null
  },
  onError: (err) => {
    testError.value = problemMessage(err, 'Failed to test profile')
    testResult.value = null
  },
})

const handleTest = () => {
  if (!testTitle.value.trim()) return
  testMutation.mutate({ path: { id: props.profile.id }, body: { title: testTitle.value } })
}

// Flatten the trace tree into indented rows for a compact verdict view.
interface TraceRow {
  depth: number
  op: string
  result: string
}
const flattenTrace = (nodes: Trace[] | null | undefined, depth = 0): TraceRow[] => {
  const out: TraceRow[] = []
  for (const n of nodes ?? []) {
    out.push({ depth, op: n.op, result: n.result })
    if (n.children?.length) out.push(...flattenTrace(n.children, depth + 1))
  }
  return out
}
const traceRows = computed(() => flattenTrace(testResult.value?.Trace))
</script>

<template>
  <div class="rounded-lg border bg-card text-card-foreground shadow-sm">
    <!-- Collapsed header -->
    <div class="flex items-center gap-3 px-4 py-3 cursor-pointer select-none" @click="toggleExpand">
      <span class="font-medium truncate">{{ profile.name }}</span>
      <Badge variant="secondary" class="text-xs shrink-0">{{ profile.domain }}</Badge>
      <span class="text-sm text-muted-foreground truncate ml-2 hidden sm:inline">
        {{ profile.bins?.length ?? 0 }} bins
      </span>
      <div class="ml-auto shrink-0">
        <ChevronUp v-if="isExpanded" class="size-4 text-muted-foreground" />
        <ChevronDown v-else class="size-4 text-muted-foreground" />
      </div>
    </div>

    <!-- Expanded editor -->
    <div v-if="isExpanded" class="px-4 pb-4">
      <Separator class="mb-4" />

      <!-- Name + domain -->
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-6">
        <div class="flex flex-col gap-2">
          <Label>Name</Label>
          <Input v-model="name" placeholder="Profile name" />
        </div>
        <div class="flex flex-col gap-2">
          <Label>Domain</Label>
          <div class="h-9 flex items-center">
            <Badge variant="secondary">{{ profile.domain }}</Badge>
            <span class="text-xs text-muted-foreground ml-2">Fixed after creation</span>
          </div>
        </div>
      </div>

      <!-- Ranked bins -->
      <div class="mb-6">
        <span class="text-sm font-medium block mb-1">Ranked quality bins</span>
        <p class="text-xs text-muted-foreground mb-3">
          Best first — index 0 is the most preferred.
        </p>
        <div v-if="bins.length === 0" class="text-sm text-muted-foreground mb-3">
          No bins ranked. Add at least one below.
        </div>
        <div v-else class="space-y-2 mb-3">
          <div
            v-for="(bin, index) in bins"
            :key="binId(bin)"
            class="flex items-center gap-2 rounded-md border px-3 py-2"
          >
            <span class="text-xs text-muted-foreground w-5 shrink-0">{{ index + 1 }}</span>
            <span class="text-sm flex-1 truncate">{{ binLabel(bin) }}</span>
            <Button
              size="icon"
              variant="ghost"
              class="size-7"
              :disabled="index === 0"
              @click="moveBin(index, -1)"
            >
              <ArrowUp class="size-4" />
            </Button>
            <Button
              size="icon"
              variant="ghost"
              class="size-7"
              :disabled="index === bins.length - 1"
              @click="moveBin(index, 1)"
            >
              <ArrowDown class="size-4" />
            </Button>
            <Button size="icon" variant="ghost" class="size-7" @click="removeBin(index)">
              <X class="size-4" />
            </Button>
          </div>
        </div>
        <Select :model-value="addBinValue" @update:model-value="handleAddBin">
          <SelectTrigger class="w-full sm:w-72">
            <SelectValue placeholder="Add a bin…" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="opt in addBinOptions" :key="binId(opt.bin)" :value="binId(opt.bin)">
              {{ opt.name }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <!-- Cutoff -->
      <div class="mb-6">
        <span class="text-sm font-medium block mb-1">Upgrade cutoff</span>
        <p class="text-xs text-muted-foreground mb-3">
          Upgrading stops once a release reaches this bin. Must be one of the ranked bins.
        </p>
        <Select v-model="cutoffId">
          <SelectTrigger class="w-full sm:w-72">
            <SelectValue placeholder="Select cutoff bin" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="bin in bins" :key="binId(bin)" :value="binId(bin)">
              {{ binLabel(bin) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <!-- Size overrides -->
      <div v-if="bins.length" class="mb-6">
        <span class="text-sm font-medium block mb-1">Per-bin size overrides</span>
        <p class="text-xs text-muted-foreground mb-3">
          Bytes per minute. Leave blank to use the global default (shown as placeholder). 0 means
          unbounded.
        </p>
        <div class="space-y-2">
          <div
            v-for="bin in bins"
            :key="binId(bin)"
            class="grid grid-cols-1 sm:grid-cols-[1fr_repeat(3,minmax(0,7rem))] gap-2 items-center"
          >
            <span class="text-sm truncate">{{ binLabel(bin) }}</span>
            <Input
              v-model="editFor(bin).min"
              type="number"
              :placeholder="placeholder(bin, 'min')"
              aria-label="Min"
            />
            <Input
              v-model="editFor(bin).preferred"
              type="number"
              :placeholder="placeholder(bin, 'preferred')"
              aria-label="Preferred"
            />
            <Input
              v-model="editFor(bin).max"
              type="number"
              :placeholder="placeholder(bin, 'max')"
              aria-label="Max"
            />
          </div>
        </div>
      </div>

      <!-- Min seeders -->
      <div class="mb-6">
        <span class="text-sm font-medium block mb-1">Minimum seeders</span>
        <p class="text-xs text-muted-foreground mb-3">
          Reject torrents with fewer seeders than this.
        </p>
        <Input v-model="minSeeders" type="number" class="w-full sm:w-40" />
      </div>

      <!-- Indexer scoping (deferred) -->
      <div class="mb-6">
        <span class="text-sm font-medium block mb-1">Indexer scoping</span>
        <p class="text-xs text-muted-foreground">
          Coming with indexer management — searches currently fan out to all indexers.
        </p>
      </div>

      <!-- Hard gates -->
      <div class="mb-6">
        <div class="flex items-center justify-between mb-3">
          <span class="text-sm font-medium">Hard gates</span>
          <Button size="sm" variant="outline" @click="addGate">
            <Plus class="size-3 mr-1" />
            Add Gate
          </Button>
        </div>
        <p class="text-xs text-muted-foreground mb-3">
          A gate whose conditions match rejects the release outright.
        </p>
        <div v-if="gates.length === 0" class="text-sm text-muted-foreground">
          No gates configured.
        </div>
        <div v-else class="space-y-4">
          <div v-for="(gate, index) in gates" :key="index" class="rounded-md border p-3">
            <div class="flex items-center gap-2 mb-3">
              <Input v-model="gate.name" placeholder="Gate name" class="flex-1" />
              <Button size="icon" variant="ghost" class="shrink-0" @click="removeGate(index)">
                <Trash2 class="size-4" />
              </Button>
            </div>
            <ConditionBuilder v-model="gate.tree" :fields="props.fields" />
          </div>
        </div>
      </div>

      <!-- Scored formats -->
      <div class="mb-6">
        <span class="text-sm font-medium block mb-1">Scored custom formats</span>
        <p class="text-xs text-muted-foreground mb-3">
          Assign a weight to add to a release's score when the format matches. Leave blank to leave
          the format unassigned.
        </p>
        <div v-if="domainFormats.length === 0" class="text-sm text-muted-foreground">
          No custom formats defined for this domain.
        </div>
        <div v-else class="space-y-2">
          <div
            v-for="cf in domainFormats"
            :key="cf.id"
            class="flex items-center gap-3 rounded-md border px-3 py-2"
          >
            <span class="text-sm flex-1 truncate">{{ cf.name }}</span>
            <Input
              v-model="formatWeights[cf.id]"
              type="number"
              placeholder="weight"
              class="w-28"
              aria-label="Weight"
            />
          </div>
        </div>
      </div>

      <!-- Test panel -->
      <div class="mb-6">
        <span class="text-sm font-medium flex items-center gap-2 mb-1">
          <FlaskConical class="size-4" />
          Test against a release title
        </span>
        <p class="text-xs text-muted-foreground mb-3">
          Parses the title in this profile's domain and runs the engine.
        </p>
        <div class="flex flex-col sm:flex-row gap-2 mb-3">
          <Input
            v-model="testTitle"
            placeholder="e.g. Dune.2021.2160p.BluRay.REMUX-FraMeSToR"
            class="flex-1"
            @keydown.enter="handleTest"
          />
          <Button :disabled="testMutation.isPending.value || !testTitle.trim()" @click="handleTest">
            Test
          </Button>
        </div>

        <p v-if="testError" class="text-sm text-destructive">{{ testError }}</p>

        <div v-if="testResult" class="rounded-md border p-3 space-y-3 text-sm">
          <div class="flex flex-wrap items-center gap-2">
            <CheckCircle2 v-if="testResult.Passed" class="size-4 text-green-500" />
            <XCircle v-else class="size-4 text-destructive" />
            <span class="font-medium">{{ binLabel(testResult.Bin) }}</span>
            <Badge :variant="testResult.Passed ? 'default' : 'destructive'" class="text-xs">
              {{ testResult.Passed ? 'Passed' : 'Rejected' }}
            </Badge>
            <Badge variant="outline" class="text-xs">Score {{ testResult.Score }}</Badge>
            <Badge v-if="testResult.Disposition" variant="secondary" class="text-xs">
              {{ testResult.Disposition }}
            </Badge>
          </div>

          <div v-if="!testResult.Passed && testResult.RejectReason?.Gate" class="text-destructive">
            Rejected by gate "{{ testResult.RejectReason.Gate }}"
            <span v-if="testResult.RejectReason.Detail" class="text-muted-foreground">
              — {{ testResult.RejectReason.Detail }}
            </span>
          </div>

          <div v-if="traceRows.length">
            <span class="text-xs font-medium text-muted-foreground block mb-1">Trace</span>
            <div class="font-mono text-xs space-y-0.5">
              <div
                v-for="(row, i) in traceRows"
                :key="i"
                :style="{ paddingLeft: `${row.depth * 12}px` }"
                class="flex items-center gap-2"
              >
                <span>{{ row.op }}</span>
                <span
                  :class="{
                    'text-green-500': row.result === 'true',
                    'text-muted-foreground': row.result === 'unknown',
                    'text-destructive': row.result === 'false',
                  }"
                >
                  {{ row.result }}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <p v-if="saveError" class="text-sm text-destructive mb-3">{{ saveError }}</p>

      <!-- Footer -->
      <Separator class="mb-4" />
      <div class="flex items-center justify-between">
        <Button
          variant="destructive"
          size="sm"
          :disabled="deleteMutation.isPending.value"
          @click="handleDelete"
        >
          <Trash2 class="size-3 mr-1" />
          Delete
        </Button>
        <div class="flex gap-2">
          <Button variant="outline" size="sm" @click="toggleExpand">Cancel</Button>
          <Button size="sm" :disabled="updateMutation.isPending.value" @click="handleSave"
            >Save</Button
          >
        </div>
      </div>
    </div>
  </div>
</template>
