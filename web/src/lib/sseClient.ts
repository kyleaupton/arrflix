// A minimal, single-connection Server-Sent Events reader with an owned
// reconnect loop.
//
// We hand-roll this instead of using the @hey-api generated SSE client
// (web/src/client/core/serverSentEvents.gen.ts) for one structural reason: that
// client resolves its request URL and headers ONCE and reuses them across
// reconnects. It therefore cannot inject a freshly-allocated `?session=` (which
// Last-Event-ID replay needs to engage) or a refreshed auth token on a
// reconnect. Owning the loop lets us re-resolve both on every attempt and reset
// the backoff after a successful connect.
//
// This is transport only — it knows nothing about the event bus, Pinia, or the
// broker's session model. The connection singleton (realtime/connection.ts)
// wires the callbacks.

export type SseStatus = 'connecting' | 'connected' | 'reconnecting' | 'error'

export interface SseFrame {
  id?: string
  event?: string
  data: unknown
}

export interface SseStreamOptions {
  /**
   * Builds the request URL. Re-invoked on every (re)connect attempt so a session
   * id captured from an earlier `ready` event is injected as `?session=` on the
   * next reconnect, letting the server resume via Last-Event-ID.
   */
  buildUrl: () => string
  /**
   * Returns the current bearer token, re-read on every attempt so a token
   * refreshed mid-stream is presented on the next reconnect. A null/undefined
   * token sends no Authorization header.
   */
  getAuthToken: () => string | null | undefined
  /** Called for each parsed SSE frame that carried a `data:` line. */
  onEvent: (frame: SseFrame) => void
  /** Called on every connection-state transition. */
  onStatus: (status: SseStatus) => void
  /** First retry delay, doubled each failed attempt. Default 500ms. */
  baseRetryMs?: number
  /** Backoff ceiling. Default 5000ms. */
  maxRetryMs?: number
  /** Injectable fetch, for tests. Default globalThis.fetch. */
  fetchFn?: typeof fetch
}

export interface SseStream {
  /** Aborts the connection and stops the reconnect loop. Idempotent. */
  close: () => void
}

export function openSseStream(opts: SseStreamOptions): SseStream {
  const {
    buildUrl,
    getAuthToken,
    onEvent,
    onStatus,
    baseRetryMs = 500,
    maxRetryMs = 5000,
    fetchFn = globalThis.fetch.bind(globalThis),
  } = opts

  const controller = new AbortController()
  const { signal } = controller
  let stopped = false

  // The resume cursor, owned here and re-sent as Last-Event-ID on every attempt.
  // Only `id:` lines move it; heartbeats / ready carry no id.
  let lastEventId: string | undefined

  const emitStatus = (s: SseStatus) => {
    if (stopped || signal.aborted) return
    onStatus(s)
  }
  const emitEvent = (frame: SseFrame) => {
    if (stopped || signal.aborted) return
    onEvent(frame)
  }

  // Sleep that resolves early on abort so a pending backoff never delays teardown.
  const sleep = (ms: number) =>
    new Promise<void>((resolve) => {
      const onAbort = () => {
        clearTimeout(timer)
        resolve()
      }
      const timer = setTimeout(() => {
        signal.removeEventListener('abort', onAbort)
        resolve()
      }, ms)
      signal.addEventListener('abort', onAbort, { once: true })
    })

  // Parse one SSE chunk (the text between blank-line separators) into a frame.
  // Ported from the generated client's parser: multi-line `data:` joined with
  // \n; `id:` moves the resume cursor; comment lines (leading `:`), `retry:`,
  // and unknown fields are ignored. Returns null when the chunk had no `data:`.
  const parseChunk = (chunk: string): SseFrame | null => {
    const dataLines: string[] = []
    let event: string | undefined
    for (const line of chunk.split('\n')) {
      if (line.startsWith('data:')) {
        dataLines.push(line.replace(/^data:\s*/, ''))
      } else if (line.startsWith('event:')) {
        event = line.replace(/^event:\s*/, '')
      } else if (line.startsWith('id:')) {
        lastEventId = line.replace(/^id:\s*/, '')
      }
    }
    if (!dataLines.length) return null
    const raw = dataLines.join('\n')
    let data: unknown
    try {
      data = JSON.parse(raw)
    } catch {
      data = raw
    }
    return { id: lastEventId, event, data }
  }

  // One connection attempt: open the stream, then pump frames until it ends or
  // drops. onOpen fires the instant the response is healthy so the loop can mark
  // us connected and reset its backoff.
  const runOnce = async (onOpen: () => void): Promise<void> => {
    const token = getAuthToken()
    const headers = new Headers({ Accept: 'text/event-stream' })
    if (token) headers.set('Authorization', `Bearer ${token}`)
    if (lastEventId !== undefined) headers.set('Last-Event-ID', lastEventId)

    const response = await fetchFn(buildUrl(), {
      method: 'GET',
      headers,
      signal,
      cache: 'no-store',
    })

    if (!response.ok) {
      throw new Error(`SSE failed: ${response.status} ${response.statusText}`)
    }
    if (!response.body) {
      throw new Error('SSE response has no body')
    }

    onOpen()

    const reader = response.body.pipeThrough(new TextDecoderStream()).getReader()
    let buffer = ''
    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += value
        const chunks = buffer.split('\n\n')
        buffer = chunks.pop() ?? ''
        for (const chunk of chunks) {
          if (!chunk) continue
          const frame = parseChunk(chunk)
          if (frame) emitEvent(frame)
        }
      }
    } finally {
      reader.releaseLock()
    }
  }

  const loop = async () => {
    let attempt = 0
    let connectedBefore = false
    while (!stopped && !signal.aborted) {
      if (connectedBefore) emitStatus('reconnecting')
      else if (attempt === 0) emitStatus('connecting')
      // else: a pre-first-connect retry — keep the 'error' set in the catch below.

      try {
        await runOnce(() => {
          connectedBefore = true
          attempt = 0 // a successful open resets the backoff
          emitStatus('connected')
        })
      } catch {
        if (stopped || signal.aborted) break
        // A never-connected failure surfaces as 'error' so the UI can show the
        // initial-connect problem; a drop after connecting stays 'reconnecting'.
        if (!connectedBefore) emitStatus('error')
      }

      if (stopped || signal.aborted) break
      attempt++
      await sleep(Math.min(baseRetryMs * 2 ** (attempt - 1), maxRetryMs))
    }
  }

  void loop()

  return {
    close: () => {
      stopped = true
      controller.abort()
    },
  }
}
