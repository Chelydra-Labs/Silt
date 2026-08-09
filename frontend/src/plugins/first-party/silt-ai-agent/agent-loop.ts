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
// The **last** iteration is reserved for a forced text answer (`toolChoice:
// none`, no tools). Models that keep searching after vault_data already has
// the answer still get a synthesis turn instead of only
// "Stopped after reaching the iteration limit."
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
  captureUiLocation,
  formatUiLocationForPrompt,
  type UiLocationSnapshot
} from '../../ui-location'
import {
  buildToolCatalogFrom,
  dispatchTool,
  getTools,
  type AgentToolDef,
  type StagedPreview,
  type ToolResult,
  type ToolEvidence
} from './tool-registry'
import { confirmOperation, rejectOperation } from './staging'
import { UNTRUSTED_CONTENT_SECURITY } from './security'
import { formatAIError } from '../../shared/formatAIError'

export const MAX_ITERATIONS = 8
/** Max non-duplicate search_notes dispatches per turn before forced synthesis. */
export const MAX_SEARCH_NOTES_PER_TURN = 3
/** Default Q&A catalog (read-only retrieval); expands on write intent. */
export const QA_TOOL_NAMES = [
  'search_notes',
  'read_blocks',
  'query_tasks',
  'get_backlinks'
] as const

// Write/organize intent for full catalog at turn start. Prefer multi-word
// phrases; avoid bare verbs that dominate Q&A ("write a summary", "what did I
// delete", "update me on…").
const WRITE_INTENT_RE =
  /\b((create|add|make) (a |the |new )?(note|task|page|block)|add note|new note|make a note|draft (a |the )?note|save (this|it|to)|put this|please rename|rename( tag| the| this)?|retitle|add tag|extract (and save|to)|organize (my |the )?notes|edit (the |this |my )?(note|task|block|page|title)|modify (the |this |my )?(note|task|block|page)|update (my |the |a |this )?(notes?|task|block|page|title)|delete (the |this |a )?(note|task|block|page|tag)|fix (the |this |a )?(typo|note|task|title)|change (the |this |a )?(title|note|task)|move this|write (a |the )?(note|task) to)\b/i

/** Tool result bodies above this many bytes are truncated for the model. */
export const TOOL_RESULT_MAX_BYTES = 10 * 1024
/** Host AI rate-limit retries per complete() call (after host wait already ran). */
export const HOST_RATE_LIMIT_MAX_RETRIES = 2
/** Cap total extra wait from host rate-limit retries per complete() call. */
export const HOST_RATE_LIMIT_MAX_WAIT_MS = 10_000
/**
 * Key embedded in host AI rate-limit errors (`key=N`). MUST match
 * `aiRateLimitRetryAfterKey` in plugin_ratelimit.go.
 */
export const HOST_AI_RATE_LIMIT_RETRY_AFTER_KEY = 'retry_after_ms'

/** Parse host rate-limit errors that embed `retry_after_ms=N`. */
export function parseHostRateLimitRetryMs(err: unknown): number | null {
  const msg =
    err instanceof Error ? err.message : typeof err === 'string' ? err : ''
  if (!msg || !/AI rate limit exceeded/i.test(msg)) return null
  const re = new RegExp(`${HOST_AI_RATE_LIMIT_RETRY_AFTER_KEY}=(\\d+)`, 'i')
  const m = msg.match(re)
  if (m) {
    const n = Number(m[1])
    if (Number.isFinite(n) && n > 0)
      return Math.min(n, HOST_RATE_LIMIT_MAX_WAIT_MS)
  }
  // Legacy message without ms — short default cooldown.
  return 1000
}

