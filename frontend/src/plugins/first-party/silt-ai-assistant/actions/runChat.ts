// Shared chat complete + stream helpers for Writing Assistant actions.

import type {
  PluginAIChatMessage,
  PluginAICompleteResult,
  PluginAIStream,
  PluginContext
} from '../../../sdk'

export type StreamSession = {
  cancel: () => void
}

function abortError(): Error {
  const err = new Error('Cancelled')
  err.name = 'AbortError'
  return err
}

/** True when the provider rejected streaming specifically (not auth/rate-limit). */
export function isStreamUnsupportedError(e: unknown): boolean {
  const err = e as { code?: string; message?: string }
  const code = String(err?.code ?? '')
  const msg = String(err?.message ?? '')
  return code.includes('bad-request') || /stream/i.test(msg)
}

export async function completeBuffered(
  ctx: PluginContext,
  messages: PluginAIChatMessage[],
  maxTokens = 1200
): Promise<PluginAICompleteResult> {
  const res = await ctx.ai.complete({
    messages,
    maxTokens,
    temperature: 0.3
  })
  if (res && typeof res === 'object' && 'content' in res) {
    return res as PluginAICompleteResult
  }
  throw new Error('Unexpected complete() result shape')
}

/**
 * Stream a completion. `onSession` receives a cancel handle as soon as the
 * stream is open. `isCancelled` is polled each delta — when true, cancel() is
 * invoked and an AbortError is thrown so callers can ignore the result.
 *
 * Buffered fallback only when the provider rejects streaming (same gate as
 * silt-ai-qa). Auth/rate-limit/timeout errors surface without a second request.
 */
export async function completeStreaming(
  ctx: PluginContext,
  messages: PluginAIChatMessage[],
  onDelta: (chunk: string, full: string) => void,
  opts: {
    maxTokens?: number
    onSession?: (session: StreamSession) => void
    isCancelled?: () => boolean
  } = {}
): Promise<{ content: string; model: string }> {
  const maxTokens = opts.maxTokens ?? 1600
  try {
    const stream = (await ctx.ai.complete({
      messages,
      maxTokens,
      temperature: 0.3,
      stream: true
    })) as PluginAIStream

    opts.onSession?.({
      cancel: () => {
        void stream.cancel()
      }
    })

    if (opts.isCancelled?.()) {
      await stream.cancel()
      throw abortError()
    }

    let full = ''
    for await (const delta of stream) {
      if (opts.isCancelled?.()) {
        await stream.cancel()
        throw abortError()
      }
      full += delta
      onDelta(delta, full)
    }
    const final = await stream.result()
    if (opts.isCancelled?.()) {
      throw abortError()
    }
    return {
      content: final.content || full,
      model: final.model || ''
    }
  } catch (e) {
    if (
      (e instanceof Error && e.name === 'AbortError') ||
      opts.isCancelled?.()
    ) {
      throw e instanceof Error && e.name === 'AbortError' ? e : abortError()
    }
    // Only fall back when the provider cannot stream (mirrors silt-ai-qa).
    if (!isStreamUnsupportedError(e)) {
      throw e
    }
    if (opts.isCancelled?.()) {
      throw abortError()
    }
    const res = await completeBuffered(ctx, messages, maxTokens)
    if (opts.isCancelled?.()) {
      throw abortError()
    }
    onDelta(res.content, res.content)
    return { content: res.content, model: res.model }
  }
}
