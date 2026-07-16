import type { PluginAIChatMessage, PluginContext } from '../../sdk'
import { aiProviderNeedsSetup } from '../../../settings/ai-setup'
import { settings as appSettings } from '../../../settings/store.svelte'
import {
  createAgentSession,
  type AgentOptions,
  type AgentSession
} from '../../first-party/silt-ai-agent/agent-loop'
import {
  confirmationEntry,
  evidenceEntry,
  statusEntry,
  textEntry,
  toolCallEntry,
  toolResultEntry,
  type AIChatEntry,
  type ProposalEntry
} from './types'
import { parseCitations } from '../../first-party/silt-ai-qa/rag'
import type { RetrievedPassage } from '../../shared/retrieval/hybrid'
import type { ToolEvidence } from '../../first-party/silt-ai-agent/tool-registry'
import { createWritingCapability } from './capabilities/writing-capability'

export interface AIChatRequestOptions {
  selectionText?: string
  blockId?: string
  instruction?: string
}

export interface AIChatCapabilityContext {
  readonly pluginContext: PluginContext
  readonly transcript: readonly AIChatEntry[]
  readonly request: Readonly<AIChatRequestOptions>
  append: (entry: AIChatEntry) => void
  update: (id: string, update: (entry: AIChatEntry) => AIChatEntry) => void
  remove: (id: string) => void
}

/**
 * Capabilities translate one backend workflow into the shared transcript.
 * Retrieval and writing integrations register implementations of this shape;
 * the shell remains unaware of where an entry came from.
 */
export interface AIChatCapability {
  id: string
  matches?: (text: string) => boolean
  run: (text: string, context: AIChatCapabilityContext) => Promise<void>
  stop?: () => void
  clear?: () => void
  resolveStaging?: (token: string, confirmed: boolean) => void
  acceptProposal?: (proposal: ProposalEntry) => void | Promise<void>
  discardProposal?: (proposal: ProposalEntry) => void | Promise<void>
  attach?: (context: PluginContext) => void
  detach?: () => void
}

export interface CapabilityRegistration {
  makeDefault?: boolean
}

