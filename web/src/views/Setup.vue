<script setup lang="ts">
import { ref, computed, type HTMLAttributes } from 'vue'
import { useRouter } from 'vue-router'
import { Popcorn, UserPlus, Key } from 'lucide-vue-next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Stepper,
  StepperItem,
  StepperTrigger,
  StepperIndicator,
  StepperTitle,
  StepperSeparator,
} from '@/components/ui/stepper'
import { client } from '@/client/client.gen'
import { bootstrapGet } from '@/client/sdk.gen'
import { useAppStore } from '@/stores/app'

const props = defineProps<{
  class?: HTMLAttributes['class']
}>()

const router = useRouter()
const appStore = useAppStore()

// Determine initial step from bootstrap state
const initialStep = computed(() => {
  if (appStore.setupSteps?.admin_account) return 1
  return 0
})
const currentStep = ref(initialStep.value)

// Admin account form
const email = ref('')
const username = ref('')
const password = ref('')
const confirmPassword = ref('')
const isLoadingAdmin = ref(false)
const adminError = ref<string | null>(null)

// TMDB key form
const tmdbKey = ref('')
const isLoadingTmdb = ref(false)
const tmdbError = ref<string | null>(null)

const adminDone = computed(() => appStore.setupSteps?.admin_account === true)
const tmdbDone = computed(() => appStore.setupSteps?.tmdb_api_key === true)

const steps = [
  { step: 0, label: 'Admin Account', icon: UserPlus },
  { step: 1, label: 'TMDB API Key', icon: Key },
]

async function refreshBootstrap() {
  try {
    const res = await bootstrapGet<true>({ throwOnError: true })
    appStore.setFromBootstrap({
      initialized: res.data.initialized,
      config: res.data.config,
      setup: res.data.setup,
    })
  } catch {
    // ignore
  }
}

async function handleAdminSubmit(e: Event) {
  e.preventDefault()
  adminError.value = null

  if (!email.value || !username.value || !password.value) {
    adminError.value = 'All fields are required'
    return
  }
  if (password.value !== confirmPassword.value) {
    adminError.value = 'Passwords do not match'
    return
  }
  if (password.value.length < 8) {
    adminError.value = 'Password must be at least 8 characters'
    return
  }

  isLoadingAdmin.value = true
  try {
    await client.post({
      url: '/v1/setup/initialize',
      body: {
        email: email.value,
        username: username.value,
        password: password.value,
      },
    })
    await refreshBootstrap()
    currentStep.value = 1
  } catch (err: any) {
    if (err.response?.status === 409) {
      adminError.value = 'System already initialized'
    } else {
      adminError.value = err.response?.data?.error || 'Setup failed'
    }
  } finally {
    isLoadingAdmin.value = false
  }
}

async function handleTmdbSubmit(e: Event) {
  e.preventDefault()
  tmdbError.value = null

  if (!tmdbKey.value) {
    tmdbError.value = 'API key is required'
    return
  }

  isLoadingTmdb.value = true
  try {
    await client.post({
      url: '/v1/setup/tmdb',
      body: { api_key: tmdbKey.value },
    })
    await refreshBootstrap()
    // After both steps complete, bootstrap returns initialized=true
    // and setupSteps becomes null — navigate directly to login.
    if (!appStore.needsSetup) {
      router.push('/login')
    }
  } catch (err: any) {
    tmdbError.value = err.response?.data?.error || 'Failed to validate TMDB key'
  } finally {
    isLoadingTmdb.value = false
  }
}

function handleContinue() {
  router.push('/login')
}
</script>

