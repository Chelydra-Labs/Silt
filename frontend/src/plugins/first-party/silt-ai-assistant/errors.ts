// Normalize PluginAIError (and unknown rejections) for Writing Assistant UI.

import type { PluginAIError } from '../../sdk'

export function formatAIError(e: unknown): string {
  const err = e as Partial<PluginAIError> & { message?: string; code?: string }
  const code = err?.code
  const msg = (err?.message || '').trim()
  switch (code) {
    case 'unauthorized':
      return 'AI provider rejected the request (unauthorized). Check your API key in Settings → AI Provider.'
    case 'rate-limited':
      return 'AI provider rate limit reached. Wait a moment and try again.'
    case 'model-missing':
      return 'Chat model is missing or invalid. Configure a model in Settings → AI Provider.'
    case 'timeout':
      return 'AI request timed out. Try a shorter selection or retry.'
    case 'unreachable':
      return 'Could not reach the AI provider. Check that the local server is running or the endpoint is correct.'
    case 'forbidden':
      return 'AI provider forbade this request.'
    case 'bad-request':
      return msg || 'AI provider rejected the request.'
    case 'server':
      return msg || 'AI provider server error. Retry in a moment.'
    default:
      if (e instanceof Error && e.name === 'AbortError') {
        return 'Cancelled.'
      }
      return msg || 'The AI request failed.'
  }
}

export function isAbortError(e: unknown): boolean {
  return (
    (e instanceof Error && e.name === 'AbortError') ||
    (typeof e === 'object' &&
      e !== null &&
      (e as { name?: string }).name === 'AbortError')
  )
}
