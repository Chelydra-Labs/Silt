// Agent loop (#596).
//
// Drives a multi-turn tool-using conversation against ctx.ai.complete. Each
// iteration sends the accumulated messages + tool catalog; if the model
// requests tools, they dispatch in parallel and the results are appended as
// 'tool' messages for the next iteration. When the model emits no tool calls,
// the final text streams to onAssistantText and the loop returns. The loop is
// bounded (max 8 iterations) so a model stuck calling tools cannot spin
// forever, and is cancellable via an AbortSignal or the session cancel flag.
//
// Phase 5 staging (#605): when a tool returns `{isStaged: true, stagedToken}`,
// the loop does NOT feed it to the model. Instead it pauses with an onStaging
// callback; the UX calls resolveStaging(token, confirmed) to resume. On
// confirm the tool's commit() runs and its result is fed back; on reject a
// "rejected by user" message is fed back. This keeps the model informed of
// the outcome without ever handing it the destructive primitive directly.

import type {
  PluginAIChatMessage,
  PluginAICompleteResult,
  PluginContext,
  PluginAIStream
} from '../../sdk'
import {
  buildToolCatalog,
  dispatchTool,
  getTools,
  type StagedPreview
} from './tool-registry'
import { confirmOperation, rejectOperation } from './staging'

export const MAX_ITERATIONS = 8
/** Tool result bodies above this many bytes are truncated for the model. */
export const TOOL_RESULT_MAX_BYTES = 10 * 1024

/** Fired when a tool stages a destructive op awaiting user confirmation. */
export interface StagingEvent {
  token: string
  preview: StagedPreview
}

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
  /**
   * Fired when a tool stages a destructive op. The loop pauses until
   * resolveStaging is called with the same token (the session exposes a
   * Promise-based resolver; see createAgentSession).
   */
  onStaging?: (event: StagingEvent) => void
  /**
   * Phase 5 staging hook: resolve a staged op to confirmed or rejected. The
   * loop awaits this for each staged tool result; the UX-backed resolver
   * resolves with true (Confirm) or false (Reject). When omitted, the loop
   * treats staging as auto-rejected (a non-interactive test default).
   */
  awaitStaging?: (event: StagingEvent) => Promise<boolean>
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
 * Convert a dispatched tool result into the 'tool' message body the model
 * sees next iteration. Normal results return their (truncated) content or
 * error. Staged results block: the loop awaits user confirmation via
 * opts.awaitStaging (defaulting to auto-reject when no UX is attached), then:
 *   - Confirm → confirmOperation redeems the token, the tool's commit() runs
 *     against the stored (unmodified) params, and its result is returned.
 *   - Reject  → rejectOperation marks the token consumed; "rejected by user"
 *     is returned so the model can re-plan.
 * Any staging error (expired, replayed, malformed) is surfaced to the model
 * as the tool message so the model can recover instead of stalling.
 */
async function materializeToolMessage(
  ctx: PluginContext,
  toolName: string,
  res: {
    content: string
    error?: string
    isStaged?: boolean
    stagedToken?: string
    stagedPreview?: StagedPreview
  },
  opts: AgentOptions
): Promise<string> {
  if (res.error) {
    return `Error: ${res.error}`
  }
  if (!res.isStaged || !res.stagedToken) {
    return truncateToolResult(res.content)
  }

  const token = res.stagedToken
  const preview = res.stagedPreview ?? { kind: toolName, summary: toolName }
  const event: StagingEvent = { token, preview }
  opts.onStaging?.(event)

  let confirmed = false
  if (opts.awaitStaging) {
    confirmed = await opts.awaitStaging(event)
  }

  if (!confirmed) {
    try {
      await rejectOperation(ctx, token)
    } catch {
      /* Token may already be consumed; treat as rejected either way. */
    }
    return `Operation "${preview.summary}" was rejected by the user. Propose a different approach or stop.`
  }

  try {
    const op = await confirmOperation(ctx, token)
    const tool = getTools().find((t) => t.name === toolName)
    if (!tool?.commit) {
      return `Error: staged operation "${op.kind}" has no commit handler.`
    }
    const committed = await tool.commit(ctx, op.params)
    return committed.error
      ? `Error: ${committed.error}`
      : truncateToolResult(committed.content)
  } catch (e: unknown) {
    const message = e instanceof Error ? e.message : String(e)
    return `Error: staged operation could not be applied (${message}).`
  }
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
      // Staged results are NOT fed to the model — they pause the loop until
      // the UX resolves them (onStaging + awaitStaging), then their commit
      // outcome (or a "rejected" message) becomes the tool message.
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
        const toolMessage = await materializeToolMessage(
          ctx,
          call.name,
          res,
          opts
        )
        if (cancelled()) break
        messages.push({
          role: 'tool',
          tool_call_id: call.id,
          content: toolMessage
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
  /**
   * Resolve a pending staged operation (Phase 5). `confirmed = true` runs
   * the tool's commit; `false` rejects and surfaces "rejected by user" to
   * the model. No-op when there is no pending resolver for `token` (the UX
   * may have raced a stale event against a new turn).
   */
  resolveStaging: (token: string, confirmed: boolean) => void
}

/**
 * Create a session bound to `ctx` with its own cancellation flag. cancel()
 * flips the flag; the in-flight run observes it between iterations and stops.
 *
 * Phase 5 staging: when a tool returns a staged result, the loop awaits the
 * `awaitStaging` callback, which resolves when resolveStaging(token, bool)
 * is called from the UX. A token → resolver Map bridges the two sides.
 */
export function createAgentSession(ctx: PluginContext): AgentSession {
  // An AbortController per active run is created in run(); cancel() trips it.
  let controller: AbortController | null = null
  const pendingStaging = new Map<string, (confirmed: boolean) => void>()

  function resolveStaging(token: string, confirmed: boolean): void {
    const resolve = pendingStaging.get(token)
    if (!resolve) return
    pendingStaging.delete(token)
    resolve(confirmed)
  }

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
      // Bridge onStaging → awaitStaging so the loop pauses until the UX
      // calls resolveStaging. onStaging is already announced by the loop
      // before awaitStaging fires, so the wrapper only owns the resolver.
      // When a caller passes their own awaitStaging (test/programmatic),
      // it wins and the session's resolver Map is bypassed.
      const awaitStaging =
        opts.awaitStaging ??
        ((event: StagingEvent): Promise<boolean> => {
          return new Promise<boolean>((resolve) => {
            pendingStaging.set(event.token, resolve)
          })
        })
      try {
        return await runAgent(ctx, userMessage, chatHistory, {
          ...opts,
          awaitStaging,
          onStaging: opts.onStaging,
          signal: controller.signal
        })
      } finally {
        if (controller?.signal.aborted) {
          // Cancel any staging still awaiting — the loop will not resume them.
          for (const resolve of pendingStaging.values()) resolve(false)
          pendingStaging.clear()
          controller = null
        }
      }
    },
    cancel() {
      controller?.abort()
    },
    resolveStaging
  }
}