function sleepAbortError(signal?: AbortSignal): Error {
  const reason = signal?.reason
  if (reason instanceof Error) return reason
  return new DOMException('Aborted', 'AbortError')
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(sleepAbortError(signal))
      return
    }
    const t = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve()
    }, ms)
    const onAbort = () => {
      clearTimeout(t)
      reject(sleepAbortError(signal))
    }
    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

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
    result: {
      content: string
      error?: string
      truncated?: boolean
      evidence?: ToolEvidence[]
    }
  }) => void
  /** Fired when a tool message is ready for the next model iteration. */
  onToolMessage?: (result: {
    id: string
    name: string
    content: string
    error?: string
    truncated?: boolean
  }) => void
  /** Fired once for an assistant turn that contains tool calls. */
  onAssistantToolCalls?: (
    calls: PluginAICompleteResult['tool_calls'],
    content: string
  ) => void
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
  /**
   * Fired after a staged op's confirm/reject path finishes (including commit
   * success/failure). The UX keeps the card pending on Confirm until this
   * reports the real outcome.
   */
  onStagingOutcome?: (
    token: string,
    outcome: 'confirmed' | 'rejected' | 'failed'
  ) => void
  /** Fired when the run completes (final text assembled). */
  onDone?: (finalText: string) => void
  /** Fired on a terminal error (not cancellation). */
  onError?: (err: unknown) => void
  /** Optional external cancellation; loop also honors the session flag. */
  signal?: AbortSignal
  /** Internal stream hook used by createAgentSession for direct cancellation. */
  onStream?: (stream: PluginAIStream) => void
}

export interface AgentRunResult {
  text: string
  iterations: number
  /** True when the loop stopped because cancellation was requested. */
  cancelled: boolean
  /**
   * True when the loop exhausted tool-using iterations without a voluntary
   * tool-free answer. A forced final synthesis turn may still have produced
   * `text`; the chat UI only shows the hard stop banner when text is empty.
   */
  hitIterationCap: boolean
  /** True when the final answer came from the reserved no-tools wrap-up turn. */
  forcedFinalAnswer?: boolean
}

/** Detect write/organize intent so the turn starts with the full tool catalog. */
export function detectWriteIntent(userMessage: string): boolean {
  return WRITE_INTENT_RE.test(userMessage)
}

/** Keys whose string values are case-folded for anti-thrash (free-text query). */
const CASEFOLD_ARG_KEYS = new Set(['query', 'q', 'text', 'content', 'type'])

/**
 * Normalize a single string for fingerprinting. Case-fold only when the parent
 * key is free-text (query/type); preserve case for path keys (notebook,
 * section, page, block_id) so filesystem-sensitive filters stay distinct.
 */
function normalizeArgString(s: string, caseFold: boolean): string {
  const t = s.trim().replace(/\s+/g, ' ')
  return caseFold ? t.toLowerCase() : t
}

/**
 * Normalize tool args for duplicate fingerprinting: deep-sort keys; collapse
 * whitespace; case-fold free-text fields only (not path/id keys).
 */
export function normalizeToolArgs(value: unknown, parentKey?: string): unknown {
  if (typeof value === 'string') {
    const caseFold =
      parentKey == null || CASEFOLD_ARG_KEYS.has(parentKey.toLowerCase())
    return normalizeArgString(value, caseFold)
  }
  if (Array.isArray(value)) {
    return value.map((v) => normalizeToolArgs(v, parentKey))
  }
  if (value && typeof value === 'object') {
    const obj = value as Record<string, unknown>
    const out: Record<string, unknown> = {}
    for (const k of Object.keys(obj).sort()) {
      out[k] = normalizeToolArgs(obj[k], k)
    }
    return out
  }
  return value
}

export function toolCallFingerprint(
  name: string,
  args: Record<string, unknown>
): string {
  return `${name}:${JSON.stringify(normalizeToolArgs(args))}`
}

/**
 * Build the system prompt from the turn's tool list + UI location snapshot
 * (#678 general chat, #680 page/block/tabs). Pass the same tools offered to
 * complete() so prompt and catalog stay in lockstep.
 */
