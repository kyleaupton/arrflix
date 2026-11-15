import { setupServer } from 'msw/node'

// One MSW server for the whole suite. Tests declare the responses they need
// with `server.use(...)`, preferably via the generated handlers in
// `@/client/msw.gen`; setup.ts resets them after each test.
export const server = setupServer()
