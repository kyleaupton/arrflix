<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { getV1Settings, patchV1Settings } from '@/client/sdk.gen'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import VersionCard from '@/components/settings/VersionCard.vue'

type SettingsMap = Record<string, unknown>

const isLoading = ref(true)
const error = ref<string | null>(null)
const settings = ref<SettingsMap>({})
const isSaving = ref(false)

// TMDB key edit state
const tmdbKeyInput = ref('')
const tmdbEditing = ref(false)
const tmdbSaving = ref(false)
const tmdbError = ref<string | null>(null)

async function loadSettings() {
  isLoading.value = true
  error.value = null
  try {
    const res = await getV1Settings<true>({ throwOnError: true })
    settings.value = res.data as SettingsMap
  } catch {
    error.value = 'Failed to load settings'
  } finally {
    isLoading.value = false
  }
}

async function saveSetting(key: string, value: unknown) {
  isSaving.value = true
  try {
    await patchV1Settings<true>({ throwOnError: true, body: { key, value } })
    // Optimistically update local state
    settings.value = { ...settings.value, [key]: value }
  } finally {
    isSaving.value = false
  }
}

onMounted(loadSettings)

const siteTitle = computed({
  get: () => String(settings.value['site.title'] ?? ''),
  set: (v: string) => saveSetting('site.title', v),
})

const signupStrategy = computed({
  get: () => String(settings.value['auth.signup_strategy'] ?? 'invite_only'),
  set: (v: string) => saveSetting('auth.signup_strategy', v),
})

const tmdbKeyDisplay = computed(() => {
  const val = settings.value['tmdb.api_key']
  if (typeof val === 'string' && val !== '') return val
  return ''
})

function startTmdbEdit() {
  tmdbKeyInput.value = ''
  tmdbEditing.value = true
  tmdbError.value = null
}

function cancelTmdbEdit() {
  tmdbEditing.value = false
  tmdbKeyInput.value = ''
  tmdbError.value = null
}

async function saveTmdbKey() {
  if (!tmdbKeyInput.value) return
  tmdbSaving.value = true
  tmdbError.value = null
  try {
    await patchV1Settings<true>({
      throwOnError: true,
      body: { key: 'tmdb.api_key', value: tmdbKeyInput.value },
    })
    settings.value = { ...settings.value, 'tmdb.api_key': '********' }
    tmdbEditing.value = false
    tmdbKeyInput.value = ''
  } catch (err: any) {
    tmdbError.value = err.response?.data?.error || 'Failed to save TMDB key'
  } finally {
    tmdbSaving.value = false
  }
}

// const maxPerUser = computed({
//   get: () => Number(settings.value['requests.max_per_user'] ?? 0),
//   set: (v: number) => saveSetting('requests.max_per_user', v),
// })
</script>

<template>
  <div class="flex flex-col gap-6">
    <div>
      <h1 class="text-2xl font-semibold">General Settings</h1>
    </div>
    <div
      v-if="error"
      class="p-4 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive"
    >
      {{ error }}
    </div>
    <div v-if="isLoading" class="space-y-3">
      <Skeleton class="h-24 w-full" />
      <Skeleton class="h-24 w-full" />
      <Skeleton class="h-24 w-full" />
    </div>
    <div v-else class="grid gap-4 md:grid-cols-2">
      <VersionCard />

      <Card>
        <CardHeader>
          <CardTitle>Site</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="flex flex-col gap-2">
            <Label for="site-title" class="text-sm text-muted-foreground">Site title</Label>
            <Input
              id="site-title"
              :model-value="siteTitle"
              @update:model-value="siteTitle = String($event)"
              :disabled="isSaving"
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Integrations</CardTitle>
        </CardHeader>
        <CardContent class="grid gap-0">
          <!-- TMDB -->
          <div class="py-3 first:pt-0 last:pb-0 border-b last:border-b-0">
            <div class="flex items-center justify-between">
              <div class="flex flex-col gap-0.5">
                <span class="text-sm font-medium">TMDB</span>
                <span class="text-xs text-muted-foreground">Media metadata provider</span>
              </div>
              <div class="flex items-center gap-3">
                <span
                  class="flex items-center gap-1.5 text-xs"
                  :class="tmdbKeyDisplay ? 'text-emerald-500' : 'text-muted-foreground'"
                >
                  <span
                    class="size-1.5 rounded-full"
                    :class="tmdbKeyDisplay ? 'bg-emerald-500' : 'bg-muted-foreground'"
                  />
                  {{ tmdbKeyDisplay ? 'Connected' : 'Not configured' }}
                </span>
                <Button
                  v-if="!tmdbEditing"
                  size="sm"
                  variant="outline"
                  @click="startTmdbEdit"
                >
                  {{ tmdbKeyDisplay ? 'Change' : 'Configure' }}
                </Button>
              </div>
            </div>
            <!-- Inline edit -->
            <div v-if="tmdbEditing" class="mt-3 flex flex-col gap-2">
              <div v-if="tmdbError" class="text-sm text-destructive">{{ tmdbError }}</div>
              <div class="flex items-center gap-2">
                <Input
                  id="tmdb-key"
                  v-model="tmdbKeyInput"
                  type="password"
                  placeholder="Enter TMDB API key"
                  class="flex-1"
                  :disabled="tmdbSaving"
                />
                <Button size="sm" :disabled="tmdbSaving || !tmdbKeyInput" @click="saveTmdbKey">
                  {{ tmdbSaving ? 'Validating...' : 'Save' }}
                </Button>
                <Button size="sm" variant="outline" :disabled="tmdbSaving" @click="cancelTmdbEdit">
                  Cancel
                </Button>
              </div>
            </div>
          </div>

          <!-- OpenSubtitles (placeholder) -->
          <div class="py-3 first:pt-0 last:pb-0 border-b last:border-b-0">
            <div class="flex items-center justify-between">
              <div class="flex flex-col gap-0.5">
                <span class="text-sm font-medium">OpenSubtitles</span>
                <span class="text-xs text-muted-foreground">Automatic subtitle downloads</span>
              </div>
              <div class="flex items-center gap-3">
                <span class="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <span class="size-1.5 rounded-full bg-muted-foreground" />
                  Not configured
                </span>
                <Button size="sm" variant="outline" disabled>
                  Configure
                </Button>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  </div>
</template>
