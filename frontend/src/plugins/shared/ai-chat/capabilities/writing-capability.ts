import type { PluginContext } from '../../../sdk'
import { embeddingProviderNeedsSetup } from '../../../../settings/ai-setup'
import { settings as appSettings } from '../../../../settings/store.svelte'
import {
  ACTION_CATALOG,
  actionById,
  isActionEnabled
} from '../../../first-party/silt-ai-assistant/catalog'
import { runExtractTasks } from '../../../first-party/silt-ai-assistant/actions/extractTasks'
import { runSuggestRelated } from '../../../first-party/silt-ai-assistant/actions/suggestRelated'
import { runSuggestTags } from '../../../first-party/silt-ai-assistant/actions/suggestTags'
import { runWritingAction } from '../../../first-party/silt-ai-assistant/actions/writing'
import type { StreamSession } from '../../../first-party/silt-ai-assistant/actions/runChat'
import { applyProposal } from '../../../first-party/silt-ai-assistant/proposal/apply'
import { resolveSettings } from '../../../first-party/silt-ai-assistant/settings'
import { buildScope } from '../../../first-party/silt-ai-assistant/scope'
import { writingAssistantChrome } from '../../../first-party/silt-ai-assistant/state.svelte'
import type {
  ActionId,
  AssistantSettings,
  Proposal,
  ScopeContext
} from '../../../first-party/silt-ai-assistant/types'
import type {
  AIChatCapability,
  AIChatCapabilityContext
} from '../ai-chat-controller.svelte'
import { proposalEntry, statusEntry, textEntry } from '../types'

export interface WritingCommand {
  actionId: ActionId
  instruction?: string
}

const ACTION_ALIASES: Record<string, ActionId> = {
  draft: 'draft-expand',
  expand: 'draft-expand',
  rewrite: 'rewrite-succinct',
  clarify: 'improve-clarity',
  'extract-tasks': 'extract-tasks',
  'suggest-tags': 'suggest-tags',
  'suggest-related': 'suggest-related'
}

/** Parse only explicit slash intents; ordinary chat remains with the agent. */
export function parseWritingCommand(text: string): WritingCommand | null {
  const match = text.trim().match(/^\/([a-z][a-z-]*)(?:\s+([\s\S]*))?$/i)
  if (!match) return null
  const actionId = ACTION_ALIASES[match[1].toLowerCase()] ?? match[1]
  if (!ACTION_CATALOG.some((action) => action.id === actionId)) return null
  return {
    actionId: actionId as ActionId,
    instruction: match[2]?.trim() || undefined
  }
}

function assistantSettings(): AssistantSettings {
  const raw = (
    appSettingsValue()?.plugins?.plugin_settings as
      Record<string, Record<string, unknown>> | undefined
  )?.['silt-ai-assistant'] as Record<string, unknown> | undefined
  return resolveSettings(raw)
}

function appSettingsValue(): { plugins?: { plugin_settings?: unknown } } {
  return appSettings.config ?? {}
}

function actionTitle(actionId: ActionId): string {
  return actionById(actionId)?.label ?? actionId
}

function hasInput(
  actionId: ActionId,
  scope: ScopeContext,
  instruction?: string
) {
  if (actionId === 'draft-expand') {
    return Boolean(scope.inputText.trim() || instruction?.trim())
  }
  return Boolean(scope.inputText.trim())
}

