import type { ProblemDetails } from '@/client/types.gen'

export function isProblem(err: unknown): err is ProblemDetails {
  return typeof err === 'object' && err !== null && 'status' in err && 'title' in err
}

export function problemMessage(err: unknown, fallback = 'Unexpected error'): string {
  if (isProblem(err)) return err.detail ?? err.title ?? fallback
  if (err instanceof Error) return err.message
  return fallback
}
