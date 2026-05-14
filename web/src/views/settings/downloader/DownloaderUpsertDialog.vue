<script setup lang="ts">
import { ref, inject, computed, watch } from 'vue'
import { useMutation } from '@tanstack/vue-query'
import { Check, Eye, EyeOff } from 'lucide-vue-next'
import {
  downloadersCreateMutation,
  downloadersUpdateMutation,
} from '@/client/@tanstack/vue-query.gen'
import { downloadersTestConfig } from '@/client/sdk.gen'
import { type Downloader, type DownloaderWriteBody } from '@/client/types.gen'
import { problemMessage } from '@/lib/api'
import BaseDialog from '@/components/modals/BaseDialog.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useModal } from '@/composables/useModal'

interface Props {
  downloader?: Downloader | null
}

const props = defineProps<Props>()

const dialogRef = inject('dialogRef') as { value: { close: (data?: unknown) => void } }
const modal = useModal()

// Mutations
const createDownloaderMutation = useMutation(downloadersCreateMutation())
const updateDownloaderMutation = useMutation(downloadersUpdateMutation())

// Form state
const downloaderForm = ref({
  name: '',
  type: 'qbittorrent',
  protocol: 'torrent' as 'torrent' | 'usenet',
  url: '',
  username: '',
  password: '',
  configJson: {} as Record<string, unknown>,
  enabled: true,
  default: false,
})

const downloaderError = ref<string | null>(null)
const isTestingConfig = ref(false)
const showPassword = ref(false)

// Initialize form when downloader changes
watch(
  () => props.downloader,
  (downloader) => {
    if (downloader) {
      downloaderForm.value = {
        name: downloader.name || '',
        type: downloader.type || 'qbittorrent',
        protocol: (downloader.protocol as 'torrent' | 'usenet') || 'torrent',
        url: downloader.url || '',
        username: downloader.username || '',
        password: '', // Don't populate password for security
        configJson:
          (downloader.configJson as unknown as Record<string, unknown>) ||
          ({} as Record<string, unknown>),
        enabled: downloader.enabled ?? true,
        default: downloader.default || false,
      }
    } else {
      downloaderForm.value = {
        name: '',
        type: 'qbittorrent',
        protocol: 'torrent',
        url: '',
        username: '',
        password: '',
        configJson: {} as Record<string, unknown>,
        enabled: true,
        default: false,
      }
    }
    downloaderError.value = null
  },
  { immediate: true },
)

const handleSaveDownloader = async () => {
  if (!downloaderForm.value.name || !downloaderForm.value.url) {
    downloaderError.value = 'Name and URL are required'
    return
  }

  try {
    const body: DownloaderWriteBody = {
      name: downloaderForm.value.name,
      type: downloaderForm.value.type,
      protocol: downloaderForm.value.protocol,
      url: downloaderForm.value.url,
      username: downloaderForm.value.username || '',
      configJson: downloaderForm.value.configJson,
      enabled: downloaderForm.value.enabled,
      default: downloaderForm.value.default,
    }

    if (downloaderForm.value.password !== '') {
      body.password = downloaderForm.value.password
    }

    if (props.downloader?.id) {
      await updateDownloaderMutation.mutateAsync({
        path: { id: props.downloader.id },
        body,
      })
    } else {
      await createDownloaderMutation.mutateAsync({
        body,
      })
    }

    downloaderError.value = null
    dialogRef.value.close({ saved: true })
  } catch (err) {
    downloaderError.value = problemMessage(err, 'Failed to save downloader')
  }
}