export function createWritingCapability(): AIChatCapability {
  let attachedContext: PluginContext | null = null
  let streamSession: StreamSession | null = null
  let generation = 0
  let cancelled = false
  const proposals = new Map<string, Proposal>()
  const applying = new Map<string, Promise<void>>()
  const applied = new Set<string>()
  const discarded = new Set<string>()

  async function runAction(
    command: WritingCommand,
    context: AIChatCapabilityContext,
    settings: AssistantSettings,
    scope: ScopeContext,
    runId: number
  ): Promise<Proposal> {
    const opts = {
      instruction: command.instruction,
      onStream: (full: string) => {
        const existing = context.transcript.find(
          (entry) => entry.kind === 'text' && entry.id === streamEntryId
        )
        if (!streamEntryId) {
          const entry = textEntry({
            role: 'assistant',
            content: full,
            streaming: true
          })
          streamEntryId = entry.id
          context.append(entry)
        } else if (existing) {
          context.update(streamEntryId, (entry) =>
            entry.kind === 'text'
              ? { ...entry, content: full, streaming: true }
              : entry
          )
        }
      },
      onSession: (session: StreamSession) => {
        streamSession = session
      },
      isCancelled: () => cancelled || runId !== generation
    }

    switch (command.actionId) {
      case 'draft-expand':
      case 'rewrite-succinct':
      case 'improve-clarity':
        return runWritingAction(
          context.pluginContext,
          command.actionId,
          scope,
          settings,
          opts
        )
      case 'extract-tasks':
        return runExtractTasks(context.pluginContext, scope, settings)
      case 'suggest-tags':
        return runSuggestTags(context.pluginContext, scope, settings)
      case 'suggest-related':
        return runSuggestRelated(context.pluginContext, scope, settings)
    }
  }

  let streamEntryId: string | null = null

  return {
    id: 'writing-proposals',
    matches: (text) =>
      writingAssistantChrome.available && parseWritingCommand(text) !== null,
    attach(context) {
      attachedContext = context
    },
    detach() {
      attachedContext = null
      streamSession = null
    },
    async run(text, context) {
      const command = parseWritingCommand(text)
      if (!command) return

      // Stamp the generation BEFORE the first await (buildScope) so a Stop
      // during preflight is not undone: previously generation was bumped only
      // after buildScope resolved, so stop() during preflight was overwritten
      // by cancelled = false here.
      const runId = ++generation
      cancelled = false

      const settings = assistantSettings()
      if (!isActionEnabled(settings, command.actionId)) {
        throw new Error(
          'That writing action is disabled in Writing Assistant settings.'
        )
      }

      const meta = actionById(command.actionId)
      if (
        meta?.needsEmbed &&
        embeddingProviderNeedsSetup(appSettings.config?.ai?.embedding)
      ) {
        throw new Error('Configure an embedding model in Settings → AI.')
      }

      const request = context.request
      const instruction = command.instruction ?? request.instruction
      const effectiveCommand = { ...command, instruction }
      const scope = await buildScope(context.pluginContext, settings, {
        selectionText: request.selectionText,
        blockId: request.blockId,
        instruction
      })
      // Stop was pressed during preflight — abandon before any side effects.
      if (cancelled || runId !== generation) return
      if (!hasInput(command.actionId, scope, instruction)) {
        throw new Error(
          command.actionId === 'draft-expand'
            ? 'Enter a short description or select text to draft from.'
            : 'Select text or open a note with content first.'
        )
      }

      streamSession = null
      streamEntryId = null
      const actionStatus = statusEntry({
        role: 'system',
        status: 'running',
        message: `${actionTitle(command.actionId)}…`
      })
      context.append(actionStatus)

      try {
        const proposal = await runAction(
          effectiveCommand,
          context,
          settings,
          scope,
          runId
        )
        if (cancelled || runId !== generation) return

        if (proposal.status === 'error') {
          // Finalize the streamed text so the user sees what was generated
          // before the failure; no proposal card is appended on error.
          if (streamEntryId) {
            context.update(streamEntryId, (entry) =>
              entry.kind === 'text'
                ? {
                    ...entry,
                    content: proposal.proposedMarkdown,
                    streaming: false
                  }
                : entry
            )
          }
          context.append(
            statusEntry({
              role: 'system',
              status: 'error',
              message: proposal.errorMessage || 'Writing action failed.'
            })
          )
          return
        }

        // Success: the proposal card carries the same content as the streamed
        // text, so remove the streamed entry to avoid showing it twice.
        if (streamEntryId) {
          context.remove(streamEntryId)
        }

        const entry = proposalEntry({
          role: 'assistant',
          title: actionTitle(command.actionId),
          content: proposal.proposedMarkdown,
          description: proposal.warning
        })
        proposals.set(entry.id, proposal)
        context.append(entry)
      } finally {
        context.remove(actionStatus.id)
        streamSession = null
      }
    },
    stop() {
      cancelled = true
      generation++
      streamSession?.cancel()
      streamSession = null
    },
    clear() {
      cancelled = true
      generation++
      streamSession?.cancel()
      streamSession = null
      proposals.clear()
      applying.clear()
      applied.clear()
      discarded.clear()
    },
    async acceptProposal(entry) {
      if (applied.has(entry.id)) return
      if (discarded.has(entry.id)) throw new Error('Proposal was discarded.')
      const existing = applying.get(entry.id)
      if (existing) return existing

      const proposal = proposals.get(entry.id)
      const ctx = attachedContext
      if (!proposal || !ctx) throw new Error('Proposal is no longer available.')

      const operation = (async () => {
        const result = await applyProposal(ctx, proposal)
        if (!result.ok) throw new Error(result.error)
        applied.add(entry.id)
      })()
      applying.set(entry.id, operation)
      try {
        await operation
      } finally {
        applying.delete(entry.id)
      }
    },
    discardProposal(entry) {
      if (applied.has(entry.id)) return
      discarded.add(entry.id)
      proposals.delete(entry.id)
    }
  }
}
