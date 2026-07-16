// Reactive controller for the AI Agent chat surface (#596).
//
// Owns the message list (user, assistant text, and transparent tool-call
// cards), the running flag, and the send/cancel entry points. send() drives
// the agent loop (agent-loop.ts); each tool call + result is pushed into the
// message list so the UX can render the agent's reasoning transparently.
//
// Phase 5 staging (#605): when a tool returns a staged result, the loop pauses
// and `pendingStaging` holds the event until the UX calls resolveStaging. The
// input is disabled while staging is pending so the user cannot enqueue a new
// turn before resolving the confirmation.

import type { PluginAIChatMessage, PluginContext } from '../../sdk'
import { aiProviderNeedsSetup } from '../../../settings/ai-setup'
import { settings as appSettings } from '../../../settings/store.svelte'
import {
  createAgentSession,
  type AgentOptions,
  type AgentSession,
  type StagingEvent
} from './agent-loop'
import { clearTools } from './tool-registry'

/** A rendered chat message. Tool calls/results are carried inline so the
 *  surface can show them as cards between the user prompt and the answer. */
export interface AgentMessage {
  id: string
  role: 'user' | 'assistant' | 'tool'
  content: string
  /** Present on assistant turns that requested a tool call. */
  toolCall?: { id: string; name: string; args: Record<string, unknown> }
  /** Present on tool-result turns (correlated by toolCallId). */
  toolResult?: {
    toolCallId: string
    name: string
    content: string
    error?: string
    truncated: boolean
  }
}

let _idCounter = 0
function nextId(): string {
  _idCounter += 1
  return `m${_idCounter}`
}

