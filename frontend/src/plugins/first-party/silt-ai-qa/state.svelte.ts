// Reactive controller for silt-ai-qa panel + index status (#227).

import type { PluginContext, PluginAIStream } from '../../sdk'
import {
  aiProviderNeedsSetup,
  embeddingProviderNeedsSetup
} from '../../../settings/ai-setup'
import { AIProviderType, AIErrorKind } from '../../../generated/enums'
import { settings as appSettings } from '../../../settings/store.svelte'
import { createConversation, type Conversation } from './conversation'
import { hybridRetrieve, RetrieveError } from './retrieve'
import { buildRAGMessages, NO_RESULTS_MESSAGE, parseCitations } from './rag'
import { resolveSettings } from './settings'
import type { Citation, IndexProgress, QASettings } from './types'
import {
  ensureIndexReady,
  getIndexInfo,
  indexPage,
  metaGet,
  metaSet,
  needsFullRebuildForModel,
  rebuildIndex,
  resetIndexState
} from './embed_index'
import { pushNotification } from '../../../notifications/store.svelte'
import { updatePluginSetting } from '../../../settings/store.svelte'
import { asString } from '../../../lib/asString'

export type PanelStatus =
  | 'idle'
  | 'asking'
  | 'streaming'
  | 'no-results'
  | 'error'
  | 'no-chat-provider'
  | 'no-embedding-provider'
  | 'indexing'

