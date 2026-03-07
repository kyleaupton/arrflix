import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import type { ServiceVersionInfo } from '@/client/types.gen'

export type VersionDebugState = 'off' | 'loading' | 'error' | 'up_to_date' | 'update_available' | 'dev' | 'unknown'

const presets: Record<Exclude<VersionDebugState, 'off' | 'loading' | 'error'>, ServiceVersionInfo> = {
  up_to_date: {
    version: 'v1.2.0',
    commit: 'abc1234',
    buildDate: '2026-03-01T00:00:00Z',
    components: { prowlarr: '1.31.2.4975' },
    update: { status: 'up_to_date' },
  },
  update_available: {
    version: 'v1.1.0',
    commit: 'abc1234',
    buildDate: '2026-02-15T00:00:00Z',
    components: { prowlarr: '1.31.2.4975' },
    update: {
      status: 'update_available',
      latest: {
        version: 'v1.2.0',
        tag: 'v1.2.0',
        url: 'https://github.com/example/arrflix/releases/tag/v1.2.0',
        notes: 'Bug fixes and performance improvements.\n- Fixed media scanning issue\n- Improved download speed detection',
        publishedAt: '2026-03-05T12:00:00Z',
      },
    },
  },
  dev: {
    version: 'dev',
    commit: 'abc1234',
    buildDate: '2026-03-07T00:00:00Z',
    update: { status: 'unknown', reason: 'dev_build' },
  },
  unknown: {
    version: 'v1.2.0-rc.1',
    commit: 'abc1234',
    buildDate: '2026-03-06T00:00:00Z',
    update: { status: 'unknown', reason: 'prerelease' },
  },
}

export const useVersionDebugStore = defineStore('versionDebug', () => {
  const state = ref<VersionDebugState>('off')

  const isActive = computed(() => state.value !== 'off')

  const mockData = computed(() => {
    if (state.value === 'off' || state.value === 'loading' || state.value === 'error') return null
    return presets[state.value]
  })

  return { state, isActive, mockData }
})