const handleTestDownloader = async () => {
  // Validate required fields
  if (!downloaderForm.value.url) {
    downloaderError.value = 'URL is required to test connection'
    return
  }

  // Always test using form values (not saved config)
  isTestingConfig.value = true
  downloaderError.value = null
  try {
    const { data: result } = await downloadersTestConfig<true>({
      throwOnError: true,
      body: {
        type: downloaderForm.value.type,
        url: downloaderForm.value.url,
        username: downloaderForm.value.username || undefined,
        password: downloaderForm.value.password || undefined,
        configJson: downloaderForm.value.configJson,
      },
    })

    if (result.success) {
      downloaderError.value = null
      await modal.alert({
        title: 'Connection Test Successful',
        message:
          result.message || `Connection test passed. Version: ${result.version || 'unknown'}`,
        severity: 'success',
      })
    } else {
      downloaderError.value = result.error || 'Connection test failed'
    }
  } catch (err) {
    downloaderError.value = problemMessage(err, 'Connection test failed')
  } finally {
    isTestingConfig.value = false
  }
}

const handleCancel = () => {
  dialogRef.value.close()
}

const isLoading = computed(
  () => createDownloaderMutation.isPending.value || updateDownloaderMutation.isPending.value,
)

const typeOptions = [{ label: 'qBittorrent', value: 'qbittorrent' }]

const protocolOptions = [
  { label: 'Torrent', value: 'torrent' },
  { label: 'Usenet', value: 'usenet' },
]
</script>

<template>
  <BaseDialog :title="downloader ? 'Edit Downloader' : 'Add Downloader'">
    <div class="flex flex-col gap-4">
      <div
        v-if="downloaderError"
        class="p-4 bg-destructive/10 border border-destructive/30 rounded-lg text-destructive text-sm"
      >
        {{ downloaderError }}
      </div>

      <div class="flex flex-col gap-2">
        <Label for="downloader-name">Name</Label>
        <Input id="downloader-name" v-model="downloaderForm.name" placeholder="Downloader name" />
      </div>

      <div class="flex flex-col gap-2">
        <Label for="downloader-type">Type</Label>
        <Select v-model="downloaderForm.type" disabled>
          <SelectTrigger id="downloader-type" class="w-full">
            <SelectValue placeholder="Select type" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="option in typeOptions" :key="option.value" :value="option.value">
              {{ option.label }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div class="flex flex-col gap-2">
        <Label for="downloader-protocol">Protocol</Label>
        <Select v-model="downloaderForm.protocol" disabled>
          <SelectTrigger id="downloader-protocol" class="w-full">
            <SelectValue placeholder="Select protocol" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem v-for="option in protocolOptions" :key="option.value" :value="option.value">
              {{ option.label }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div class="flex flex-col gap-2">
        <Label for="downloader-url">URL</Label>
        <Input
          id="downloader-url"
          v-model="downloaderForm.url"
          placeholder="http://localhost:8080"
        />
      </div>

      <div class="flex flex-col gap-2">
        <Label for="downloader-username">Username (optional)</Label>
        <Input id="downloader-username" v-model="downloaderForm.username" />
      </div>

      <div class="flex flex-col gap-2">
        <Label for="downloader-password">Password (optional)</Label>
        <div class="relative">
          <Input
            id="downloader-password"
            v-model="downloaderForm.password"
            :type="showPassword ? 'text' : 'password'"
            :placeholder="downloader ? 'Leave blank to keep current' : ''"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            class="absolute right-1 top-1/2 -translate-y-1/2"
            @click="showPassword = !showPassword"
          >
            <Eye v-if="!showPassword" class="size-4" />
            <EyeOff v-else class="size-4" />
          </Button>
        </div>
      </div>

      <div class="flex items-center justify-between">
        <Label for="downloader-enabled">Enabled</Label>
        <Switch id="downloader-enabled" v-model="downloaderForm.enabled" />
      </div>

      <div class="flex items-center justify-between">
        <Label for="downloader-default">Default</Label>
        <Switch id="downloader-default" v-model="downloaderForm.default" />
      </div>
    </div>

    <template #footer>
      <div class="flex items-center justify-between w-full">
        <Button variant="outline" :disabled="isTestingConfig" @click="handleTestDownloader">
          <Check class="mr-2 size-4" />
          Test Connection
        </Button>
        <div class="flex gap-2">
          <Button variant="outline" @click="handleCancel">Cancel</Button>
          <Button :disabled="isLoading" @click="handleSaveDownloader">Save</Button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>
