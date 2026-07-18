<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { toast } from 'vue-sonner'
import { Eye, EyeOff, Send } from 'lucide-vue-next'
import {
  emailProviderGetOptions,
  emailProviderGetQueryKey,
  emailProviderSaveMutation,
  emailProviderTestMutation,
} from '@/client/@tanstack/vue-query.gen'
import type { EmailProviderWriteBody } from '@/client/types.gen'
import { problemMessage } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

const auth = useAuthStore()
const queryClient = useQueryClient()

// --- Form state ---
type SecurityMode = 'starttls' | 'implicit_tls' | 'none'
const form = reactive({
  provider: 'smtp' as const,
  fromAddress: '',
  fromName: '',
  replyTo: '',
  host: '',
  port: 587,
  security: 'starttls' as SecurityMode,
  auth: true,
  username: '',
  password: '',
  skipTlsVerify: false,
  enabled: true,
})

const showPassword = ref(false)
const saveError = ref<string | null>(null)
const testError = ref<string | null>(null)
const preset = ref('custom')
const testTo = ref('')

// --- Static SMTP presets (host/port/TLS prefills). SMTP is the ceiling: every
// provider worth having hands out relay creds, so one preset list covers them. ---
interface Preset {
  label: string
  value: string
  host?: string
  port?: number
  security?: SecurityMode
}
const PRESETS: Preset[] = [
  { label: 'Custom', value: 'custom' },
  { label: 'Gmail', value: 'gmail', host: 'smtp.gmail.com', port: 587, security: 'starttls' },
  {
    label: 'Outlook / Microsoft 365',
    value: 'outlook',
    host: 'smtp.office365.com',
    port: 587,
    security: 'starttls',
  },
  {
    label: 'Fastmail',
    value: 'fastmail',
    host: 'smtp.fastmail.com',
    port: 465,
    security: 'implicit_tls',
  },
  {
    label: 'Amazon SES (us-east-1)',
    value: 'ses',
    host: 'email-smtp.us-east-1.amazonaws.com',
    port: 587,
    security: 'starttls',
  },
  { label: 'Mailgun', value: 'mailgun', host: 'smtp.mailgun.org', port: 587, security: 'starttls' },
  {
    label: 'Postmark',
    value: 'postmark',
    host: 'smtp.postmarkapp.com',
    port: 587,
    security: 'starttls',
  },
  {
    label: 'SendGrid',
    value: 'sendgrid',
    host: 'smtp.sendgrid.net',
    port: 587,
    security: 'starttls',
  },
  { label: 'Brevo', value: 'brevo', host: 'smtp-relay.brevo.com', port: 587, security: 'starttls' },
  {
    label: 'Resend',
    value: 'resend',
    host: 'smtp.resend.com',
    port: 465,
    security: 'implicit_tls',
  },
]

const SECURITY_OPTIONS: { label: string; value: SecurityMode }[] = [
  { label: 'STARTTLS (typically 587)', value: 'starttls' },
  { label: 'Implicit TLS (typically 465)', value: 'implicit_tls' },
  { label: 'None (unencrypted)', value: 'none' },
]

function applyPreset(value: string) {
  preset.value = value
  const p = PRESETS.find((x) => x.value === value)
  if (!p || value === 'custom') return
  if (p.host) form.host = p.host
  if (p.port) form.port = p.port
  if (p.security) form.security = p.security
}

// --- Load current config ---
const query = useQuery({ ...emailProviderGetOptions(), staleTime: 30_000 })
const isLoading = computed(() => query.isLoading.value)
const configured = computed(() => query.data.value?.configured ?? false)
const hasPassword = computed(() => query.data.value?.hasPassword ?? false)

watch(
  () => query.data.value,
  (data) => {
    if (!data || !data.configured) {
      testTo.value = testTo.value || auth.user?.email || ''
      return
    }
    form.fromAddress = data.fromAddress ?? ''
    form.fromName = data.fromName ?? ''
    form.replyTo = data.replyTo ?? ''
    form.host = data.host ?? ''
    form.port = data.port ?? 587
    form.security = (data.security as SecurityMode) ?? 'starttls'
    form.auth = data.auth ?? true
    form.username = data.username ?? ''
    form.password = '' // write-only — never populated
    form.skipTlsVerify = data.skipTlsVerify ?? false
    form.enabled = data.enabled ?? true
    testTo.value = testTo.value || auth.user?.email || ''
  },
  { immediate: true },
)

