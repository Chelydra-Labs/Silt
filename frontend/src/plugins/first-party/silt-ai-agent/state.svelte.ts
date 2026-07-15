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
  let running = $state(false)
  /**
   * Pending staged operation the user must confirm or reject. Set when the
   * agent loop emits onStaging; cleared when the UX calls resolveStaging.
   * Null when no destructive op is awaiting confirmation.
   */
  let pendingStaging = $state<StagingEvent | null>(null)
  let ctxRef: PluginContext | null = null
  let session: AgentSession | null = null

  /** Convert the rendered message list into the chat-history shape the loop
   *  consumes (user + assistant + prior tool turns, excluding the
   *  system prompt the loop prepends itself). */
  function toHistory(): PluginAIChatMessage[] {
    const out: PluginAIChatMessage[] = []
    for (const m of messages) {
      if (m.role === 'user') {
        out.push({ role: 'user', content: m.content })
      } else if (m.role === 'assistant') {
        if (m.toolCall) {
          out.push({
            role: 'assistant',
            content: m.content,
            tool_calls: [
              {
                id: m.toolCall.id,
                name: m.toolCall.name,
                arguments: m.toolCall.args
              }
            ]
          })
        } else {
          out.push({ role: 'assistant', content: m.content })
        }
      } else if (m.role === 'tool' && m.toolResult) {
        out.push({
          role: 'tool',
          tool_call_id: m.toolResult.toolCallId,
          content: m.toolResult.error
            ? `Error: ${m.toolResult.error}`
            : m.toolResult.content
        })
      }
    }
    return out
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
    if (!q || running || !session) {
      // Lazily attach if the hub passed a context before onVaultOpen wired
      // the controller (e.g. in tests).
      if (!session && ctx) attach(ctx)
      if (!session) return
    }
    const userMsg: AgentMessage = { id: nextId(), role: 'user', content: q }
    messages = [...messages, userMsg]
    running = true
    const assistantId = nextId()
    messages = [
      ...messages,
      { id: assistantId, role: 'assistant', content: '' }
    ]

    const updateAssistant = (content: string) => {
      messages = messages.map((m) =>
        m.id === assistantId ? { ...m, content } : m
      )
    }

    const opts: AgentOptions = {
      onAssistantText: (_chunk, acc) => updateAssistant(acc),
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
      onToolResult: (r) => {
        const truncated = r.result.content.length > 10 * 1024
        messages = [
          ...messages,
          {
            id: nextId(),
            role: 'tool',
            content: '',
            toolResult: {
              toolCallId: r.id,
              name: r.name,
              content: r.result.content,
              error: r.result.error,
              truncated
            }
          }
        ]
      },
      onStaging: (event) => {
        // Surface to the UX; the loop blocks until resolveStaging().
        pendingStaging = event
      },
      onError: (err) => {
        const msg = err instanceof Error ? err.message : String(err)
        updateAssistant(`Error: ${msg}`)
      }
    }

    try {
      const res = await session.run(q, toHistory().slice(0, -2), opts)
      if (res.cancelled) {
        updateAssistant(
          messages.find((m) => m.id === assistantId)?.content + ' [stopped]'
        )
      } else if (res.hitIterationCap) {
        updateAssistant(
          (messages.find((m) => m.id === assistantId)?.content ?? '') +
            '\n\n[reached iteration limit]'
        )
      } else {
        updateAssistant(res.text)
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      updateAssistant(`Error: ${msg}`)
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
    pendingStaging = null
  }

  function dispose() {
    detach()
    clearTools()
    messages = []
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
