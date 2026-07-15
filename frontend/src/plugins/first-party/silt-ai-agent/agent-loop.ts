// Agent loop (#596).
//
// Drives a multi-turn tool-using conversation against ctx.ai.complete. Each
// iteration sends the accumulated messages + tool catalog; if the model
// requests tools, they dispatch in parallel and the results are appended as
// 'tool' messages for the next iteration. When the model emits no tool calls,
// the final text streams to onAssistantText and the loop returns. The loop is
// bounded (max 8 iterations) so a model stuck calling tools cannot spin
// forever, and is cancellable via an AbortSignal or the session cancel flag.

import type {
  PluginAIChatMessage,
  PluginAICompleteResult,
  PluginContext,
  PluginAIStream
} from '../../sdk'
import { buildToolCatalog, dispatchTool, getTools } from './tool-registry'

export const MAX_ITERATIONS = 8
/** Tool result bodies above this many bytes are truncated for the model. */
export const TOOL_RESULT_MAX_BYTES = 10 * 1024

export interface AgentOptions {
  /** Streamed assistant text delta (final answer). */
  onAssistantText?: (chunk: string, acc: string) => void
  /** Fired when the model requests a tool call, before dispatch. */
  onToolCall?: (call: {
    id: string
    name: string
    args: Record<string, unknown>
  }) => void
  /** Fired when a tool result lands (success or error). */
  onToolResult?: (result: {
    id: string
    name: string
    result: { content: string; error?: string }
  }) => void
  /** Fired when the run completes (final text assembled). */
  onDone?: (finalText: string) => void
  /** Fired on a terminal error (not cancellation). */
  onError?: (err: unknown) => void
  /** Optional external cancellation; loop also honors the session flag. */
  signal?: AbortSignal
}

export interface AgentRunResult {
  text: string
  iterations: number
  /** True when the loop stopped because cancellation was requested. */
  cancelled: boolean
  /** True when the loop hit MAX_ITERATIONS without a final answer. */
  hitIterationCap: boolean
}

/**
 * Build the system prompt from the registered tool catalog + live vault
 * context. The model needs to know which tools exist and that it operates on
 * the active notebook.
 */
export function buildSystemPrompt(ctx: PluginContext): string {
  const tools = getTools()
  const toolLines = tools.length
    ? tools.map((t) => `- ${t.name}: ${t.description}`).join('\n')
    : '- (no tools registered)'
  const notebook = ctx.activeNotebook || '(none)'
  return [
    "You are Silt AI Agent, an assistant that works inside the user's note vault.",
    `Active notebook: ${notebook}.`,
    'Use the available tools to search, read, create, and organize notes.',
    'When you have enough information, answer the user directly without calling more tools.',
    'Available tools:',
    toolLines
  ].join('\n')
}

/** Truncate a tool result body to TOOL_RESULT_MAX_BYTES with a marker. */
export function truncateToolResult(content: string): string {
  if (content.length <= TOOL_RESULT_MAX_BYTES) return content
  // Slice on the byte budget and append the marker so the model knows there
  // was more it cannot see (it can re-query with a narrower request).
  const slice = content.slice(0, TOOL_RESULT_MAX_BYTES)
  return `${slice}\n[… truncated at 10KB]`
}

/**
 * Consume one ctx.ai.complete streaming call: iterate content deltas and
 * resolve the final result (which carries tool_calls). Returns the streamed
 * text plus the reassembled completion.
 */
async function consumeStream(
  stream: PluginAIStream,
  onAssistantText?: (chunk: string, acc: string) => void,
  isCancelled?: () => boolean
): Promise<{ text: string; result: PluginAICompleteResult }> {
  let acc = ''
  // The stream is an async iterable of content deltas. We drain it for the
  // live UX, then await result() for the authoritative tool_calls + final
  // content. result() resolves when the upstream 'done' event fires.
  for await (const delta of stream) {
    if (isCancelled?.()) {
      try {
        await stream.cancel()
      } catch {
        /* best-effort */
      }
      break
    }
    acc += delta
    onAssistantText?.(delta, acc)
  }
  const result = await stream.result()
  // If the model streamed no text but result carries content (e.g. a
  // reasoning-only stream), surface it so the UX shows something.
  if (!acc && result.content) {
    acc = result.content
    onAssistantText?.(result.content, acc)
  }
  return { text: result.content || acc, result }
}

