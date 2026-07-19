<script setup lang="ts">
import { computed, ref, type HTMLAttributes } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Popcorn } from 'lucide-vue-next'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'

const props = defineProps<{
  class?: HTMLAttributes['class']
}>()

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

// The token is bound to the link; the invitee never types their email — it comes
// from the invite. A missing token means the link was mangled or hand-edited.
const token = computed(() => {
  const t = route.query.token
  return typeof t === 'string' ? t : ''
})

const username = ref('')
const password = ref('')
const confirmPassword = ref('')
const localError = ref<string | null>(null)

const errorMessage = computed(() => localError.value ?? auth.errorMessage)

async function handleSubmit(e: Event) {
  e.preventDefault()
  localError.value = null

  if (!username.value || !password.value) {
    localError.value = 'Username and password are required'
    return
  }
  if (password.value !== confirmPassword.value) {
    localError.value = 'Passwords do not match'
    return
  }
  if (password.value.length < 8) {
    localError.value = 'Password must be at least 8 characters'
    return
  }

  const ok = await auth.acceptInvite(token.value, username.value, password.value)
  if (ok) {
    // Accept issues a session, so we land logged-in — go straight to the app.
    router.push('/')
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
        <Card v-if="!token">
          <CardHeader class="text-center">
            <CardTitle class="text-xl">Invalid invite link</CardTitle>
            <CardDescription>
              This link is missing its token. Ask whoever invited you to send a fresh link.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <router-link to="/login">
              <Button variant="outline" class="w-full">Go to login</Button>
            </router-link>
          </CardContent>
        </Card>

        <Card v-else>
          <CardHeader class="text-center">
            <CardTitle class="text-xl">Accept your invite</CardTitle>
            <CardDescription>Choose a username and password to finish setting up.</CardDescription>
          </CardHeader>
          <CardContent>
            <form @submit="handleSubmit">
              <FieldGroup>
                <Field v-if="errorMessage">
                  <FieldDescription class="text-destructive">
                    {{ errorMessage }}
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel for="username">Username</FieldLabel>
                  <Input
                    id="username"
                    v-model="username"
                    type="text"
                    placeholder="username"
                    required
                    :disabled="auth.isLoading"
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
                    :disabled="auth.isLoading"
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
                    :disabled="auth.isLoading"
                  />
                </Field>
                <Field>
                  <Button type="submit" :disabled="auth.isLoading">
                    {{ auth.isLoading ? 'Setting up...' : 'Accept invite' }}
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
