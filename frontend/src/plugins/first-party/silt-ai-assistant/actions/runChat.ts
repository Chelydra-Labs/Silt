// Shared chat complete + stream helpers for Writing Assistant actions.

import type {
  PluginAIChatMessage,
  PluginAICompleteResult,
  PluginAIStream,
  PluginContext
} from '../../../sdk'

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

export async function completeStreaming(
  ctx: PluginContext,
  messages: PluginAIChatMessage[],
  onDelta: (chunk: string, full: string) => void,
  maxTokens = 1600
): Promise<{ content: string; model: string }> {
  try {
    const stream = (await ctx.ai.complete({
      messages,
      maxTokens,
      temperature: 0.3,
      stream: true
    })) as PluginAIStream

    let full = ''
    for await (const delta of stream) {
      full += delta
      onDelta(delta, full)
    }
    const final = await stream.result()
    return {
      content: final.content || full,
      model: final.model || ''
    }
  } catch {
    // Providers that reject streaming fall back to buffered complete.
    const res = await completeBuffered(ctx, messages, maxTokens)
    onDelta(res.content, res.content)
    return { content: res.content, model: res.model }
  }
}
