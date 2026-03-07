import { ref, computed, watch } from 'vue'
import { defineStore } from 'pinia'

export interface AppConfig {
  siteTitle: string
  signupStrategy: string
  version: string
}

export interface SetupSteps {
  admin_account: boolean
  tmdb_api_key: boolean
}

export const useAppStore = defineStore('app', () => {
  const initialized = ref<boolean | null>(null)
  const config = ref<AppConfig>({
    siteTitle: 'Arrflix',
    signupStrategy: 'invite_only',
    version: '',
  })
  const setupSteps = ref<SetupSteps | null>(null)
  const isReady = ref(false)

  const needsSetup = computed(() => initialized.value === false)

  function setFromBootstrap(data: {
    initialized: boolean
    config: AppConfig
    setup?: SetupSteps | null
  }) {
    initialized.value = data.initialized
    config.value = data.config
    setupSteps.value = data.setup ?? null
  }

  // Reactively update the browser tab title
  watch(
    () => config.value.siteTitle,
    (title) => {
      document.title = title || 'Arrflix'
    },
    { immediate: true },
  )

  return {
    initialized,
    config,
    setupSteps,
    isReady,
    needsSetup,
    setFromBootstrap,
  }
})