export function createAgentCapability(): AIChatCapability {
  let session: AgentSession | null = null
  let protocolHistory: PluginAIChatMessage[] = []
  let citationPassages: RetrievedPassage[] = []
  const emittedEvidence = new Set<string>()

  return {
    id: 'agent-tools',
    attach(context) {
      session?.cancel()
      session = createAgentSession(context)
    },
    detach() {
      session?.cancel()
      session = null
    },
    async run(text, context) {
      citationPassages = []
      emittedEvidence.clear()
      if (!session) session = createAgentSession(context.pluginContext)
      const priorHistory = protocolHistory.map((message) => ({ ...message }))
      protocolHistory = [...protocolHistory, { role: 'user', content: text }]

      let assistantId: string | null = null
      let finalTextRecorded = false

      const updateAssistant = (content: string, streaming: boolean) => {
        if (!content) return
        if (!assistantId) {
          const entry = textEntry({
            role: 'assistant',
            content,
            streaming
          })
          assistantId = entry.id
          context.append(entry)
          return
        }
        const id = assistantId
        context.update(id, (entry) =>
          entry.kind === 'text' ? { ...entry, content, streaming } : entry
        )
      }

      const options: AgentOptions = {
        onAssistantText: (_chunk, accumulated) =>
          updateAssistant(accumulated, true),
        onAssistantToolCalls: (calls, content) => {
          if (assistantId) context.remove(assistantId)
          assistantId = null
          protocolHistory = [
            ...protocolHistory,
            { role: 'assistant', content, tool_calls: calls }
          ]
        },
        onToolCall: (call) => {
          context.append(
            toolCallEntry({
              role: 'assistant',
              toolCallId: call.id,
              toolName: call.name,
              args: call.args
            })
          )
        },
        onToolResult: ({ result }) => {
          for (const evidence of result.evidence ?? []) {
            const key = `${evidence.blockId}:${evidence.citationIndex}`
            citationPassages.push({
              blockId: evidence.blockId,
              notebook: evidence.notebook ?? '',
              section: evidence.section ?? '',
              page: evidence.page ?? '',
              lineNumber: evidence.lineNumber ?? 0,
              text: evidence.snippet ?? '',
              score: 0,
              citeIndex: evidence.citationIndex
            })
            if (emittedEvidence.has(key)) continue
            emittedEvidence.add(key)
            context.append(
              evidenceEntry({
                role: 'assistant',
                citationIndex: evidence.citationIndex,
                target: {
                  blockId: evidence.blockId,
                  notebook: evidence.notebook,
                  section: evidence.section,
                  page: evidence.page
                },
                title:
                  evidence.title ||
                  [evidence.notebook, evidence.section, evidence.page]
                    .filter(Boolean)
                    .join(' > ') ||
                  evidence.blockId,
                excerpt: evidence.snippet
              })
            )
          }
        },
        onToolMessage: (result) => {
          protocolHistory = [
            ...protocolHistory,
            { role: 'tool', tool_call_id: result.id, content: result.content }
          ]
          context.append(
            toolResultEntry({
              role: 'system',
              toolCallId: result.id,
              toolName: result.name,
              output: result.content,
              error: result.error,
              truncated: result.truncated
            })
          )
        },
        onStaging: (event) => {
          context.append(
            confirmationEntry({
              role: 'system',
              token: event.token,
              operation: event.preview.kind.replace(/_/g, ' '),
              summary: event.preview.summary,
              details: event.preview.details,
              affectedCount: event.preview.affectedCount,
              state: 'pending'
            })
          )
        },
        onDone: (finalText) => {
          // Keep citation numbering identical to the retrieval prompt. The
          // parser deliberately drops unknown markers, matching Q&A behavior.
          parseCitations(finalText, citationPassages)
          updateAssistant(finalText, false)
          if (finalText) {
            protocolHistory = [
              ...protocolHistory,
              { role: 'assistant', content: finalText }
            ]
            finalTextRecorded = true
          }
        }
      }

      const result = await session.run(text, priorHistory, options)
      if (result.cancelled) return
      if (result.hitIterationCap) {
        context.append(
          statusEntry({
            role: 'system',
            status: 'iteration-limit',
            message: 'Stopped after reaching the iteration limit.'
          })
        )
      }
      if (!finalTextRecorded && result.text) {
        updateAssistant(result.text, false)
        protocolHistory = [
          ...protocolHistory,
          { role: 'assistant', content: result.text }
        ]
      }
    },
    stop() {
      session?.cancel()
    },
    resolveStaging(token, confirmed) {
      session?.resolveStaging(token, confirmed)
    },
    clear() {
      session?.cancel()
      protocolHistory = []
      citationPassages = []
      emittedEvidence.clear()
    }
  }
}