// --- Save ---
const saveM = useMutation({
  ...emailProviderSaveMutation(),
  onSuccess: () => {
    saveError.value = null
    queryClient.invalidateQueries({ queryKey: emailProviderGetQueryKey() })
    toast.success('Email settings saved')
  },
  onError: (err) => {
    saveError.value = problemMessage(err, 'Failed to save email settings')
  },
})

function handleSave() {
  saveError.value = null
  const body: EmailProviderWriteBody = {
    provider: 'smtp',
    fromAddress: form.fromAddress.trim(),
    fromName: form.fromName.trim() || undefined,
    replyTo: form.replyTo.trim() || undefined,
    host: form.host.trim(),
    port: form.port,
    security: form.security,
    auth: form.auth,
    username: form.auth ? form.username.trim() || undefined : undefined,
    skipTlsVerify: form.skipTlsVerify,
    enabled: form.enabled,
  }
  // Write-only password: only send when the admin typed one; empty preserves the
  // stored secret (see the GET contract — password is never echoed back).
  if (form.password !== '') body.password = form.password
  saveM.mutate({ body })
}

// --- Test (uses the SAVED config, so save before testing) ---
const testM = useMutation({
  ...emailProviderTestMutation(),
  onSuccess: () => {
    testError.value = null
    toast.success(`Test email sent to ${testTo.value}`)
  },
  onError: (err) => {
    // The backend returns the verbatim SMTP failure (502); surface it as-is so
    // the admin sees the real reason (auth, TLS, connection refused, …).
    testError.value = problemMessage(err, 'Test email failed')
  },
})

function handleTest() {
  testError.value = null
  if (!testTo.value.trim()) {
    testError.value = 'Enter a recipient address to send a test.'
    return
  }
  testM.mutate({ body: { to: testTo.value.trim() } })
}

const isSaving = computed(() => saveM.isPending.value)
const isTesting = computed(() => testM.isPending.value)
</script>

