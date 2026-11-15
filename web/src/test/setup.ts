import '@testing-library/jest-dom/vitest'
import { afterAll, afterEach, beforeAll } from 'vitest'
import { cleanup } from '@testing-library/vue'
import { client } from '@/client/client.gen'
import { server } from './server'

// The app calls the API by relative URL, which a browser resolves against the
// page and Node's fetch can't. Anchor it to jsdom's origin; the generated MSW
// handlers match any origin.
client.setConfig({ baseUrl: window.location.origin })

// A request no test declared is a test bug. MSW's own 'error' mode only rejects
// the fetch, which TanStack Query swallows into an error state the test may
// never look at, so record the request and fail the test after it runs.
const unhandled: string[] = []
beforeAll(() =>
  server.listen({
    onUnhandledRequest: (req) => {
      unhandled.push(`${req.method} ${new URL(req.url).pathname}`)
    },
  }),
)
afterEach(() => {
  cleanup()
  server.resetHandlers()
  const missed = unhandled.splice(0)
  if (missed.length) {
    throw new Error(
      `Request(s) with no MSW handler; declare them with server.use(...):\n  ${missed.join('\n  ')}`,
    )
  }
})
afterAll(() => server.close())