<template>
  <div class="flex min-h-svh flex-col items-center justify-center gap-6 p-6 md:p-10">
    <div class="flex w-full max-w-sm flex-col gap-6">
      <a href="#" class="flex items-center gap-2 self-center font-medium">
        <div
          class="bg-primary text-primary-foreground flex size-10 items-center justify-center rounded-md"
        >
          <Popcorn class="size-8" />
        </div>
        <div class="text-2xl font-semibold">Arrflix</div>
      </a>

      <!-- Stepper -->
      <Stepper :model-value="currentStep" class="w-full">
        <StepperItem
          v-for="item in steps"
          :key="item.step"
          :step="item.step"
          class="relative flex w-full flex-col items-center justify-center"
        >
          <StepperTrigger as="div" class="cursor-default">
            <StepperIndicator class="bg-muted">
              <component :is="item.icon" class="size-4" />
            </StepperIndicator>
          </StepperTrigger>
          <StepperSeparator
            v-if="item.step < steps.length - 1"
            class="absolute left-[calc(50%+20px)] right-[calc(-50%+10px)] top-5 block h-0.5 shrink-0 rounded-full bg-muted group-data-[state=completed]:bg-primary"
          />
          <StepperTitle class="text-xs font-medium mt-1">
            {{ item.label }}
          </StepperTitle>
        </StepperItem>
      </Stepper>

      <div :class="cn('flex flex-col gap-6', props.class)">
        <!-- Step 1: Admin Account -->
        <Card v-if="currentStep === 0">
          <CardHeader class="text-center">
            <CardTitle class="text-xl">Create Admin Account</CardTitle>
            <CardDescription>Set up your administrator credentials</CardDescription>
          </CardHeader>
          <CardContent>
            <!-- Already completed -->
            <div v-if="adminDone" class="flex flex-col items-center gap-4 py-4">
              <p class="text-muted-foreground text-sm text-center">
                Admin account has already been created.
              </p>
              <Button class="w-full" @click="currentStep = 1">
                Continue
              </Button>
            </div>

            <form v-else @submit="handleAdminSubmit">
              <FieldGroup>
                <Field v-if="adminError">
                  <FieldDescription class="text-destructive">
                    {{ adminError }}
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel for="email">Email</FieldLabel>
                  <Input
                    id="email"
                    v-model="email"
                    type="email"
                    placeholder="admin@example.com"
                    required
                    :disabled="isLoadingAdmin"
                  />
                </Field>
                <Field>
                  <FieldLabel for="username">Username</FieldLabel>
                  <Input
                    id="username"
                    v-model="username"
                    type="text"
                    placeholder="admin"
                    required
                    :disabled="isLoadingAdmin"
                  />
                </Field>
                <Field>
                  <FieldLabel for="password">Password</FieldLabel>
                  <Input
                    id="password"
                    v-model="password"
                    type="password"
                    placeholder="Minimum 8 characters"
                    required
                    :disabled="isLoadingAdmin"
                  />
                </Field>
                <Field>
                  <FieldLabel for="confirmPassword">Confirm Password</FieldLabel>
                  <Input
                    id="confirmPassword"
                    v-model="confirmPassword"
                    type="password"
                    placeholder="Re-enter password"
                    required
                    :disabled="isLoadingAdmin"
                  />
                </Field>
                <Field>
                  <Button type="submit" class="w-full" :disabled="isLoadingAdmin">
                    {{ isLoadingAdmin ? 'Creating account...' : 'Continue' }}
                  </Button>
                </Field>
              </FieldGroup>
            </form>
          </CardContent>
        </Card>

        <!-- Step 2: TMDB API Key -->
        <Card v-if="currentStep === 1">
          <CardHeader class="text-center">
            <CardTitle class="text-xl">Connect TMDB</CardTitle>
            <CardDescription>
              An API key is required for media metadata
            </CardDescription>
          </CardHeader>
          <CardContent>
            <!-- Already completed -->
            <div v-if="tmdbDone" class="flex flex-col items-center gap-4 py-4">
              <p class="text-muted-foreground text-sm text-center">
                TMDB API key has already been configured.
              </p>
              <Button class="w-full" @click="handleContinue">
                Continue to Login
              </Button>
            </div>

            <form v-else @submit="handleTmdbSubmit">
              <FieldGroup>
                <Field v-if="tmdbError">
                  <FieldDescription class="text-destructive">
                    {{ tmdbError }}
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel for="tmdb-key">API Key</FieldLabel>
                  <Input
                    id="tmdb-key"
                    v-model="tmdbKey"
                    type="password"
                    placeholder="Enter your TMDB API key"
                    required
                    :disabled="isLoadingTmdb"
                  />
                  <FieldDescription>
                    Get a free key at
                    <a
                      href="https://www.themoviedb.org/settings/api"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="underline"
                    >themoviedb.org</a>
                  </FieldDescription>
                </Field>
                <Field>
                  <Button type="submit" class="w-full" :disabled="isLoadingTmdb">
                    {{ isLoadingTmdb ? 'Validating...' : 'Save & Validate' }}
                  </Button>
                </Field>
              </FieldGroup>
            </form>
          </CardContent>
        </Card>
      </div>
    </div>
  </div>
</template>