export function buildSystemPrompt(
  ctx: PluginContext,
  location?: UiLocationSnapshot,
  toolsForTurn?: AgentToolDef[]
): string {
  const tools = toolsForTurn ?? getTools()
  const toolLines = tools.length
    ? tools.map((t) => `- ${t.name}: ${t.description}`).join('\n')
    : '- (no tools registered)'
  const qaOnly =
    toolsForTurn != null &&
    toolsForTurn.every((t) =>
      (QA_TOOL_NAMES as readonly string[]).includes(t.name)
    ) &&
    toolsForTurn.length > 0 &&
    toolsForTurn.length <= QA_TOOL_NAMES.length
  const raw =
    location ??
    (typeof ctx.getUiLocation === 'function'
      ? ctx.getUiLocation()
      : captureUiLocation())
  // Prefer explicit snapshot fields; fall back to live ctx getters for tests
  // that only stub activeNotebook.
  const loc: UiLocationSnapshot = {
    notebook: raw.notebook || ctx.activeNotebook || '',
    section: raw.section || ctx.activeSection || '',
    page: raw.page || ctx.activePage || '',
    ...(raw.blockId ? { blockId: raw.blockId } : {}),
    openTabs: raw.openTabs ?? []
  }
  const useToolsLine = qaOnly
    ? 'Use the available tools to search and read notes.'
    : 'Use the available tools to search, read, create, and organize notes.'
  const writePolicy = qaOnly
    ? [
        'WRITE POLICY: This turn offers read-only vault tools. Answer from search',
        'and read results. If the user later asks to change the vault, write tools',
        'may become available on a subsequent turn.'
      ]
    : [
        'WRITE POLICY: Prefer read-only tools first. Direct-write tools (create_note,',
        'update_block, extract_and_save) apply immediately as single reversible edits.',
        'Destructive bulk ops (rename_tag) are staged and require user confirmation',
        'before any vault mutation.',
        'For page-relative writes ("this page", "here"), target the Current page from',
        'UI LOCATION unless the user names a different path.'
      ]
  return [
    'You are Silt AI Agent, a general-purpose assistant with first-class access',
    "to the user's Silt note vault via tools.",
    'Answer general knowledge and non-vault questions directly when vault tools',
    'are unnecessary — do not refuse solely because a topic is outside Silt',
    'product docs or outside the vault.',
    "When the user's notes may answer the question, prefer searching and reading",
    'the vault (and the current page from UI LOCATION) and ground answers in',
    'that material when applicable.',
    useToolsLine,
    '',
    'AFTER EACH TOOL RESULT: If <vault_data> already answers the user, respond',
    'with the answer and cite locations — do not call more tools. If not, name',
    'one missing fact and call at most one targeted tool.',
    '',
    formatUiLocationForPrompt(loc),
    '',
    UNTRUSTED_CONTENT_SECURITY,
    '',
    ...writePolicy,
    '',
    'Available tools:',
    toolLines
  ].join('\n')
}

/**
 * Wrap vault-derived tool content in hard delimiters so the model can
 * distinguish untrusted data from instructions (defense-in-depth beyond the
 * system-prompt SECURITY lines).
 */
export function wrapUntrustedToolResult(
  toolName: string,
  content: string
): string {
  const body = truncateToolResult(content)
  // Angle-bracket vault_data tags are unambiguous delimiters for untrusted
  // note content so the model cannot confuse them with system instructions.
  return `<vault_data tool="${toolName}">\n${body}\n</vault_data>`
}

/** Truncate a tool result body to TOOL_RESULT_MAX_BYTES with a marker. */
export function truncateToolResult(content: string): string {
  const encoder = new TextEncoder()
  if (encoder.encode(content).byteLength <= TOOL_RESULT_MAX_BYTES)
    return content

  // Reserve space for the marker. Iterating code points rather than UTF-16
  // units prevents a byte-boundary cut from leaving a lone surrogate behind.
  const marker = '\n[… truncated at 10KB]'
  const markerBytes = encoder.encode(marker).byteLength
  const contentBudget = Math.max(0, TOOL_RESULT_MAX_BYTES - markerBytes)
  let bytes = 0
  let slice = ''
  for (const character of content) {
    const characterBytes = encoder.encode(character).byteLength
    if (bytes + characterBytes > contentBudget) break
    slice += character
    bytes += characterBytes
  }
  return `${slice}${marker}`
}

