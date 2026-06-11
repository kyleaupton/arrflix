<script setup lang="ts">
import { ref } from 'vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { Plus, Star } from 'lucide-vue-next'
import {
  qualityListProfilesOptions,
  qualityListProfilesQueryKey,
  qualityCreateProfileMutation,
  qualityListTiersOptions,
  qualityListTiersQueryKey,
  qualityBindTierMutation,
  qualityListCustomFormatsOptions,
  routingGetFieldsOptions,
} from '@/client/@tanstack/vue-query.gen'
import { useQualityBins } from '@/composables/useQualityBins'
import { problemMessage } from '@/lib/api'
import QualityProfileCard from '@/components/settings/QualityProfileCard.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'

const queryClient = useQueryClient()

const { data: profiles, isLoading: profilesLoading } = useQuery(qualityListProfilesOptions())
const { data: fields } = useQuery(routingGetFieldsOptions())
const { data: tiers } = useQuery(qualityListTiersOptions())
const { data: customFormats } = useQuery(qualityListCustomFormatsOptions())

// Vocabulary per domain — used to seed a brand-new profile with the full ranked
// bin list (a valid skeleton the user then trims), since the service requires
// non-empty bins + a cutoff at create time.
const movieVocab = useQualityBins('movie')
const seriesVocab = useQualityBins('series')

// ----- Add profile -----

const addOpen = ref(false)
const newName = ref('')
const newDomain = ref<'movie' | 'series'>('movie')
const addError = ref<string | null>(null)

const openAdd = () => {
  newName.value = ''
  newDomain.value = 'movie'
  addError.value = null
  addOpen.value = true
}

const createMutation = useMutation({
  ...qualityCreateProfileMutation(),
  onSuccess: () => {
    queryClient.invalidateQueries({ queryKey: qualityListProfilesQueryKey() })
    addOpen.value = false
  },
  onError: (err) => {
    addError.value = problemMessage(err, 'Failed to create profile')
  },
})

const handleCreate = () => {
  addError.value = null
  if (!newName.value.trim()) {
    addError.value = 'Name is required'
    return
  }
  // Seed best-first: the vocabulary is ordered worst→best, so reverse it.
  const vocab =
    newDomain.value === 'movie' ? movieVocab.vocabulary.value : seriesVocab.vocabulary.value
  const bins = [...vocab].reverse().map((o) => o.bin)
  if (bins.length === 0) {
    addError.value = 'Bin vocabulary is still loading — try again in a moment'
    return
  }
  createMutation.mutate({
    body: {
      name: newName.value,
      domain: newDomain.value,
      bins,
      cutoff: bins[0]!,
      minSeeders: 0,
      indexers: [],
      gates: [],
      sizeOverrides: [],
      formats: [],
    },
  })
}

// ----- Tiers matrix -----

const TIERS = ['HD', '4K'] as const
const DOMAINS = ['movie', 'series'] as const

const profilesForDomain = (domain: 'movie' | 'series') =>
  (profiles.value ?? []).filter((p) => p.domain === domain)

const boundProfileId = (tier: 'HD' | '4K', domain: 'movie' | 'series'): string =>
  (tiers.value ?? []).find((t) => t.tier === tier && t.domain === domain)?.profileId ?? ''

const tierError = ref<string | null>(null)
const bindMutation = useMutation({
  ...qualityBindTierMutation(),
  onSuccess: () => {
    tierError.value = null
    queryClient.invalidateQueries({ queryKey: qualityListTiersQueryKey() })
  },
  onError: (err) => {
    tierError.value = problemMessage(err, 'Failed to bind tier')
  },
})

