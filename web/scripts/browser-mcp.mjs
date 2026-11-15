#!/usr/bin/env node
// Starts the Playwright MCP server (`@playwright/mcp`) already signed in to the
// local Arrflix, so an agent can look at the UI it is building.
//
//   node web/scripts/browser-mcp.mjs desktop   Chromium at a laptop viewport
//   node web/scripts/browser-mcp.mjs mobile    WebKit emulating an iPhone
//
// Runs inside the dev container or a factory sandbox, where the app is served
// at http://localhost:8484 and the dev image provides `playwright-mcp` and its
// browsers. Speaks MCP over stdio; everything this script logs goes to stderr.
//
// Sign-in mints a fresh session on every start: it logs in through the API and
// hands the resulting refresh cookie to the browser. A saved session can't be
// reused, because refresh tokens rotate and presenting a spent one counts as
// reuse, which revokes the session.
//
// Env: ARRFLIX_BROWSER_URL (default http://localhost:8484),
// ARRFLIX_BROWSER_LOGIN / ARRFLIX_BROWSER_PASSWORD (default: the seeded admin).

import { spawn } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const PROFILES = {
  desktop: ['--browser', 'chromium', '--viewport-size', '1440x900', '--no-sandbox'],
  mobile: ['--browser', 'webkit', '--device', 'iPhone 15'],
}

const profile = process.argv[2]
if (!(profile in PROFILES)) {
  console.error(`usage: browser-mcp.mjs ${Object.keys(PROFILES).join('|')}`)
  process.exit(2)
}

const appUrl = new URL(process.env.ARRFLIX_BROWSER_URL ?? 'http://localhost:8484')
const login = process.env.ARRFLIX_BROWSER_LOGIN ?? 'admin@example.com'
const password = process.env.ARRFLIX_BROWSER_PASSWORD ?? 'password'
const log = (msg) => console.error(`browser-mcp: ${msg}`)

// Outside the checkout: in a factory sandbox the checkout is the run's clone,
// and anything left in it would be committed.
const workDir = mkdtempSync(join(tmpdir(), `arrflix-browser-${profile}-`))

/** Logs in, retrying while the app starts up. Returns the refresh cookie's Set-Cookie line. */
async function signIn() {
  const deadline = Date.now() + 60_000
  let lastError = ''
  while (Date.now() < deadline) {
    try {
      const res = await fetch(new URL('/api/v1/auth/login', appUrl), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ login, password }),
      })
      if (res.ok) {
        const cookie = res.headers.getSetCookie().find((c) => c.startsWith('arrflix_rt='))
        if (cookie) return cookie
        throw new Error('login succeeded but set no refresh cookie')
      }
      // 4xx is an answer (wrong credentials, no such user); 5xx is the stack starting.
      if (res.status < 500) throw new Error(`login as ${login} failed: HTTP ${res.status}`)
      lastError = `HTTP ${res.status}`
    } catch (err) {
      if (!(err instanceof TypeError)) throw err // TypeError is fetch's "couldn't connect"
      lastError = err.cause?.code ?? err.message
    }
    await new Promise((r) => setTimeout(r, 2000))
  }
  throw new Error(`app at ${appUrl.origin} not reachable after 60s (${lastError})`)
}

/** Converts a Set-Cookie line into Playwright's storage-state cookie shape. */
function toStorageCookie(setCookie) {
  const [pair, ...attrs] = setCookie.split(';').map((s) => s.trim())
  const eq = pair.indexOf('=')
  const cookie = {
    name: pair.slice(0, eq),
    value: pair.slice(eq + 1),
    domain: appUrl.hostname,
    path: '/',
    expires: -1,
    httpOnly: false,
    secure: false,
    sameSite: 'Lax',
  }
  for (const attr of attrs) {
    const [key, val = ''] = attr.split('=')
    switch (key.toLowerCase()) {
      case 'path':
        cookie.path = val
        break
      case 'max-age':
        cookie.expires = Math.floor(Date.now() / 1000) + Number(val)
        break
      case 'httponly':
        cookie.httpOnly = true
        break
      case 'secure':
        cookie.secure = true
        break
      case 'samesite':
        cookie.sameSite = val.charAt(0).toUpperCase() + val.slice(1).toLowerCase()
        break
    }
  }
  return cookie
}

const args = [
  ...PROFILES[profile],
  '--headless',
  '--isolated',
  // The dev build registers the PWA service worker, which would serve cached
  // pages over fresh Vite output.
  '--block-service-workers',
  '--output-dir',
  join(workDir, 'output'),
]

try {
  const cookie = toStorageCookie(await signIn())
  const statePath = join(workDir, 'storage-state.json')
  writeFileSync(statePath, JSON.stringify({ cookies: [cookie], origins: [] }))
  args.push('--storage-state', statePath)
  log(`signed in as ${login}`)
} catch (err) {
  // Still start: the browser is useful signed out, and the agent can log in by hand.
  log(`starting signed out: ${err.message}`)
}

const child = spawn('playwright-mcp', args, { stdio: 'inherit' })
const cleanup = () => rmSync(workDir, { recursive: true, force: true })
for (const signal of ['SIGINT', 'SIGTERM', 'SIGHUP']) {
  process.on(signal, () => child.kill(signal))
}
child.on('error', (err) => {
  log(`could not start playwright-mcp: ${err.message}`)
  cleanup()
  process.exit(1)
})
child.on('exit', (code, signal) => {
  cleanup()
  process.exit(code ?? (signal ? 1 : 0))
})
