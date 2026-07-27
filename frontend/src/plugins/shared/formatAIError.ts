// Format PluginAIError (and unknown rejections) for user-facing AI UI.
// After normalizeAIError, stream/IPC rejections are Error instances with
// `name: 'PluginAIError'` and attached `code`/`status`. Plain `{ code, message }`
// objects remain supported for older call sites and tests.

import { asString } from '../../lib/asString'
import { AIErrorKind } from '../../generated/enums'

function messageOf(e: unknown): string {
  if (typeof e === 'object' && e !== null && 'message' in e) {
    return asString((e as { message?: unknown }).message).trim()
  }
  return ''
}

function codeOf(e: unknown): string | undefined {
  if (typeof e === 'object' && e !== null && 'code' in e) {
    const code = (e as { code?: unknown }).code
    return typeof code === 'string' ? code : undefined
  }
  return undefined
}

function messageForCode(code: string | undefined, msg: string): string | null {
  switch (code) {
    case AIErrorKind.ErrUnauthorized:
      return 'AI provider rejected the request (unauthorized). Check your API key in Settings → AI.'
    case AIErrorKind.ErrRateLimited:
      return 'AI provider rate limit reached. Wait a moment and try again.'
    case AIErrorKind.ErrModelMissing:
      return 'Chat model is missing or invalid. Configure a model in Settings → AI.'
    case AIErrorKind.ErrTimeout:
      return 'AI request timed out. Try again, or check that the provider is reachable.'
    case AIErrorKind.ErrUnreachable:
      return 'Could not reach the AI provider. Check that the local server is running or the endpoint is correct.'
    case AIErrorKind.ErrForbidden:
      return 'AI provider forbade this request.'
    case AIErrorKind.ErrBadRequest:
      return msg || 'AI provider rejected the request.'
    case AIErrorKind.ErrServer:
      return msg || 'AI provider server error. Retry in a moment.'
    default:
      return null
  }
}

export function formatAIError(e: unknown): string {
  if (e instanceof Error && e.name === 'AbortError') return 'Cancelled.'

  const code = codeOf(e)
  const msg = messageOf(e)
  const fromCode = messageForCode(code, msg)
  if (fromCode != null) return fromCode

  if (e instanceof Error) return e.message || 'The AI request failed.'

  if (e != null && typeof e === 'object') {
    if (msg) return msg
    if ((e as { name?: string }).name === 'AbortError') return 'Cancelled.'
    return 'The AI request failed.'
  }

  if (e == null) return 'The AI request failed.'
  return asString(e, 'The AI request failed.')
}

export function isAbortError(e: unknown): boolean {
  return (
    (e instanceof Error && e.name === 'AbortError') ||
    (typeof e === 'object' &&
      e !== null &&
      (e as { name?: string }).name === 'AbortError')
  )
}
