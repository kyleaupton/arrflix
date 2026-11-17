import { defineComponent, h, type Component } from 'vue'
import { render } from '@testing-library/vue'
import { createPinia, setActivePinia } from 'pinia'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createMemoryHistory, createRouter } from 'vue-router'
import DialogContainer from '@/components/DialogContainer.vue'
import { installRealtime } from '@/realtime/bindings'
import { useAuthStore } from '@/stores/auth'
import { useDialogStore } from '@/stores/dialog'

export interface Viewer {
  sub: string
  permissions: string[]
}

/** A requester: may view, create and withdraw their own requests. */
export const requester: Viewer = {
  sub: '00000000-0000-0000-0000-000000000001',
  permissions: ['requests.view.own', 'requests.cancel.own', 'requests.create:movie:hd'],
}

/**
 * Renders a component the way the app hosts it: fresh Pinia and TanStack Query
 * instances, a router, the signed-in viewer, and the dialog container that
 * `useModal()` dialogs render into. Every API call goes through MSW, so declare
 * the responses the component needs with `server.use(...)` first.
 *
 * `realtime` installs the app's realtime bindings on this render's query
 * client, as `main.ts` does. Pair it with `@/test/realtime` to deliver events.
 * The bindings register on the realtime module's listener registry, which lives
 * for the whole test file, so use it in at most one test per file.
 */
export function renderWithApp(
  component: Component,
  options: { props?: Record<string, unknown>; viewer?: Viewer; realtime?: boolean } = {},
) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const viewer = options.viewer ?? requester
  const auth = useAuthStore()
  auth.user = { sub: viewer.sub, roles: [], permissions: viewer.permissions }

  // The dialog list is module state shared by every test in the file.
  useDialogStore().closeAll()

  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  if (options.realtime) installRealtime(queryClient)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { render: () => null } }],
  })

  const Host = defineComponent({
    render: () => [h(component, options.props), h(DialogContainer)],
  })

  return render(Host, {
    global: { plugins: [pinia, [VueQueryPlugin, { queryClient }], router] },
  })
}
