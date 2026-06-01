import { isConnected, status } from '@/realtime/connection'

// Connection status for the UI (e.g. a "live / reconnecting" indicator). Reads
// the shared singleton refs; safe to call from any component.
export function useRealtimeStatus() {
  return { status, isConnected }
}
