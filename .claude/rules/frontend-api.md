---
paths:
  - "web/src/**"
---

# Frontend API client conventions

The TypeScript client in `src/client/` is auto-generated from `backend/internal/http/docs/openapi.json` by `@hey-api/openapi-ts`. **Do not edit anything under `src/client/`** — it is overwritten on every `just web-genclient` / `just gen` run. These rules cover everything in `src/` that talks to the API.

The backend emits RFC 9457 problem-details errors. The generated client types each operation's `*Error` as `ProblemDetails`, which carries `{ status, title, detail, type, errors? }`. The whole point of these rules is to make sure the frontend actually consumes that typed surface instead of casting it away.

## Rules

1. **Always go through the generated SDK.**
   Never call `client.post(...)` / `client.get(...)` with a hardcoded URL. Those calls bypass the typed path params, body, and error responses, and they drift silently when routes change. If you need an operation, it has a generated function in `@/client/sdk.gen` or a generated TanStack helper in `@/client/@tanstack/vue-query.gen`.

2. **Stores use `throwOnError: true` + try/catch.**
   Pinia stores own multi-step sequences (login → setToken → fetchMe; bootstrap; etc.). One try/catch wraps the sequence. The thrown value is a `ProblemDetails` — unwrap with `problemMessage(err)` from `@/lib/api`. Use `isProblem(err)` when you need to branch on `status`.

3. **Component reads use `useQuery` with the generated `*Options` helpers.**
   `useQuery(xOptions(...))`. The generated helper sets `throwOnError: true` internally, so the error surfaces as `isError` / `error`. No raw `await` inside `queryFn`; no `{ data }` destructure with `data!`.

4. **Component mutations use `useMutation` with the generated `*Mutation` helpers, and define `onSuccess` / `onError` at the mutation declaration.**
   The `err` parameter inside `onError` is typed as the operation's `ProblemDetails` — no cast needed. Side effects (toast, dialog close, navigation) belong in the callbacks. `mutation.isPending.value` is the single source of truth for disabling buttons.

5. **`mutateAsync` + try/catch is allowed only when you need linear sequencing after the mutation.**
   E.g., "save, then conditionally trigger a second mutation, then navigate." If `onSuccess` is enough, use `onSuccess`. Don't reach for `mutateAsync` just out of habit.

6. **All user-visible error strings flow through `problemMessage(err, fallback)`.**
   Do not write `err instanceof Error ? err.message : ...` — the SDK throws a parsed `ProblemDetails` object, which is not an `Error` instance, so that branch never fires and users always see the fallback. Do not invent error shapes (`err as { response?: { data?: { error?: string } } }`, `err as { message?: string; data?: { error?: string } }`, `err as { body?: { error?: string } }`). Those fields do not exist on the thrown value.

7. **401 is handled globally — don't catch it per-call.**
   `src/main.ts` registers a response interceptor on the client that logs out and redirects to `/login` on any 401. Per-call code should treat 401 as "already handled, the user is being redirected" and not branch on it. The same goes for the response error parser inside `client.gen.ts` — it already parses JSON bodies into `ProblemDetails`, so catches receive the parsed object, not a raw `Response`.

8. **After a mutation, invalidate dependent queries via `queryClient.invalidateQueries`, keyed by the generated `*QueryKey()` helper. Do not use `refetch()` for cache coherence.**
   `invalidateQueries` marks data stale across every observer of that key — mounted queries refetch immediately, unmounted ones refetch on next mount. It works identically when one component reads the query and correctly when many do, so we always use it regardless of current readers. `refetch()` is reserved for **user-driven** "redo this fetch" actions (a "Try Again" button after an error). `Library.vue:49` is the canonical example. Don't reach for `refetch()` just because the query handle exposes it.

## Patterns

### The `problemMessage` helper

```ts
// src/lib/api.ts
import type { ProblemDetails } from '@/client/types.gen'

export function isProblem(err: unknown): err is ProblemDetails {
  return typeof err === 'object' && err !== null && 'status' in err && 'title' in err
}

export function problemMessage(err: unknown, fallback = 'Unexpected error'): string {
  if (isProblem(err)) return err.detail ?? err.title ?? fallback
  if (err instanceof Error) return err.message
  return fallback
}
```

One place to evolve if the backend ever adds richer fields.

### Store call (single step)

```ts
import { settingsList } from '@/client/sdk.gen'
import { problemMessage } from '@/lib/api'

async function loadSettings() {
  isLoading.value = true
  try {
    const res = await settingsList<true>({ throwOnError: true })
    settings.value = res.data
  } catch (err) {
    error.value = problemMessage(err, 'Failed to load settings')
  } finally {
    isLoading.value = false
  }
}
```

### Store call (multi-step sequence)

```ts
import { authLogin } from '@/client/sdk.gen'
import { problemMessage } from '@/lib/api'

async function loginWithPassword(login: string, password: string): Promise<boolean> {
  isLoading.value = true
  errorMessage.value = null
  try {
    const res = await authLogin<true>({ throwOnError: true, body: { login, password } })
    setToken(res.data.token)
    await fetchMe()
    return true
  } catch (err) {
    errorMessage.value = problemMessage(err, 'Invalid credentials')
    return false
  } finally {
    isLoading.value = false
  }
}
```

### Store call (branching on status)

```ts
import { setupInitialize } from '@/client/sdk.gen'
import { isProblem, problemMessage } from '@/lib/api'

try {
  await setupInitialize<true>({ throwOnError: true, body })
  await refreshBootstrap()
} catch (err) {
  if (isProblem(err) && err.status === 409) {
    adminError.value = 'System already initialized'
  } else {
    adminError.value = problemMessage(err, 'Setup failed')
  }
}
```

