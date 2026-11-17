import { onTestFinished } from 'vitest'
import { HttpResponse } from 'msw'
import { handleEventsStream } from '@/client/msw.gen'
import { connect, reset } from '@/realtime/connection'
import { server } from './server'

/**
 * Opens the app's realtime connection against an MSW-served event stream and
 * returns a function that delivers one SSE frame on it. The frames travel the
 * same path a server's do: the SSE reader parses them and the connection
 * dispatches them to whatever bindings are installed. The connection is torn
 * down when the test finishes.
 */
export function openEventStream(): (event: string, data: unknown) => void {
  const encoder = new TextEncoder()
  let controller: ReadableStreamDefaultController<Uint8Array> | undefined
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      controller = c
    },
  })
  server.use(
    handleEventsStream(
      () => new HttpResponse(body, { headers: { 'Content-Type': 'text/event-stream' } }),
    ),
  )
  connect()
  onTestFinished(() => reset())

  return (event, data) => {
    controller?.enqueue(encoder.encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`))
  }
}