const bindTier = (tier: 'HD' | '4K', domain: 'movie' | 'series', value: unknown) => {
  const profileId = String(value ?? '')
  if (!profileId) return
  bindMutation.mutate({ body: { tier, domain, profileId } })
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <Tabs default-value="profiles">
      <TabsList>
        <TabsTrigger value="profiles">Profiles</TabsTrigger>
        <TabsTrigger value="tiers">Tiers</TabsTrigger>
        <TabsTrigger value="formats">Custom Formats</TabsTrigger>
      </TabsList>

      <!-- Profiles -->
      <TabsContent value="profiles">
        <Card>
          <CardHeader>
            <div class="flex items-center justify-between">
              <div>
                <CardTitle class="text-xl font-semibold mb-2">Quality Profiles</CardTitle>
                <p class="text-sm text-muted-foreground">
                  A profile decides which release qualities are acceptable for a want, how they
                  rank, where upgrading stops, and which releases are gated or scored.
                </p>
              </div>
              <Button @click="openAdd">
                <Plus class="mr-2 size-4" />
                Add Profile
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <div v-if="profilesLoading" class="space-y-3">
              <Skeleton class="h-12 w-full" />
              <Skeleton class="h-12 w-full" />
              <Skeleton class="h-12 w-full" />
            </div>

            <template v-else>
              <div v-if="!profiles || profiles.length === 0" class="py-8">
                <Empty>
                  <EmptyMedia variant="icon">
                    <Star class="size-5" />
                  </EmptyMedia>
                  <EmptyContent>
                    <EmptyHeader>
                      <EmptyTitle>No quality profiles</EmptyTitle>
                    </EmptyHeader>
                    <EmptyDescription>
                      Click "Add Profile" to define an acceptable-quality ladder for a media domain.
                    </EmptyDescription>
                  </EmptyContent>
                </Empty>
              </div>

              <div v-else class="space-y-3">
                <QualityProfileCard
                  v-for="profile in profiles"
                  :key="profile.id"
                  :profile="profile"
                  :fields="fields || []"
                />
              </div>
            </template>
          </CardContent>
        </Card>
      </TabsContent>

      <!-- Tiers -->
      <TabsContent value="tiers">
        <Card>
          <CardHeader>
            <CardTitle class="text-xl font-semibold mb-2">Tier bindings</CardTitle>
            <p class="text-sm text-muted-foreground">
              Requesters pick a coarse tier (HD or 4K); each tier resolves to a concrete profile per
              media domain.
            </p>
          </CardHeader>
          <CardContent>
            <p v-if="tierError" class="text-sm text-destructive mb-3">{{ tierError }}</p>
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div v-for="tier in TIERS" :key="tier" class="rounded-md border p-4">
                <span class="text-sm font-medium block mb-3">{{ tier }}</span>
                <div class="space-y-3">
                  <div v-for="dom in DOMAINS" :key="dom" class="flex flex-col gap-1.5">
                    <Label class="capitalize">{{ dom }}</Label>
                    <Select
                      :model-value="boundProfileId(tier, dom)"
                      @update:model-value="(v) => bindTier(tier, dom, v)"
                    >
                      <SelectTrigger class="w-full">
                        <SelectValue placeholder="No profile bound" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem v-for="p in profilesForDomain(dom)" :key="p.id" :value="p.id">
                          {{ p.name }}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </TabsContent>

      <!-- Custom formats (read-only) -->
      <TabsContent value="formats">
        <Card>
          <CardHeader>
            <CardTitle class="text-xl font-semibold mb-2">Custom formats</CardTitle>
            <p class="text-sm text-muted-foreground">
              Custom formats are reusable named scorers profiles assign weights to. Creating &amp;
              editing custom formats is coming soon — for now they are seeded and read-only.
            </p>
          </CardHeader>
          <CardContent>
            <div
              v-if="!customFormats || customFormats.length === 0"
              class="text-sm text-muted-foreground"
            >
              No custom formats defined.
            </div>
            <Table v-else>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Domain</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow v-for="cf in customFormats" :key="cf.id">
                  <TableCell class="font-medium">{{ cf.name }}</TableCell>
                  <TableCell class="capitalize">{{ cf.domain }}</TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </TabsContent>
    </Tabs>

    <!-- Add profile dialog -->
    <Dialog v-model:open="addOpen">
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New quality profile</DialogTitle>
          <DialogDescription>
            Pick a name and media domain. The domain is fixed after creation — it determines the bin
            vocabulary the profile speaks.
          </DialogDescription>
        </DialogHeader>
        <div class="flex flex-col gap-4 py-2">
          <div class="flex flex-col gap-2">
            <Label>Name</Label>
            <Input
              v-model="newName"
              placeholder="e.g. 1080p Bluray"
              @keydown.enter="handleCreate"
            />
          </div>
          <div class="flex flex-col gap-2">
            <Label>Domain</Label>
            <Select v-model="newDomain">
              <SelectTrigger class="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="movie">Movie</SelectItem>
                <SelectItem value="series">Series</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <p v-if="addError" class="text-sm text-destructive">{{ addError }}</p>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="addOpen = false">Cancel</Button>
          <Button :disabled="createMutation.isPending.value" @click="handleCreate">Create</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