export function createQAController() {
  let settings: QASettings = $state(resolveSettings(null))
  let progress: IndexProgress = $state({
    status: 'idle',
    done: 0,
    total: 0
  })
  let panelStatus: PanelStatus = $state('idle')
  let errorMessage = $state('')
  let answer = $state('')
  let citations: Citation[] = $state([])
  const conversation: Conversation = createConversation()
  // Svelte 5 reactivity bridge: conversation.ts is a plain .ts module (no
  // $state), so its internal messages array mutations are invisible to
  // fine-grained reactivity. This epoch counter bumps on every mutation;
  // the `messages` getter reads it to establish the reactive dependency.
  let conversationEpoch = $state(0)
  const _addUser = conversation.addUser.bind(conversation)
  const _addAssistant = conversation.addAssistant.bind(conversation)
  const _updateLast = conversation.updateLastAssistant.bind(conversation)
  const _clear = conversation.clear.bind(conversation)
  conversation.addUser = (c: string) => {
    _addUser(c)
    conversationEpoch++
  }
  conversation.addAssistant = (c: string, ci?: Citation[]) => {
    _addAssistant(c, ci)
    conversationEpoch++
  }
  conversation.updateLastAssistant = (c: string, ci?: Citation[]) => {
    _updateLast(c, ci)
    conversationEpoch++
  }
  conversation.clear = () => {
    _clear()
    conversationEpoch++
  }
  let activeStream: PluginAIStream | null = null
  let askInFlight = false
  let indexTimer: ReturnType<typeof setTimeout> | null = null
  /** Serializes rebuild / page index so they never interleave. */
  let indexChain: Promise<void> = Promise.resolve()
  let disposed = false
  /** Session-scoped dismiss for the stale-index banner ("Later"). */
  let staleBannerDismissed = $state(false)
  /** Soft toast once per search session when querying a stale index. */
  let staleSearchToasted = false
  /** Last one-sided hybrid failure message (session-scoped; #630). */
  let searchDegradeReason = $state<string | null>(null)

  function loadSettings(_ctx?: PluginContext) {
    const raw = appSettings.config?.plugins?.plugin_settings?.['silt-ai-qa']
    settings = resolveSettings(raw)
  }

  function setSettings(next: QASettings) {
    settings = next
  }

  async function setStaleReason(reason: string | null) {
    settings = { ...settings, stale_reason: reason }
    try {
      await updatePluginSetting('silt-ai-qa', 'stale_reason', reason)
    } catch (e) {
      console.warn('silt-ai-qa: failed to persist stale_reason:', e)
    }
    if (reason) staleBannerDismissed = false
  }

  function dismissStaleBanner() {
    staleBannerDismissed = true
  }

  async function checkTaskTypeMigration(ctx: PluginContext) {
    const providerType = String(
      appSettings.config?.ai?.embedding?.provider_type ?? ''
    )
    if (providerType !== AIProviderType.ProviderGoogle) return
    try {
      // task_type_used records whether the index was built with Google's
      // document/query task-type asymmetry. Missing or 'none' on a Google
      // provider means the index predates #610 and should be rebuilt.
      const used = await metaGet(ctx, 'task_type_used')
      if (used !== 'asymmetric') {
        if (!settings.stale_reason) {
          await setStaleReason(
            'Search index format updated — rebuild for best results'
          )
        }
      }
    } catch {
      /* ignore */
    }
  }

  /** Stamp whether this index used Google's task-type asymmetry. */
  async function stampTaskTypeMeta(ctx: PluginContext) {
    const providerType = String(
      appSettings.config?.ai?.embedding?.provider_type ?? ''
    )
    try {
      await metaSet(
        ctx,
        'task_type_used',
        providerType === AIProviderType.ProviderGoogle ? 'asymmetric' : 'none'
      )
    } catch {
      /* best-effort */
    }
  }

  function chatReady(): boolean {
    return !aiProviderNeedsSetup(appSettings.config?.ai?.chat)
  }

  function embedReady(): boolean {
    return !embeddingProviderNeedsSetup(appSettings.config?.ai?.embedding)
  }

  function configuredEmbedModel(): string {
    return String(appSettings.config?.ai?.embedding?.model ?? '').trim()
  }

  async function refreshIndexInfo(ctx: PluginContext) {
    try {
      const info = await getIndexInfo(ctx)
      progress = {
        ...progress,
        status:
          info.chunkCount > 0
            ? 'ready'
            : progress.status === 'indexing'
              ? 'indexing'
              : info.chunkCount === 0
                ? 'idle'
                : progress.status,
        model: info.model,
        dimensions: info.dimensions,
        chunkCount: info.chunkCount
      }
    } catch {
      /* ignore */
    }
  }

  function runIndexJob(job: () => Promise<void>): Promise<void> {
    const next = indexChain.then(job, job)
    // Keep the chain alive even if a job fails.
    indexChain = next.catch(() => {})
    return next
  }

  async function rebuild(ctx: PluginContext) {
    if (!embedReady()) {
      progress = {
        status: 'unconfigured',
        done: 0,
        total: 0,
        message: 'Configure a search model in Settings → AI'
      }
      return
    }
    await runIndexJob(async () => {
      if (disposed) return
      try {
        await rebuildIndex(ctx, settings, (p) => {
          if (!disposed) progress = p
        })
        if (!disposed) {
          await stampTaskTypeMeta(ctx)
          await setStaleReason(null)
          staleBannerDismissed = false
          staleSearchToasted = false
        }
      } catch (e: unknown) {
        if (!disposed) {
          progress = {
            status: 'error',
            done: 0,
            total: 0,
            lastError: e instanceof Error ? e.message : String(e)
          }
        }
        throw e
      }
    })
  }

  /**
   * On vault open / config change: ensure schema, rebuild if empty or model
   * mismatch, otherwise refresh status.
   */
  async function ensureIndex(ctx: PluginContext) {
    if (!embedReady()) {
      progress = {
        status: 'unconfigured',
        done: 0,
        total: 0,
        message: 'Configure a search model in Settings → AI'
      }
      return
    }
    await runIndexJob(async () => {
      if (disposed) return
      try {
        await ensureIndexReady(ctx)
        await checkTaskTypeMigration(ctx)
        const model = configuredEmbedModel()
        const info = await getIndexInfo(ctx)
        const mustRebuild =
          info.chunkCount === 0 ||
          (await needsFullRebuildForModel(ctx, model, info.dimensions))
        if (mustRebuild) {
          await rebuildIndex(ctx, settings, (p) => {
            if (!disposed) progress = p
          })
          if (!disposed) {
            await stampTaskTypeMeta(ctx)
            await setStaleReason(null)
            staleSearchToasted = false
          }
        } else {
          progress = {
            status: 'ready',
            done: 0,
            total: 0,
            model: info.model,
            dimensions: info.dimensions,
            chunkCount: info.chunkCount,
            message: `Indexed ${info.chunkCount} notes`
          }
        }
      } catch (e: unknown) {
        if (!disposed) {
          progress = {
            status: 'error',
            done: 0,
            total: 0,
            lastError: e instanceof Error ? e.message : String(e)
          }
        }
      }
    })
  }

  function schedulePageIndex(
    ctx: PluginContext,
    notebook: string,
    section: string,
    page: string
  ) {
    if (!settings.auto_reembed || !embedReady() || disposed) return
    if (indexTimer) clearTimeout(indexTimer)
    indexTimer = setTimeout(() => {
      void runIndexJob(async () => {
        if (disposed) return
        try {
          // Model change mid-session: full rebuild instead of partial page.
          const model = configuredEmbedModel()
          if (await needsFullRebuildForModel(ctx, model)) {
            await rebuildIndex(ctx, settings, (p) => {
              if (!disposed) progress = p
            })
            if (!disposed) await stampTaskTypeMeta(ctx)
            return
          }
          await indexPage(ctx, notebook, section, page, settings, (p) => {
            if (!disposed) progress = p
          })
        } catch (e: unknown) {
          if (!disposed) {
            progress = {
              status: 'error',
              done: 0,
              total: 0,
              lastError: e instanceof Error ? e.message : String(e)
            }
          }
        }
      })
    }, settings.reindex_debounce_ms)
  }

  async function ask(ctx: PluginContext, question: string) {
    const q = question.trim()
    if (!q) return
    if (askInFlight) return
    if (!chatReady()) {
      panelStatus = 'no-chat-provider'
      errorMessage = 'Configure a chat model in Settings → AI'
      return
    }
    if (!embedReady()) {
      panelStatus = 'no-embedding-provider'
      errorMessage =
        'Configure a search model in Settings → AI to build the search index'
      return
    }

    askInFlight = true
    conversation.addUser(q)
    answer = ''
    // Keep prior citations visible until a new answer lands.
    panelStatus = 'asking'
    errorMessage = ''
    // Clear prior degrade so a healthy search does not keep showing the banner.
    searchDegradeReason = null
    let assistantStarted = false

    try {
      if (
        settings.stale_reason &&
        !staleSearchToasted &&
        !staleBannerDismissed
      ) {
        staleSearchToasted = true
        pushNotification({
          kind: 'info',
          message: 'Search index is outdated; results may be less accurate.'
        })
      }
      const passages = await hybridRetrieve(ctx, q, settings, (info) => {
        searchDegradeReason = info.message
        void ctx.ai.auditEvent?.({
          kind: 'search_degraded',
          side: info.side,
          status: 'degraded'
        })
      })
      if (passages.length === 0) {
        panelStatus = 'no-results'
        answer = NO_RESULTS_MESSAGE
        conversation.addAssistant(NO_RESULTS_MESSAGE)
        citations = []
        return
      }

      const messages = buildRAGMessages(
        q,
        passages,
        conversation.getMessages().slice(0, -1)
      )
      panelStatus = 'streaming'

      try {
        const stream = await ctx.ai.complete({
          messages: messages,
          stream: true,
          temperature: 0.3
        })
        activeStream = stream
        conversation.addAssistant('')
        assistantStarted = true
        let acc = ''
        for await (const delta of activeStream) {
          acc += delta
          answer = acc
          conversation.updateLastAssistant(acc)
        }
        const final = await activeStream.result()
        answer = final.content || acc
        citations = parseCitations(answer, passages)
        conversation.updateLastAssistant(answer, citations)
        panelStatus = 'idle'
      } catch (streamErr: unknown) {
        // Fallback to non-stream if provider rejects streaming.
        if (
          asString((streamErr as { code?: unknown } | null)?.code).includes(
            AIErrorKind.ErrBadRequest
          ) ||
          /stream/i.test(
            asString((streamErr as { message?: unknown } | null)?.message)
          )
        ) {
          const res = await ctx.ai.complete({
            messages: messages,
            temperature: 0.3
          })
          answer = res.content
          citations = parseCitations(answer, passages)
          if (assistantStarted) {
            conversation.updateLastAssistant(answer, citations)
          } else {
            conversation.addAssistant(answer, citations)
            assistantStarted = true
          }
          panelStatus = 'idle'
          return
        }
        throw streamErr
      } finally {
        activeStream = null
      }
    } catch (e: unknown) {
      panelStatus = 'error'
      if (e instanceof RetrieveError) {
        errorMessage = e instanceof Error ? e.message : String(e)
      } else {
        errorMessage =
          e && typeof e === 'object' && 'message' in e
            ? String(e.message)
            : String(e)
      }
      const errText = `Error: ${errorMessage}`
      answer = errText
      if (assistantStarted) {
        conversation.updateLastAssistant(errText)
      } else {
        conversation.addAssistant(errText)
      }
    } finally {
      askInFlight = false
    }
  }

  async function stop() {
    if (activeStream) {
      try {
        await activeStream.cancel()
      } catch {
        /* ignore */
      }
      activeStream = null
      if (panelStatus === 'streaming' || panelStatus === 'asking') {
        panelStatus = 'idle'
      }
    }
  }

  function clear() {
    conversation.clear()
    answer = ''
    citations = []
    panelStatus = 'idle'
    errorMessage = ''
  }

  function dispose() {
    disposed = true
    if (indexTimer) clearTimeout(indexTimer)
    indexTimer = null
    void stop()
    resetIndexState()
  }

  return {
    get settings() {
      return settings
    },
    get progress() {
      return progress
    },
    get panelStatus() {
      return panelStatus
    },
    get errorMessage() {
      return errorMessage
    },
    get answer() {
      return answer
    },
    get citations() {
      return citations
    },
    get messages() {
      void conversationEpoch
      return conversation.getMessages()
    },
    get askInFlight() {
      return askInFlight
    },
    get staleBannerDismissed() {
      return staleBannerDismissed
    },
    get showStaleBanner() {
      return Boolean(settings.stale_reason) && !staleBannerDismissed
    },
    get searchDegradeReason() {
      return searchDegradeReason
    },
    clearSearchDegrade() {
      searchDegradeReason = null
    },
    loadSettings,
    setSettings,
    setStaleReason,
    dismissStaleBanner,
    chatReady,
    embedReady,
    refreshIndexInfo,
    rebuild,
    ensureIndex,
    schedulePageIndex,
    ask,
    stop,
    clear,
    dispose
  }
}

export type QAController = ReturnType<typeof createQAController>

let controller: QAController | null = null

/** Reactive flag for chrome (title-bar toggle) — true while the plugin is loaded. */
export const aiAssistantChrome = $state({
  available: false
})

export function getQAController(): QAController | null {
  return controller
}

export function setQAController(c: QAController | null) {
  controller = c
  aiAssistantChrome.available = c != null
}