class RunAbortError extends Error {
  constructor() {
    super('Agent run cancelled')
    this.name = 'AbortError'
  }
}

function abortError(): RunAbortError {
  return new RunAbortError()
}

function raceAbort<T>(
  promise: PromiseLike<T>,
  signal?: AbortSignal
): Promise<T> {
  if (!signal) return Promise.resolve(promise)
  if (signal.aborted) {
    // The caller may have just created a pending staging promise before
    // noticing the already-aborted signal. Attach a rejection handler even
    // though this race is already lost, so cleanup can reject that promise
    // without producing an unhandled rejection.
    void Promise.resolve(promise).catch(() => {})
    return Promise.reject(abortError())
  }

  return new Promise<T>((resolve, reject) => {
    const onAbort = () => {
      cleanup()
      reject(abortError())
    }
    const cleanup = () => signal.removeEventListener('abort', onAbort)
    signal.addEventListener('abort', onAbort, { once: true })
    Promise.resolve(promise).then(
      (value) => {
        cleanup()
        resolve(value)
      },
      (error: unknown) => {
        cleanup()
        reject(error instanceof Error ? error : new Error(String(error)))
      }
    )
  })
}

function isAbortError(error: unknown): boolean {
  return (
    error instanceof RunAbortError ||
    (error instanceof Error && error.name === 'AbortError')
  )
}

