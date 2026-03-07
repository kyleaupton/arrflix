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
          <CardTitle>TMDB</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="flex flex-col gap-2">
            <Label for="tmdb-key" class="text-sm text-muted-foreground">API Key</Label>
            <div v-if="tmdbError" class="text-sm text-destructive">{{ tmdbError }}</div>
            <template v-if="tmdbEditing">
              <Input
                id="tmdb-key"
                v-model="tmdbKeyInput"
                type="password"
                placeholder="Enter new TMDB API key"
                :disabled="tmdbSaving"
              />
              <div class="flex gap-2">
                <Button size="sm" :disabled="tmdbSaving || !tmdbKeyInput" @click="saveTmdbKey">
                  {{ tmdbSaving ? 'Validating...' : 'Save' }}
                </Button>
                <Button size="sm" variant="outline" :disabled="tmdbSaving" @click="cancelTmdbEdit">
                  Cancel
                </Button>
              </div>
            </template>
            <template v-else>
              <div class="flex items-center gap-2">
                <Input
                  id="tmdb-key"
                  :model-value="tmdbKeyDisplay"
                  type="password"
                  disabled
                  :placeholder="tmdbKeyDisplay ? '' : 'Not configured'"
                />
                <Button size="sm" variant="outline" @click="startTmdbEdit">
                  {{ tmdbKeyDisplay ? 'Change' : 'Set' }}
                </Button>
              </div>
            </template>
          </div>
        </CardContent>
      </Card>
    </div>
  </div>
</template>