export function createAIChatController(initialContext?: PluginContext) {
  let transcript = $state<AIChatEntry[]>([])
  let busy = $state(false)
  let pendingConfirmation = $state<string | null>(null)
  let context = initialContext ?? null
  let activeCapability: AIChatCapability | null = null
  const capabilities = new Map<string, AIChatCapability>()
  const entryOwners = new Map<string, string>()
  let defaultCapabilityId = 'agent-tools'
  const providerReady = $derived(
    !aiProviderNeedsSetup(appSettings.config?.ai?.chat as any)
  )
  const pendingProposal = $derived(
    transcript.find(
      (entry) =>
        entry.kind === 'proposal' && (entry.state ?? 'pending') === 'pending'
    )?.id ?? null
  )

  function append(entry: AIChatEntry) {
    transcript = [...transcript, entry]
    if (activeCapability) entryOwners.set(entry.id, activeCapability.id)
    if (entry.kind === 'confirmation' && entry.state === 'pending') {
      pendingConfirmation = entry.token
    }
  }

  function update(id: string, updater: (entry: AIChatEntry) => AIChatEntry) {
    transcript = transcript.map((entry) =>
      entry.id === id ? updater(entry) : entry
    )
  }

  function remove(id: string) {
    transcript = transcript.filter((entry) => entry.id !== id)
    entryOwners.delete(id)
  }

  function registerCapability(
    capability: AIChatCapability,
    registration: CapabilityRegistration = {}
  ) {
    capabilities.get(capability.id)?.detach?.()
    capabilities.set(capability.id, capability)
    if (registration.makeDefault) defaultCapabilityId = capability.id
    if (context) capability.attach?.(context)
    return () => {
      if (capabilities.get(capability.id) !== capability) return
      capability.detach?.()
      capabilities.delete(capability.id)
    }
  }

  function attach(nextContext: PluginContext) {
    if (context === nextContext) return
    for (const capability of capabilities.values()) capability.detach?.()
    context = nextContext
    for (const capability of capabilities.values())
      capability.attach?.(nextContext)
  }

  function pickCapability(text: string): AIChatCapability | undefined {
    for (const capability of capabilities.values()) {
      if (capability.id !== defaultCapabilityId && capability.matches?.(text)) {
        return capability
      }
    }
    return capabilities.get(defaultCapabilityId)
  }

  async function send(text: string, request: AIChatRequestOptions = {}) {
    const prompt = text.trim()
    if (!prompt || busy || pendingConfirmation || !context || !providerReady)
      return

    const capability = pickCapability(prompt)
    if (!capability) return
    append(textEntry({ role: 'user', content: prompt }))
    const runningStatus = statusEntry({
      role: 'system',
      status: 'thinking',
      message: 'Thinking…'
    })
    append(runningStatus)
    busy = true
    activeCapability = capability

    try {
      await capability.run(prompt, {
        pluginContext: context,
        request,
        get transcript() {
          return transcript
        },
        append,
        update,
        remove
      })
    } catch (error) {
      append(
        statusEntry({
          role: 'system',
          status: 'error',
          message: error instanceof Error ? error.message : String(error)
        })
      )
    } finally {
      remove(runningStatus.id)
      busy = false
      activeCapability = null
    }
  }

  function stop() {
    if (!busy) return
    activeCapability?.stop?.()
    append(
      statusEntry({
        role: 'system',
        status: 'stopped',
        message: 'Stopped by you.'
      })
    )
  }

  function clear() {
    activeCapability?.stop?.()
    for (const capability of capabilities.values()) capability.clear?.()
    transcript = []
    entryOwners.clear()
    pendingConfirmation = null
    busy = false
    activeCapability = null
  }

  function resolveStaging(token: string, confirmed: boolean) {
    const confirmation = transcript.find(
      (entry) => entry.kind === 'confirmation' && entry.token === token
    )
    if (confirmation) {
      update(confirmation.id, (entry) =>
        entry.kind === 'confirmation'
          ? { ...entry, state: confirmed ? 'confirmed' : 'rejected' }
          : entry
      )
    }
    pendingConfirmation = null
    ;(
      activeCapability ?? capabilities.get(defaultCapabilityId)
    )?.resolveStaging?.(token, confirmed)
  }

  async function acceptProposal(id: string) {
    const proposal = transcript.find(
      (entry): entry is ProposalEntry =>
        entry.id === id && entry.kind === 'proposal'
    )
    if (!proposal || proposal.state === 'accepted') return
    const owner = entryOwners.get(id)
    await (owner ? capabilities.get(owner) : undefined)?.acceptProposal?.(
      proposal
    )
    update(id, (entry) =>
      entry.kind === 'proposal' ? { ...entry, state: 'accepted' } : entry
    )
  }

  async function discardProposal(id: string) {
    const proposal = transcript.find(
      (entry): entry is ProposalEntry =>
        entry.id === id && entry.kind === 'proposal'
    )
    if (!proposal || proposal.state === 'discarded') return
    const owner = entryOwners.get(id)
    await (owner ? capabilities.get(owner) : undefined)?.discardProposal?.(
      proposal
    )
    update(id, (entry) =>
      entry.kind === 'proposal' ? { ...entry, state: 'discarded' } : entry
    )
  }

  function dispose() {
    clear()
    for (const capability of capabilities.values()) capability.detach?.()
    capabilities.clear()
    context = null
  }

  registerCapability(createAgentCapability(), { makeDefault: true })

  registerCapability(createWritingCapability())

  return {
    attach,
    dispose,
    registerCapability,
    send,
    stop,
    clear,
    resolveStaging,
    acceptProposal,
    discardProposal,
    get transcript() {
      return transcript
    },
    get busy() {
      return busy
    },
    get providerReady() {
      return providerReady
    },
    get pendingConfirmation() {
      return pendingConfirmation
    },
    get pendingProposal() {
      return pendingProposal
    }
  }
}

export type AIChatController = ReturnType<typeof createAIChatController>
