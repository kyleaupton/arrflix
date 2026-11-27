import type { ProblemDetails } from '@/client/types.gen'

export function isProblem(err: unknown): err is ProblemDetails {
  return typeof err === 'object' && err !== null && 'status' in err && 'title' in err
}

export function problemMessage(err: unknown, fallback = 'Unexpected error'): string {
  if (isProblem(err)) return err.detail ?? err.title ?? fallback
  if (err instanceof Error) return err.message
  return fallback
}

/** The API's validation message for one request field, if it rejected that field. */
export function fieldProblem(err: unknown, location: string): string | null {
  if (!isProblem(err)) return null
  return err.errors?.find((e) => e.location === location)?.message ?? null
}