<template>
  <div class="mx-auto flex max-w-2xl flex-col gap-6">
    <header class="flex flex-col gap-1">
      <h1 class="text-2xl font-semibold">Email</h1>
      <p class="text-muted-foreground">
        Configure an SMTP relay so Arrflix can send email notifications.
      </p>
    </header>

    <div v-if="isLoading" class="space-y-4">
      <Skeleton class="h-64 w-full rounded-xl" />
      <Skeleton class="h-40 w-full rounded-xl" />
    </div>

    <template v-else>
      <!-- Provider config -->
      <Card>
        <CardHeader>
          <CardTitle>SMTP server</CardTitle>
          <CardDescription>
            Any provider that gives you SMTP credentials works — Gmail, Fastmail, SES, Mailgun,
            Postmark, and more. Pick a preset to prefill the server, or enter it manually.
          </CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div
            v-if="saveError"
            class="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
          >
            {{ saveError }}
          </div>

          <div class="flex flex-col gap-2">
            <Label>Preset</Label>
            <Select :model-value="preset" @update:model-value="(v) => applyPreset(v as string)">
              <SelectTrigger class="w-full">
                <SelectValue placeholder="Choose a provider" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="p in PRESETS" :key="p.value" :value="p.value">
                  {{ p.label }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="grid gap-4 sm:grid-cols-[2fr_1fr]">
            <div class="flex flex-col gap-2">
              <Label for="email-host">Host</Label>
              <Input id="email-host" v-model="form.host" placeholder="smtp.example.com" />
            </div>
            <div class="flex flex-col gap-2">
              <Label for="email-port">Port</Label>
              <Input id="email-port" v-model.number="form.port" type="number" min="1" max="65535" />
            </div>
          </div>

          <div class="flex flex-col gap-2">
            <Label for="email-security">Security</Label>
            <Select v-model="form.security">
              <SelectTrigger id="email-security" class="w-full">
                <SelectValue placeholder="Select security mode" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="s in SECURITY_OPTIONS" :key="s.value" :value="s.value">
                  {{ s.label }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div class="flex items-center justify-between gap-4">
            <div>
              <Label for="email-auth">Requires authentication</Label>
              <p class="text-xs text-muted-foreground">
                Most relays require a username and password.
              </p>
            </div>
            <Switch id="email-auth" v-model="form.auth" />
          </div>

          <template v-if="form.auth">
            <div class="flex flex-col gap-2">
              <Label for="email-username">Username</Label>
              <Input id="email-username" v-model="form.username" autocomplete="off" />
            </div>
            <div class="flex flex-col gap-2">
              <Label for="email-password">Password</Label>
              <div class="relative">
                <Input
                  id="email-password"
                  v-model="form.password"
                  :type="showPassword ? 'text' : 'password'"
                  autocomplete="new-password"
                  :placeholder="hasPassword ? 'Leave blank to keep current' : ''"
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
          </template>

          <div class="flex items-center justify-between gap-4">
            <div>
              <Label for="email-skip-tls">Skip TLS verification</Label>
              <p class="text-xs text-muted-foreground">
                Only for relays with self-signed certificates.
              </p>
            </div>
            <Switch id="email-skip-tls" v-model="form.skipTlsVerify" />
          </div>
        </CardContent>
      </Card>

      <!-- Sender identity -->
      <Card>
        <CardHeader>
          <CardTitle>Sender</CardTitle>
          <CardDescription
            >How notification emails appear in the recipient's inbox.</CardDescription
          >
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-2">
            <Label for="email-from">From address</Label>
            <Input id="email-from" v-model="form.fromAddress" placeholder="arrflix@example.com" />
          </div>
          <div class="grid gap-4 sm:grid-cols-2">
            <div class="flex flex-col gap-2">
              <Label for="email-from-name">From name (optional)</Label>
              <Input id="email-from-name" v-model="form.fromName" placeholder="Arrflix" />
            </div>
            <div class="flex flex-col gap-2">
              <Label for="email-reply-to">Reply-to (optional)</Label>
              <Input id="email-reply-to" v-model="form.replyTo" placeholder="noreply@example.com" />
            </div>
          </div>
          <div class="flex items-center justify-between gap-4">
            <div>
              <Label for="email-enabled">Enabled</Label>
              <p class="text-xs text-muted-foreground">
                Turn off to stop all email delivery without losing this configuration.
              </p>
            </div>
            <Switch id="email-enabled" v-model="form.enabled" />
          </div>
        </CardContent>
      </Card>

      <div class="flex justify-end">
        <Button :disabled="isSaving" @click="handleSave">
          {{ isSaving ? 'Saving…' : 'Save' }}
        </Button>
      </div>

      <!-- Test send -->
      <Card>
        <CardHeader>
          <CardTitle>Send a test email</CardTitle>
          <CardDescription>
            Sends a test using your <strong>saved</strong> settings. Save any changes above first.
          </CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-3">
          <div
            v-if="testError"
            class="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
          >
            {{ testError }}
          </div>
          <div class="flex flex-col gap-2 sm:flex-row sm:items-end">
            <div class="flex flex-1 flex-col gap-2">
              <Label for="email-test-to">Recipient</Label>
              <Input
                id="email-test-to"
                v-model="testTo"
                type="email"
                placeholder="you@example.com"
              />
            </div>
            <Button
              variant="outline"
              :disabled="!configured || isTesting"
              :title="!configured ? 'Save your email settings first' : undefined"
              @click="handleTest"
            >
              <Send class="mr-2 size-4" />
              {{ isTesting ? 'Sending…' : 'Send test' }}
            </Button>
          </div>
          <p v-if="!configured" class="text-xs text-muted-foreground">
            Save your settings to enable the test.
          </p>
        </CardContent>
      </Card>
    </template>
  </div>
</template>
