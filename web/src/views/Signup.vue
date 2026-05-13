<script setup lang="ts">
import { ref, type HTMLAttributes } from 'vue'
import { useRouter } from 'vue-router'
import { Popcorn } from 'lucide-vue-next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { client } from '@/client/client.gen'

const props = defineProps<{
  class?: HTMLAttributes['class']
}>()

const router = useRouter()

const email = ref('')
const username = ref('')
const password = ref('')
const confirmPassword = ref('')
const isLoading = ref(false)
const errorMessage = ref<string | null>(null)

async function handleSubmit(e: Event) {
  e.preventDefault()
  errorMessage.value = null

  // Validation
  if (!email.value || !username.value || !password.value) {
    errorMessage.value = 'All fields are required'
    return
  }
  if (password.value !== confirmPassword.value) {
    errorMessage.value = 'Passwords do not match'
    return
  }
  if (password.value.length < 8) {
    errorMessage.value = 'Password must be at least 8 characters'
    return
  }

  isLoading.value = true

  try {
    await client.post({
      url: '/v1/auth/signup',
      body: {
        email: email.value,
        username: username.value,
        password: password.value,
      },
    })
    // Success - redirect to login
    router.push('/login')
  } catch (err: any) {
    errorMessage.value = err.response?.data?.error || err.message || 'Signup failed'
  } finally {
    isLoading.value = false
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
            <CardTitle class="text-xl"> Create Account </CardTitle>
            <CardDescription> Sign up for an account </CardDescription>
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
                  <FieldLabel for="email"> Email </FieldLabel>
                  <Input
                    id="email"
                    v-model="email"
                    type="email"
                    placeholder="you@example.com"
                    required
                    :disabled="isLoading"
                  />
                </Field>
                <Field>
                  <FieldLabel for="username"> Username </FieldLabel>
                  <Input
                    id="username"
                    v-model="username"
                    type="text"
                    placeholder="username"
                    required
                    :disabled="isLoading"
                  />
                </Field>
                <Field>
                  <FieldLabel for="password"> Password </FieldLabel>
                  <Input
                    id="password"
                    v-model="password"
                    type="password"
                    placeholder="Minimum 8 characters"
                    required
                    :disabled="isLoading"
                  />
                </Field>
                <Field>
                  <FieldLabel for="confirmPassword"> Confirm Password </FieldLabel>
                  <Input
                    id="confirmPassword"
                    v-model="confirmPassword"
                    type="password"
                    placeholder="Re-enter password"
                    required
                    :disabled="isLoading"
                  />
                </Field>
                <Field>
                  <Button type="submit" :disabled="isLoading">
                    {{ isLoading ? 'Creating account...' : 'Create Account' }}
                  </Button>
                  <FieldDescription class="text-center">
                    Already have an account?
                    <router-link to="/login"> Login </router-link>
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
