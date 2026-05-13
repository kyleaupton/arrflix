<script setup lang="ts">
import { ref, type HTMLAttributes } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { Popcorn } from 'lucide-vue-next'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldSeparator,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'

const props = defineProps<{
  class?: HTMLAttributes['class']
}>()

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const login = ref('')
const password = ref('')

async function handleSubmit(e: Event) {
  e.preventDefault()
  const success = await authStore.loginWithPassword(login.value, password.value)
  if (success) {
    const redirectParam = route.query.redirect
    const redirectValue = Array.isArray(redirectParam) ? redirectParam[0] : redirectParam
    const target =
      typeof redirectValue === 'string' && redirectValue.startsWith('/') ? redirectValue : '/'
    router.replace(target)
  }
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

      <div :class="cn('flex flex-col gap-6', props.class)">
        <Card>
          <CardHeader class="text-center">
            <CardTitle class="text-xl"> Welcome back </CardTitle>
            <CardDescription> Login with your Plex account? </CardDescription>
          </CardHeader>
          <CardContent>
            <form @submit="handleSubmit">
              <FieldGroup>
                <Field>
                  <Button variant="outline" type="button" @click="authStore.startPlexSso()">
                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
                      <path
                        d="M11.643 0H4.68l7.679 12L4.68 24h6.963l7.677-12-7.677-12"
                        fill="currentColor"
                      />
                    </svg>
                    Login with Plex
                  </Button>
                </Field>
                <FieldSeparator class="*:data-[slot=field-separator-content]:bg-card">
                  Or continue with
                </FieldSeparator>
                <Field v-if="authStore.errorMessage">
                  <FieldDescription class="text-destructive">
                    {{ authStore.errorMessage }}
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel for="login"> Email or Username </FieldLabel>
                  <Input
                    id="login"
                    v-model="login"
                    type="text"
                    placeholder="email or username"
                    required
                    :disabled="authStore.isLoading"
                  />
                </Field>
                <Field>
                  <div class="flex items-center">
                    <FieldLabel for="password"> Password </FieldLabel>
                    <a href="#" class="ml-auto text-sm underline-offset-4 hover:underline">
                      Forgot your password?
                    </a>
                  </div>
                  <Input
                    id="password"
                    v-model="password"
                    type="password"
                    required
                    :disabled="authStore.isLoading"
                  />
                </Field>
                <Field>
                  <Button type="submit" :disabled="authStore.isLoading">
                    {{ authStore.isLoading ? 'Logging in...' : 'Login' }}
                  </Button>
                  <FieldDescription class="text-center">
                    Don't have an account?
                    <router-link to="/signup"> Sign up </router-link>
                  </FieldDescription>
                </Field>
              </FieldGroup>
            </form>
          </CardContent>
        </Card>
      </div>
    </div>
  </div>
</template>