function visibleToolResult(
  res: ToolResult
): ToolResult & { truncated?: boolean } {
  if (res.error) return res
  const truncated =
    new TextEncoder().encode(res.content).byteLength > TOOL_RESULT_MAX_BYTES
  return {
    ...res,
    content: truncateToolResult(res.content),
    ...(truncated ? { truncated: true } : {})
  }
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
  opts: AgentOptions,
  signal?: AbortSignal
): Promise<string> {
  if (res.error) {
    return `Error: ${res.error}`
  }
  if (!res.isStaged || !res.stagedToken) {
    return wrapUntrustedToolResult(toolName, res.content)
  }

  const token = res.stagedToken
  const preview = res.stagedPreview ?? { kind: toolName, summary: toolName }
  const event: StagingEvent = { token, preview }
  opts.onStaging?.(event)

  let confirmed = false
  if (opts.awaitStaging) {
    confirmed = await raceAbort(opts.awaitStaging(event), signal)
  }

  if (!confirmed) {
    try {
      // rejectOperation returns false when the token is already consumed/
      // expired (treat as rejected either way); a genuine DB error throws and
      // is surfaced so the model/user knows the token state is uncertain
      // rather than masking a failure as a successful reject.
      await raceAbort(rejectOperation(ctx, token), signal)
    } catch (error: unknown) {
      if (signal?.aborted || isAbortError(error)) throw error
      const message = error instanceof Error ? error.message : String(error)
      opts.onStagingOutcome?.(token, 'failed')
      return `Error: could not reject operation "${preview.summary}" (${message}). The token may still be redeemable.`
    }
    opts.onStagingOutcome?.(token, 'rejected')
    void ctx.ai.auditEvent?.({
      kind: 'staging_decision',
      tool: toolName,
      outcome: 'rejected',
      status: 'rejected'
    })
    return `Operation "${preview.summary}" was rejected by the user. Propose a different approach or stop.`
  }

  try {
    const op = await raceAbort(confirmOperation(ctx, token), signal)
    const tool = getTools().find((t) => t.name === toolName)
    if (!tool?.commit) {
      opts.onStagingOutcome?.(token, 'failed')
      void ctx.ai.auditEvent?.({
        kind: 'staging_decision',
        tool: toolName,
        outcome: 'failed',
        status: 'failed'
      })
      return `Error: staged operation "${op.kind}" has no commit handler.`
    }
    const committed = await raceAbort(tool.commit(ctx, op.params), signal)
    if (committed.error) {
      opts.onStagingOutcome?.(token, 'failed')
      void ctx.ai.auditEvent?.({
        kind: 'staging_decision',
        tool: toolName,
        outcome: 'failed',
        status: 'failed'
      })
      return `Error: ${committed.error}`
    }
    opts.onStagingOutcome?.(token, 'confirmed')
    void ctx.ai.auditEvent?.({
      kind: 'staging_decision',
      tool: toolName,
      outcome: 'confirmed',
      status: 'confirmed'
    })
    return wrapUntrustedToolResult(toolName, committed.content)
  } catch (e: unknown) {
    // Abort after confirmOperation may have already marked the token used=1
    // without running commit. Surface 'failed' so the confirmation card is
    // not left pending forever.
    if (signal?.aborted || isAbortError(e)) {
      opts.onStagingOutcome?.(token, 'failed')
      throw e
    }
    const message = e instanceof Error ? e.message : String(e)
    opts.onStagingOutcome?.(token, 'failed')
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
  signal?: AbortSignal
): Promise<{ text: string; result: PluginAICompleteResult }> {
  let acc = ''
  const iterator = stream[Symbol.asyncIterator]()
  const cancelStream = () => {
    void stream.cancel().catch(() => {
      /* best-effort; the abort result is authoritative */
    })
  }
  signal?.addEventListener('abort', cancelStream, { once: true })
  try {
    // Use explicit next() calls so a stream with no further deltas still races
    // cancellation instead of waiting forever in for-await.
    while (true) {
      const step = await raceAbort(iterator.next(), signal)
      if (step.done) break
      acc += step.value
      onAssistantText?.(step.value, acc)
    }
  } finally {
    signal?.removeEventListener('abort', cancelStream)
  }
  const result = await raceAbort(stream.result(), signal)
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
  // Snapshot UI location once at run start (#680) so mid-run navigation is ignored.
  const location: UiLocationSnapshot =
    typeof ctx.getUiLocation === 'function'
      ? ctx.getUiLocation()
      : captureUiLocation()
  // Q&A subset by default; full catalog when the user message shows write intent.
  const mode: 'qa' | 'full' = detectWriteIntent(userMessage) ? 'full' : 'qa'
  const allTools = getTools()
  const toolsForMode = (): AgentToolDef[] => {
    if (mode === 'full') return allTools
    const qaSet = new Set<string>(QA_TOOL_NAMES)
    return allTools.filter((t) => qaSet.has(t.name))
  }
  let toolsForTurn = toolsForMode()
  const systemPrompt = buildSystemPrompt(ctx, location, toolsForTurn)
  const messages: PluginAIChatMessage[] = [
    { role: 'system', content: systemPrompt },
    ...chatHistory,
    { role: 'user', content: userMessage }
  ]

  let iterations = 0
  let lastText = ''
  /** True once we have fed at least one tool result into `messages`. */
  let hadToolResults = false
  /** Non-duplicate search_notes handler invocations this turn. */
  let searchNotesDispatchCount = 0
  /** Fingerprints of tool calls already dispatched this turn. */
  const seenToolFingerprints = new Set<string>()
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
      // Forced synthesis: hard iteration ceiling OR search_notes budget.
      const forceFinalAnswer =
        (iterations === MAX_ITERATIONS && hadToolResults) ||
        (searchNotesDispatchCount >= MAX_SEARCH_NOTES_PER_TURN &&
          hadToolResults)
      if (forceFinalAnswer) {
        // Explicit steer so providers that ignore toolChoice=none still stop
        // searching and synthesize from prior <vault_data> turns.
        messages.push({
          role: 'user',
          content:
            'Tool budget reached. Answer the user now using only the tool ' +
            'results already in this conversation. Do not call tools. If the ' +
            'notes contain the answer, state it clearly and cite the relevant note.'
        })
      }
      toolsForTurn = toolsForMode()
      const completeReq = {
        // Providers receive a stable request snapshot; later tool-result
        // appends must not mutate an earlier request retained by a test
        // double or transport adapter.
        messages: messages.map((message) => ({
          ...message,
          ...(message.tool_calls
            ? { tool_calls: message.tool_calls.map((call) => ({ ...call })) }
            : {})
        })),
        ...(forceFinalAnswer
          ? {
              // No catalog + none: model must produce text from prior tool turns.
              toolChoice: { mode: 'none' as const }
            }
          : {
              tools: buildToolCatalogFrom(toolsForTurn),
              toolChoice: { mode: 'auto' as const }
            }),
        stream: true as const
      }
      // Host rate limit: Go already waits up to ~3s; if still denied, cool down
      // and retry a few times so a multi-turn tool loop does not fail the turn.
      let stream!: PluginAIStream
      let rateWaitedMs = 0
      for (let attempt = 0; ; attempt++) {
        try {
          stream = (await raceAbort(
            ctx.ai.complete(completeReq),
            opts.signal
          )) as PluginAIStream
          break
        } catch (err) {
          const retryMs = parseHostRateLimitRetryMs(err)
          if (
            retryMs == null ||
            attempt >= HOST_RATE_LIMIT_MAX_RETRIES ||
            rateWaitedMs + retryMs > HOST_RATE_LIMIT_MAX_WAIT_MS
          ) {
            throw err
          }
          rateWaitedMs += retryMs
          await sleep(retryMs, opts.signal)
        }
      }
      opts.onStream?.(stream)

      const { text, result } = await consumeStream(
        stream,
        opts.onAssistantText,
        opts.signal
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
      if (calls.length === 0 || forceFinalAnswer) {
        // Voluntary stop, or reserved wrap-up turn (ignore any stray tool_calls).
        opts.onDone?.(lastText)
        const emptyForced = forceFinalAnswer && !lastText.trim()
        return {
          text: lastText,
          iterations,
          cancelled: false,
          // Hard-stop banner only when wrap-up produced nothing useful.
          hitIterationCap: emptyForced,
          forcedFinalAnswer: forceFinalAnswer || undefined
        }
      }

      // Append the assistant turn (with tool_calls) so the next iteration
      // carries the correlation ids the provider expects.
      messages.push({
        role: 'assistant',
        content: lastText,
        tool_calls: calls
      })
      opts.onAssistantToolCalls?.(calls, lastText)

      // Dispatch all requested tools in parallel; surface each to the UX.
      // Staged results are NOT fed to the model — they pause the loop until
      // the UX resolves them (onStaging + awaitStaging), then their commit
      // outcome (or a "rejected" message) becomes the tool message.
      // Tool handlers cannot be preempted mid-IPC: dispatchTool has no signal
      // parameter. raceAbort only abandons the caller's wait; an in-flight
      // mutation may still complete. The pre-dispatch gate below prevents
      // starting new mutations after Stop.
      const results = await Promise.allSettled(
        calls.map(async (call) => {
          // Pre-dispatch gate: if the run was already cancelled (Stop pressed
          // during the model call), do NOT start the tool — direct-write tools
          // would otherwise mutate the vault after the user stopped.
          let res: ToolResult
          try {
            if (opts.signal?.aborted) {
              res = { content: '', error: 'Cancelled before tool completed.' }
            } else {
              const args = call.arguments ?? {}
              const fp = toolCallFingerprint(call.name, args)
              // Dup guard before handler: same normalized name+args → error.
              if (seenToolFingerprints.has(fp)) {
                res = {
                  content: '',
                  error:
                    'Duplicate tool call with the same arguments. Answer from existing evidence or change your approach/arguments.'
                }
              } else if (
                call.name === 'search_notes' &&
                searchNotesDispatchCount >= MAX_SEARCH_NOTES_PER_TURN
              ) {
                // Parallel multi-search blast: enforce budget before dispatch.
                seenToolFingerprints.add(fp)
                res = {
                  content: '',
                  error:
                    `Search budget reached (max ${MAX_SEARCH_NOTES_PER_TURN} ` +
                    'search_notes per turn). Answer from existing evidence or ' +
                    'stop calling search_notes.'
                }
              } else {
                seenToolFingerprints.add(fp)
                if (call.name === 'search_notes') {
                  searchNotesDispatchCount++
                }
                opts.onToolCall?.({
                  id: call.id,
                  name: call.name,
                  args
                })
                void ctx.ai.auditEvent?.({
                  kind: 'tool_call',
                  tool: call.name,
                  tool_call_id: call.id,
                  status: 'start'
                })
                res = await raceAbort(
                  dispatchTool(ctx, call.name, args),
                  opts.signal
                )
              }
            }
          } catch (error: unknown) {
            const message =
              isAbortError(error) || cancelled()
                ? 'Cancelled before tool completed.'
                : formatAIError(error)
            res = { content: '', error: message }
          }
          // The dispatch callback must never reject: a thrown UX/visible
          // callback would otherwise drop this call's tool message and leave
          // the assistant tool_call without a result (a provider protocol
          // error on the next turn). Convert any such throw into the result.
          const visible = visibleToolResult(res)
          void ctx.ai.auditEvent?.({
            kind: 'tool_call',
            tool: call.name,
            tool_call_id: call.id,
            status: visible.error
              ? 'error'
              : visible.isStaged
                ? 'staged'
                : 'ok',
            // Do not send raw args/content — server redacts, but keep payload lean.
            staged: Boolean(visible.isStaged)
          })
          try {
            opts.onToolResult?.({
              id: call.id,
              name: call.name,
              result: visible
            })
          } catch {
            /* a UX callback error must not abort the tool-result protocol */
          }
          return { call, res: visible }
        })
      )
      if (cancelled()) {
        // Promise.allSettled has only waited for the abort races, not for
        // underlying tools that ignored the signal. Report what each call
        // managed before cancellation without starting staging/commit work.
        // Best-effort reject any staged tokens so they are not left redeemable
        // until TTL after Stop/close.
        for (const outcome of results) {
          if (outcome.status !== 'fulfilled') continue
          const { call, res } = outcome.value
          if (res.isStaged && res.stagedToken) {
            void rejectOperation(ctx, res.stagedToken).catch(() => {
              /* best-effort; token may already be expired/consumed */
            })
          }
          const content = res.error
            ? `Error: ${res.error}`
            : res.isStaged
              ? 'Cancelled before confirmation.'
              : res.content
          opts.onToolMessage?.({
            id: call.id,
            name: call.name,
            content,
            error: res.error,
            truncated: res.truncated
          })
        }
        return {
          text: lastText,
          iterations,
          cancelled: true,
          hitIterationCap: false
        }
      }
      for (const outcome of results) {
        if (outcome.status !== 'fulfilled') {
          // Each dispatch is expected to convert failures into a ToolResult;
          // preserve an unexpected rejection as an independent tool error.
          continue
        }
        const { call, res } = outcome.value
        const toolMessage = await materializeToolMessage(
          ctx,
          call.name,
          res,
          opts,
          opts.signal
        )
        if (cancelled()) break
        opts.onToolMessage?.({
          id: call.id,
          name: call.name,
          content: toolMessage,
          error: res.error,
          truncated:
            res.truncated || toolMessage.includes('… truncated at 10KB')
        })
        messages.push({
          role: 'tool',
          tool_call_id: call.id,
          content: toolMessage
        })
        hadToolResults = true
        // Catalog mode is fixed at turn start (write-intent → full, else Q&A).
        // Mid-turn expand after a write cannot fire from Q&A mode (write tools
        // are not offered), so we do not flip mode here — start a new turn
        // with create/edit language if the user needs write tools.
      }
    }
    // Exited without a final answer (e.g. last iteration had no prior tools
    // so forceFinalAnswer never ran, yet the model only emitted tool calls).
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
 * Create a session bound to `ctx`. Each run owns its controller, active stream,
 * and staging waiters so cancelling an older run cannot affect a newer one.
 *
 * Phase 5 staging: when a tool returns a staged result, the loop awaits the
 * `awaitStaging` callback, which resolves when resolveStaging(token, bool)
 * is called from the UX. A token → resolver Map bridges the two sides.
 */
export function createAgentSession(ctx: PluginContext): AgentSession {
  type PendingStaging = {
    resolve: (confirmed: boolean) => void
    reject: (error: unknown) => void
  }
  type ActiveRun = {
    controller: AbortController
    stream: PluginAIStream | null
    pendingStaging: Map<string, PendingStaging>
    callerSignal?: AbortSignal
  }

  let activeRun: ActiveRun | null = null

  function cancelRun(run: ActiveRun): void {
    run.controller.abort()
    if (run.stream) {
      void run.stream.cancel().catch(() => {
        /* best-effort; the abort result is authoritative */
      })
    }
    const error = abortError()
    // Consume pending staging tokens so Stop/close does not leave redeemable
    // used=0 rows until TTL (defense-in-depth for any future redeem-by-token path).
    for (const [token, pending] of run.pendingStaging) {
      void rejectOperation(ctx, token).catch(() => {
        /* best-effort; token may already be expired/consumed */
      })
      pending.reject(error)
    }
    run.pendingStaging.clear()
  }

  function resolveStaging(token: string, confirmed: boolean): void {
    const run = activeRun
    const pending = run?.pendingStaging.get(token)
    if (!run || !pending) return
    run.pendingStaging.delete(token)
    pending.resolve(confirmed)
  }

  return {
    async run(userMessage, chatHistory, opts = {}) {
      if (activeRun) cancelRun(activeRun)
      const run: ActiveRun = {
        controller: new AbortController(),
        stream: null,
        pendingStaging: new Map(),
        callerSignal: opts.signal
      }
      activeRun = run

      const onCallerAbort = () => run.controller.abort()
      if (opts.signal) {
        // Chain the caller's signal so either source aborts the run.
        if (opts.signal.aborted) run.controller.abort()
        else
          opts.signal.addEventListener('abort', onCallerAbort, { once: true })
      }
      // Bridge onStaging → awaitStaging so the loop pauses until the UX
      // calls resolveStaging. onStaging is already announced by the loop
      // before awaitStaging fires, so the wrapper only owns the resolver.
      // When a caller passes their own awaitStaging (test/programmatic),
      // it wins and the session's resolver Map is bypassed.
      const awaitStaging =
        opts.awaitStaging ??
        ((event: StagingEvent): Promise<boolean> => {
          return new Promise<boolean>((resolve, reject) => {
            run.pendingStaging.set(event.token, { resolve, reject })
          })
        })
      try {
        return await runAgent(ctx, userMessage, chatHistory, {
          ...opts,
          awaitStaging,
          signal: run.controller.signal,
          onStream: (stream) => {
            run.stream = stream
            opts.onStream?.(stream)
            if (run.controller.signal.aborted) cancelRun(run)
          }
        })
      } finally {
        if (run.callerSignal) {
          run.callerSignal.removeEventListener('abort', onCallerAbort)
        }
        for (const pending of run.pendingStaging.values()) {
          pending.reject(abortError())
        }
        run.pendingStaging.clear()
        if (activeRun === run) {
          activeRun = null
        }
      }
    },
    cancel() {
      if (activeRun) cancelRun(activeRun)
    },
    resolveStaging
  }
}
