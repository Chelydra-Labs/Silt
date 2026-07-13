// Reactive controller for Writing Assistant (#230–#232).

import type { PluginContext } from '../../sdk'
import {
  aiProviderNeedsSetup,
  embeddingProviderNeedsSetup
} from '../../../settings/ai-setup'
import { settings as appSettings } from '../../../settings/store.svelte'
import { runExtractTasks } from './actions/extractTasks'
import { runSuggestRelated } from './actions/suggestRelated'
import { runSuggestTags } from './actions/suggestTags'
import { runWritingAction } from './actions/writing'
import type { StreamSession } from './actions/runChat'
import { enabledActions, isActionEnabled } from './catalog'
import { formatAIError, isAbortError } from './errors'
import { applyProposal } from './proposal/apply'
import { resolveSettings } from './settings'
import { buildScope } from './scope'
import type {
  ActionId,
  AssistantSettings,
  PanelStatus,
  Proposal
} from './types'
import { openWritingAssistantDrawer } from './drawer.svelte'

export type RunOpts = {
  selectionText?: string
  blockId?: string
  instruction?: string
}

export function createAssistantController() {
  let settings: AssistantSettings = $state(resolveSettings(null))
  let panelStatus: PanelStatus = $state('idle')
  let errorMessage = $state('')
  let statusDetail = $state('')
  let streamText = $state('')
  let proposal: Proposal | null = $state(null)
  let instruction = $state('')
  let selectedAction: ActionId = $state('draft-expand')
  let runInFlight = false
  let disposed = false
  let runGeneration = 0
  let cancelRequested = false
  let streamSession: StreamSession | null = null
  let lastRun: { actionId: ActionId; opts: RunOpts } | null = null
  let appliedClearTimer: ReturnType<typeof setTimeout> | null = null

  function loadSettings() {
    const raw = (appSettings.config?.plugins?.plugin_settings as any)?.[
      'silt-ai-assistant'
    ] as Record<string, unknown> | undefined
    settings = resolveSettings(raw)
    const enabled = enabledActions(settings)
    if (!enabled.find((a) => a.id === selectedAction) && enabled[0]) {
      selectedAction = enabled[0].id
    }
  }

  function setSettings(next: AssistantSettings) {
    settings = next
  }

  function chatReady(): boolean {
    return !aiProviderNeedsSetup(appSettings.config?.ai?.chat as any)
  }

  function embedReady(): boolean {
    return !embeddingProviderNeedsSetup(
      appSettings.config?.ai?.embedding as any
    )
  }

  function cancelActiveStream() {
    cancelRequested = true
    streamSession?.cancel()
    streamSession = null
  }

  function discard() {
    cancelActiveStream()
    runGeneration++
    proposal = null
    streamText = ''
    panelStatus = 'idle'
    errorMessage = ''
    statusDetail = ''
    runInFlight = false
  }

  async function run(
    ctx: PluginContext,
    actionId: ActionId,
    opts: RunOpts = {}
  ) {
    if (disposed) return
    // Allow re-run: cancel prior work first.
    if (runInFlight) {
      cancelActiveStream()
    }

    loadSettings()

    if (!isActionEnabled(settings, actionId)) {
      panelStatus = 'error'
      errorMessage = 'That action is disabled in Writing Assistant settings.'
      return
    }

    const meta = enabledActions(settings).find((a) => a.id === actionId)
    if (meta?.needsChat && !chatReady()) {
      panelStatus = 'no-chat-provider'
      errorMessage = 'Configure a chat model in Settings → AI Provider.'
      return
    }
    if (meta?.needsEmbed && !embedReady()) {
      panelStatus = 'no-embedding-provider'
      errorMessage = 'Configure an embedding model in Settings → AI Provider.'
      return
    }

    const gen = ++runGeneration
    cancelRequested = false
    streamSession = null
    runInFlight = true
    selectedAction = actionId
    proposal = null
    streamText = ''
    errorMessage = ''
    statusDetail = ''
    panelStatus = 'running'
    lastRun = {
      actionId,
      opts: {
        selectionText: opts.selectionText,
        blockId: opts.blockId,
        instruction: opts.instruction ?? instruction
      }
    }
    openWritingAssistantDrawer()

    try {
      const scope = await buildScope(ctx, settings, {
        selectionText: opts.selectionText,
        blockId: opts.blockId,
        instruction: opts.instruction ?? instruction
      })

      if (gen !== runGeneration || disposed) return

      if (actionId === 'draft-expand') {
        if (
          !(opts.instruction ?? instruction).trim() &&
          !scope.inputText.trim()
        ) {
          panelStatus = 'no-input'
          errorMessage =
            'Enter a short description or select text to draft from.'
          return
        }
      } else if (!scope.inputText.trim()) {
        panelStatus = 'no-input'
        errorMessage = 'Select text or open a note with content first.'
        return
      }

      let result: Proposal
      switch (actionId) {
        case 'draft-expand':
        case 'rewrite-succinct':
        case 'improve-clarity':
          panelStatus = 'streaming'
          result = await runWritingAction(ctx, actionId, scope, settings, {
            instruction: opts.instruction ?? instruction,
            onStream: (full) => {
              if (gen === runGeneration) streamText = full
            },
            onSession: (session) => {
              streamSession = session
            },
            isCancelled: () =>
              cancelRequested || gen !== runGeneration || disposed
          })
          break
        case 'extract-tasks':
          result = await runExtractTasks(ctx, scope, settings)
          break
        case 'suggest-tags':
          result = await runSuggestTags(ctx, scope, settings)
          break
        case 'suggest-related':
          result = await runSuggestRelated(ctx, scope, settings)
          break
        default:
          throw new Error(`Unknown action: ${actionId}`)
      }

      if (gen !== runGeneration || disposed || cancelRequested) return
      proposal = result
      streamText = result.proposedMarkdown
      if (result.status === 'error') {
        panelStatus = 'error'
        errorMessage = result.errorMessage || 'Action failed'
      } else {
        panelStatus = 'ready'
      }
    } catch (e) {
      if (gen !== runGeneration || disposed) return
      if (isAbortError(e) || cancelRequested) {
        panelStatus = 'cancelled'
        errorMessage = ''
        statusDetail = 'Cancelled.'
        proposal = null
        streamText = ''
        return
      }
      panelStatus = 'error'
      errorMessage = formatAIError(e)
      proposal = null
    } finally {
      if (gen === runGeneration) {
        runInFlight = false
        streamSession = null
      }
    }
  }

  async function regenerate(ctx: PluginContext) {
    if (!lastRun) return
    await run(ctx, lastRun.actionId, lastRun.opts)
  }

  async function accept(ctx: PluginContext): Promise<boolean> {
    if (!proposal || proposal.status !== 'ready') return false
    const res = await applyProposal(ctx, proposal)
    if (!res.ok) {
      panelStatus = 'error'
      errorMessage = res.error
      return false
    }
    panelStatus = 'applied'
    statusDetail = res.detail || 'Applied.'
    streamText = ''
    proposal = null
    if (appliedClearTimer) clearTimeout(appliedClearTimer)
    appliedClearTimer = setTimeout(() => {
      if (panelStatus === 'applied') {
        panelStatus = 'idle'
        statusDetail = ''
      }
    }, 2500)
    return true
  }

  function toggleTag(tag: string) {
    if (!proposal?.tags) return
    const set = new Set(proposal.selectedTags ?? [])
    if (set.has(tag)) set.delete(tag)
    else set.add(tag)
    proposal = { ...proposal, selectedTags: [...set] }
  }

  function toggleRelated(id: string) {
    if (!proposal?.related) return
    const set = new Set(proposal.selectedRelatedIds ?? [])
    if (set.has(id)) set.delete(id)
    else set.add(id)
    proposal = { ...proposal, selectedRelatedIds: [...set] }
  }

  function dispose() {
    disposed = true
    cancelActiveStream()
    runGeneration++
    proposal = null
    streamText = ''
    if (appliedClearTimer) clearTimeout(appliedClearTimer)
  }

  return {
    get settings() {
      return settings
    },
    get panelStatus() {
      return panelStatus
    },
    get errorMessage() {
      return errorMessage
    },
    get statusDetail() {
      return statusDetail
    },
    get streamText() {
      return streamText
    },
    get proposal() {
      return proposal
    },
    get instruction() {
      return instruction
    },
    set instruction(v: string) {
      instruction = v
    },
    get selectedAction() {
      return selectedAction
    },
    set selectedAction(v: ActionId) {
      selectedAction = v
    },
    get lastRun() {
      return lastRun
    },
    loadSettings,
    setSettings,
    chatReady,
    embedReady,
    run,
    regenerate,
    accept,
    discard,
    cancelActiveStream,
    toggleTag,
    toggleRelated,
    dispose
  }
}

export type AssistantController = ReturnType<typeof createAssistantController>

let controller: AssistantController | null = $state(null)

export function setAssistantController(c: AssistantController | null) {
  controller = c
}

export function getAssistantController(): AssistantController | null {
  return controller
}

/** Title-bar / chrome availability when plugin is loaded. */
export const writingAssistantChrome = $state({
  available: false
})

export function syncWritingAssistantChrome(c: AssistantController | null) {
  writingAssistantChrome.available = c != null
}