/**
 * Run one agent turn. Appends the user message to `chatHistory`, loops up to
 * MAX_ITERATIONS, and returns the final text + control flags.
 */
export async function runAgent(
  ctx: PluginContext,
  userMessage: string,
  chatHistory: PluginAIChatMessage[],
  opts: AgentOptions = {}
): Promise<AgentRunResult> {
  const cancelled = () => Boolean(opts.signal?.aborted)
  const messages: PluginAIChatMessage[] = [
    { role: 'system', content: buildSystemPrompt(ctx) },
    ...chatHistory,
    { role: 'user', content: userMessage }
  ]

  let iterations = 0
  let lastText = ''
  try {
    while (iterations < MAX_ITERATIONS) {
      if (cancelled()) {
        return {
          text: lastText,
          iterations,
          cancelled: true,
          hitIterationCap: false
        }
      }
      iterations++
      const stream = (await ctx.ai.complete({
        messages,
        tools: buildToolCatalog(),
        toolChoice: { mode: 'auto' },
        stream: true
      })) as PluginAIStream

      const { text, result } = await consumeStream(
        stream,
        opts.onAssistantText,
        cancelled
      )
      lastText = text

      if (cancelled()) {
        return {
          text: lastText,
          iterations,
          cancelled: true,
          hitIterationCap: false
        }
      }

      const calls = result.tool_calls ?? []
      if (calls.length === 0) {
        // No further tool use: this is the final answer.
        opts.onDone?.(lastText)
        return {
          text: lastText,
          iterations,
          cancelled: false,
          hitIterationCap: false
        }
      }

      // Append the assistant turn (with tool_calls) so the next iteration
      // carries the correlation ids the provider expects.
      messages.push({
        role: 'assistant',
        content: lastText,
        tool_calls: calls
      })

      // Dispatch all requested tools in parallel; surface each to the UX.
      const results = await Promise.all(
        calls.map(async (call) => {
          opts.onToolCall?.({
            id: call.id,
            name: call.name,
            args: call.arguments
          })
          const res = await dispatchTool(ctx, call.name, call.arguments)
          opts.onToolResult?.({ id: call.id, name: call.name, result: res })
          return { call, res }
        })
      )
      for (const { call, res } of results) {
        if (cancelled()) break
        messages.push({
          role: 'tool',
          tool_call_id: call.id,
          content: res.error
            ? `Error: ${res.error}`
            : truncateToolResult(res.content)
        })
      }
    }
    // Exited the loop by hitting the cap without a tool-free final answer.
    return {
      text: lastText,
      iterations,
      cancelled: false,
      hitIterationCap: true
    }
  } catch (err) {
    if (cancelled()) {
      return {
        text: lastText,
        iterations,
        cancelled: true,
        hitIterationCap: false
      }
    }
    opts.onError?.(err)
    throw err
  }
}

export interface AgentSession {
  /** Run one user turn. See runAgent. */
  run: (
    userMessage: string,
    chatHistory: PluginAIChatMessage[],
    opts?: AgentOptions
  ) => Promise<AgentRunResult>
  /** Request cancellation of the in-flight run. */
  cancel: () => void
}

/**
 * Create a session bound to `ctx` with its own cancellation flag. cancel()
 * flips the flag; the in-flight run observes it between iterations and stops.
 */
export function createAgentSession(ctx: PluginContext): AgentSession {
  // An AbortController per active run is created in run(); cancel() trips it.
  let controller: AbortController | null = null
  return {
    async run(userMessage, chatHistory, opts = {}) {
      controller?.abort()
      controller = new AbortController()
      if (opts.signal) {
        // Chain the caller's signal so either source aborts the run.
        if (opts.signal.aborted) controller.abort()
        else
          opts.signal.addEventListener('abort', () => controller?.abort(), {
            once: true
          })
      }
      try {
        return await runAgent(ctx, userMessage, chatHistory, {
          ...opts,
          signal: controller.signal
        })
      } finally {
        if (controller?.signal.aborted) controller = null
      }
    },
    cancel() {
      controller?.abort()
    }
  }
}
