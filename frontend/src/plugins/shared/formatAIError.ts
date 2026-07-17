// Format PluginAIError (and unknown rejections) for user-facing AI UI.
// Stream/IPC rejections are plain objects `{ code, message }`, not Error
// instances — String(err) yields "[object Object]" and must not be used.

import type { PluginAIError } from '../sdk'

export function formatAIError(e: unknown): string {
  if (e instanceof Error) {
    if (e.name === 'AbortError') return 'Cancelled.'
    return e.message || 'The AI request failed.'
  }
  if (e != null && typeof e === 'object') {
    const err = e as Partial<PluginAIError> & {
      message?: string
      code?: string
    }
    const code = err.code
    const msg = (err.message || '').trim()
    switch (code) {
      case 'unauthorized':
        return 'AI provider rejected the request (unauthorized). Check your API key in Settings → AI.'
      case 'rate-limited':
        return 'AI provider rate limit reached. Wait a moment and try again.'
      case 'model-missing':
        return 'Chat model is missing or invalid. Configure a model in Settings → AI.'
      case 'timeout':
        return 'AI request timed out. Try again, or check that the provider is reachable.'
      case 'unreachable':
        return 'Could not reach the AI provider. Check that the local server is running or the endpoint is correct.'
      case 'forbidden':
        return 'AI provider forbade this request.'
      case 'bad-request':
        return msg || 'AI provider rejected the request.'
      case 'server':
        return msg || 'AI provider server error. Retry in a moment.'
      default:
        if (msg) return msg
        if ((err as { name?: string }).name === 'AbortError')
          return 'Cancelled.'
        return 'The AI request failed.'
    }
  }
  if (e == null) return 'The AI request failed.'
  return String(e)
}

export function isAbortError(e: unknown): boolean {
  return (
    (e instanceof Error && e.name === 'AbortError') ||
    (typeof e === 'object' &&
      e !== null &&
      (e as { name?: string }).name === 'AbortError')
  )
}
