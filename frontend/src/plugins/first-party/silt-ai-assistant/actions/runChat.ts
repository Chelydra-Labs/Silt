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
      const err = new Error('Cancelled')
      err.name = 'AbortError'
      throw err
    }

    let full = ''
    for await (const delta of stream) {
      if (opts.isCancelled?.()) {
        await stream.cancel()
        const err = new Error('Cancelled')
        err.name = 'AbortError'
        throw err
      }
      full += delta
      onDelta(delta, full)
    }
    const final = await stream.result()
    if (opts.isCancelled?.()) {
      const err = new Error('Cancelled')
      err.name = 'AbortError'
      throw err
    }
    return {
      content: final.content || full,
      model: final.model || ''
    }
  } catch (e) {
    if (
      e instanceof Error &&
      (e.name === 'AbortError' || opts.isCancelled?.())
    ) {
      throw e
    }
    // Providers that reject streaming fall back to buffered complete.
    if (opts.isCancelled?.()) {
      const err = new Error('Cancelled')
      err.name = 'AbortError'
      throw err
    }
    const res = await completeBuffered(ctx, messages, maxTokens)
    if (opts.isCancelled?.()) {
      const err = new Error('Cancelled')
      err.name = 'AbortError'
      throw err
    }
    onDelta(res.content, res.content)
    return { content: res.content, model: res.model }
  }
}