### Component read

```ts
import { useQuery } from '@tanstack/vue-query'
import { mediaGetMovieOptions } from '@/client/@tanstack/vue-query.gen'
import { problemMessage } from '@/lib/api'

const { isLoading, isError, data, error } = useQuery(
  computed(() => mediaGetMovieOptions({ path: { id: id.value } })),
)

const errorMessage = computed(() => problemMessage(error.value, 'Failed to load movie'))
```

```vue
<div v-if="isError" class="text-destructive">{{ errorMessage }}</div>
```

### Component mutation (preferred — callbacks at declaration)

```ts
import { useMutation } from '@tanstack/vue-query'
import { invitesCreateMutation } from '@/client/@tanstack/vue-query.gen'
import { toast } from 'vue-sonner'
import { problemMessage } from '@/lib/api'

const createInvite = useMutation({
  ...invitesCreateMutation(),
  onSuccess: () => {
    toast.success('Invite sent')
    dialogRef.value.close({ saved: true })
  },
  onError: (err) => {
    // err is typed as InvitesCreateError = ProblemDetails
    error.value = problemMessage(err, 'Failed to create invite')
  },
})

function handleSave() {
  if (!email.value) {
    error.value = 'Email is required'
    return
  }
  createInvite.mutate({ body: { email: email.value } })
}
```

```vue
<Button :disabled="createInvite.isPending.value" @click="handleSave">
  {{ createInvite.isPending.value ? 'Sending...' : 'Send Invite' }}
</Button>
```

### Component mutation (escape hatch — `mutateAsync` for linear sequencing)

```ts
import { useMutation } from '@tanstack/vue-query'
import { librariesCreateMutation, librariesScanMutation } from '@/client/@tanstack/vue-query.gen'
import { problemMessage } from '@/lib/api'

const createLibrary = useMutation(librariesCreateMutation())
const scanLibrary = useMutation(librariesScanMutation())

async function handleSaveAndScan() {
  try {
    const lib = await createLibrary.mutateAsync({ body: form.value })
    await scanLibrary.mutateAsync({ path: { id: lib.id } })
    router.push(`/library/${lib.id}`)
  } catch (err) {
    error.value = problemMessage(err, 'Failed to create library')
  }
}
```

Use this shape only when steps actually depend on each other. If both mutations are independent, give them their own `onError` callbacks.

### Invalidation after mutation

```ts
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { librariesListQueryKey, librariesDeleteMutation } from '@/client/@tanstack/vue-query.gen'
import { problemMessage } from '@/lib/api'

const queryClient = useQueryClient()

function invalidateLibraries() {
  queryClient.invalidateQueries({ queryKey: librariesListQueryKey() })
}

const deleteLibrary = useMutation({
  ...librariesDeleteMutation(),
  onSuccess: invalidateLibraries,
  onError: (err) => {
    error.value = problemMessage(err, 'Failed to delete library')
  },
})
```

The local helper is optional but pays off when the same invalidation fires from multiple sites (mutation success, modal close, SSE event). For a single call site, inline it:

```ts
onSuccess: () => queryClient.invalidateQueries({ queryKey: librariesListQueryKey() }),
```

For invalidating across all variants of a parameterized query (e.g. all `mediaGet({ path: { id } })` regardless of `id`), use the partial-match form against the operation ID:

```ts
queryClient.invalidateQueries({ queryKey: [{ _id: 'mediaGet' }] })
```

The generated key shape is `[{ _id: '<operationId>', baseUrl, path?, query?, body?, headers? }]`, so a queryKey containing only `_id` partial-matches all variants.

## Anti-patterns

| Don't                                                    | Why                                                                                                   | Use instead                                                                    |
| -------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| `client.post({ url: '/v1/indexer/test', body })`         | Bypasses generated types; URL drifts on backend rename                                                | The generated SDK function (`indexersTestUnsaved`, etc.)                       |
| `const { data } = await sdkFn({ ... })` then `data!`     | Silently drops the error channel; runtime null-deref on server failure                                | `await sdkFn<true>({ throwOnError: true, ... })`, or wrap in `useQuery`        |
| `err instanceof Error ? err.message : 'fallback'`        | Thrown ProblemDetails isn't an `Error` — fallback always fires                                        | `problemMessage(err, 'fallback')`                                              |
| `err as { message?: string; data?: { error?: string } }` | Invented shape; fields don't exist on the thrown value                                                | `problemMessage(err, ...)` or `isProblem(err)` for branching                   |
| `err as { response?: { data?: ... } }` (Axios-style)     | We don't use Axios; the field doesn't exist                                                           | `problemMessage(err, ...)`                                                     |
| Catching 401 per-call                                    | Already handled globally in `main.ts`                                                                 | Let it propagate; the interceptor logs out and redirects                       |
| Manually tracking `isSaving` next to a mutation          | Duplicates `mutation.isPending.value`                                                                 | Bind buttons/spinners to `mutation.isPending.value`                            |
| `onSuccess: () => refetch()` after a mutation            | Only updates the one query the component holds a handle to; misses every other reader of the same key | `queryClient.invalidateQueries({ queryKey: xQueryKey() })` (Rule 8)            |
| Hand-rolling query keys like `['libraries']`             | Drifts from the generated key shape; partial matches break                                            | Use the generated `*QueryKey()` helper from `@/client/@tanstack/vue-query.gen` |
