import { ApiError } from './client'

/** Field name (as the server names it, e.g. "e_mail") to its message, from a validation failure. */
export function fieldErrors(err: unknown): Record<string, string> {
  if (!(err instanceof ApiError) || !Array.isArray(err.data)) return {}
  const out: Record<string, string> = {}
  for (const item of err.data as unknown[]) {
    if (typeof item !== 'object' || item === null) continue
    const { error_field: field, error_msg: msg } = item as Record<string, unknown>
    if (typeof field === 'string' && typeof msg === 'string') out[field] = msg
  }
  return out
}

/** One line fit to show a user. Unexpected errors are not echoed, they may hold internals. */
export function describeError(err: unknown): string {
  if (err instanceof ApiError) return err.message || '服务器出错了，请稍后再试'
  return '出了点问题，请稍后再试'
}