export function createAgentController() {
  let messages = $state<AgentMessage[]>([])
  // The rendered list deliberately omits the provider-facing assistant
  // tool_calls message. Keep that protocol transcript separately so every
  // tool result remains correlated on subsequent turns.
  let transcript = $state<PluginAIChatMessage[]>([])
  let running = $state(false)
  /**
   * Pending staged operation the user must confirm or reject. Set when the
   * agent loop emits onStaging; cleared when the UX calls resolveStaging.
   * Null when no destructive op is awaiting confirmation.
   */
  let pendingStaging = $state<StagingEvent | null>(null)
  let ctxRef: PluginContext | null = null
  let session: AgentSession | null = null

  function providerReady(): boolean {
    return !aiProviderNeedsSetup(appSettings.config?.ai?.chat as any)
  }

  /** Return the canonical provider-facing transcript, excluding system. */
  function toHistory(): PluginAIChatMessage[] {
    return transcript.map((message) => ({
      ...message,
      ...(message.tool_calls
        ? { tool_calls: message.tool_calls.map((call) => ({ ...call })) }
        : {})
    }))
  }

  /** Wire the controller to a PluginContext (set on vault open). */
  function attach(ctx: PluginContext) {
    ctxRef = ctx
    session = createAgentSession(ctx)
  }

  function detach() {
    session?.cancel()
    session = null
    ctxRef = null
  }

  async function send(ctx: PluginContext, text: string) {
    const q = text.trim()
    if (!q || running) return
    // Lazily attach if the hub passed a context before onVaultOpen wired
    // the controller (e.g. in tests).
    if (!session && ctx) attach(ctx)
    if (!session) return

    const priorHistory = toHistory()
    const userMsg: AgentMessage = { id: nextId(), role: 'user', content: q }
    messages = [...messages, userMsg]
    transcript = [...transcript, { role: 'user', content: q }]
    running = true

    const appendAssistant = (content: string) => {
      if (!content) return false
      messages = [...messages, { id: nextId(), role: 'assistant', content }]
      transcript = [...transcript, { role: 'assistant', content }]
      return true
    }

    let streamedAssistantId: string | null = null
    const updateStreamedAssistant = (content: string) => {
      if (!content) return
      if (!streamedAssistantId) {
        streamedAssistantId = nextId()
        messages = [
          ...messages,
          { id: streamedAssistantId, role: 'assistant', content }
        ]
        return
      }
      messages = messages.map((message) =>
        message.id === streamedAssistantId ? { ...message, content } : message
      )
    }

    const finishAssistant = (content: string) => {
      if (!content) return false
      if (!streamedAssistantId) return appendAssistant(content)
      messages = messages.map((message) =>
        message.id === streamedAssistantId ? { ...message, content } : message
      )
      transcript = [...transcript, { role: 'assistant', content }]
      streamedAssistantId = null
      return true
    }

    const appendToolResult = (result: {
      id: string
      name: string
      content: string
      error?: string
      truncated?: boolean
    }) => {
      messages = [
        ...messages,
        {
          id: nextId(),
          role: 'tool',
          content: '',
          toolResult: {
            toolCallId: result.id,
            name: result.name,
            content: result.content,
            error: result.error,
            truncated: result.truncated ?? false
          }
        }
      ]
    }

    let finalTextShown = false

    const opts: AgentOptions = {
      // A streamed assistant message is created lazily. If the stream later
      // proves to be a tool-calling turn, remove that provisional text before
      // the tool cards are appended; final text remains live in the UI.
      onAssistantText: (_chunk, acc) => updateStreamedAssistant(acc),
      onAssistantToolCalls: (calls, content) => {
        if (!calls?.length) return
        if (streamedAssistantId) {
          messages = messages.filter(
            (message) => message.id !== streamedAssistantId
          )
          streamedAssistantId = null
        }
        transcript = [
          ...transcript,
          { role: 'assistant', content, tool_calls: calls }
        ]
      },
      onToolCall: (call) => {
        messages = [
          ...messages,
          {
            id: nextId(),
            role: 'tool',
            content: '',
            toolCall: { id: call.id, name: call.name, args: call.args }
          }
        ]
      },
      onToolMessage: (result) => {
        transcript = [
          ...transcript,
          { role: 'tool', tool_call_id: result.id, content: result.content }
        ]
        appendToolResult(result)
      },
      onDone: (finalText) => {
        finalTextShown = finishAssistant(finalText)
      },
      onStaging: (event) => {
        // Surface to the UX; the loop blocks until resolveStaging().
        pendingStaging = event
      }
    }

    try {
      const res = await session.run(q, priorHistory, opts)
      if (res.cancelled) {
        finishAssistant(`${res.text ? `${res.text} ` : ''}[stopped]`)
      } else if (res.hitIterationCap) {
        finishAssistant(
          `${res.text ? `${res.text}\n\n` : ''}[reached iteration limit]`
        )
      } else if (!finalTextShown) {
        // Keep the controller usable with a session implementation that
        // returns text without invoking onDone (notably small test doubles).
        appendAssistant(res.text)
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      finishAssistant(`Error: ${msg}`)
    } finally {
      running = false
      pendingStaging = null
    }
  }

  /**
   * Resolve a pending staged op (Phase 5). Forwards to the agent session,
   * which unblocks the loop and either commits (confirmed) or rejects
   * (rejected → "rejected by user" surfaces to the model). Clears
   * pendingStaging; the loop's finally block is the source of truth so a
   * second stage in the same turn re-populates it.
   */
  function resolveStaging(token: string, confirmed: boolean) {
    pendingStaging = null
    session?.resolveStaging(token, confirmed)
  }

  function cancel() {
    session?.cancel()
  }

  function clear() {
    if (running) session?.cancel()
    messages = []
    transcript = []
    pendingStaging = null
  }

  function dispose() {
    detach()
    clearTools()
    messages = []
    transcript = []
    pendingStaging = null
  }

  return {
    attach,
    detach,
    send,
    cancel,
    clear,
    dispose,
    resolveStaging,
    providerReady,
    get messages() {
      return messages
    },
    get running() {
      return running
    },
    get pendingStaging() {
      return pendingStaging
    }
  }
}

export type AgentController = ReturnType<typeof createAgentController>

let controller: AgentController | null = null

export function getAgentController(): AgentController | null {
  return controller
}

export function setAgentController(c: AgentController | null) {
  controller = c
}
