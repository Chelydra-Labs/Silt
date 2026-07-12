// Reactive controller for silt-ai-qa panel + index status (#227).

import type { PluginContext, PluginAIStream } from '../../sdk'
import {
  aiProviderNeedsSetup,
  embeddingProviderNeedsSetup
} from '../../../settings/ai-setup'
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
  needsFullRebuildForModel,
  rebuildIndex,
  resetIndexState
} from './embed_index'

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
  let conversation: Conversation = createConversation()
  let activeStream: PluginAIStream | null = null
  let askInFlight = false
  let indexTimer: ReturnType<typeof setTimeout> | null = null
  /** Serializes rebuild / page index so they never interleave. */
  let indexChain: Promise<void> = Promise.resolve()
  let disposed = false

  function loadSettings(_ctx?: PluginContext) {
    const raw = (appSettings.config?.plugins?.plugin_settings as any)?.[
      'silt-ai-qa'
    ] as Record<string, unknown> | undefined
    settings = resolveSettings(raw)
  }

  function setSettings(next: QASettings) {
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
        message: 'Configure an embedding model in Settings → AI Provider'
      }
      return
    }
    await runIndexJob(async () => {
      if (disposed) return
      try {
        await rebuildIndex(ctx, settings, (p) => {
          if (!disposed) progress = p
        })
      } catch (e: any) {
        if (!disposed) {
          progress = {
            status: 'error',
            done: 0,
            total: 0,
            lastError: e?.message ?? String(e)
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
        message: 'Configure an embedding model in Settings → AI Provider'
      }
      return
    }
    await runIndexJob(async () => {
      if (disposed) return
      try {
        await ensureIndexReady(ctx)
        const model = configuredEmbedModel()
        const info = await getIndexInfo(ctx)
        const mustRebuild =
          info.chunkCount === 0 ||
          (await needsFullRebuildForModel(ctx, model, info.dimensions))
        if (mustRebuild) {
          await rebuildIndex(ctx, settings, (p) => {
            if (!disposed) progress = p
          })
        } else {
          progress = {
            status: 'ready',
            done: 0,
            total: 0,
            model: info.model,
            dimensions: info.dimensions,
            chunkCount: info.chunkCount,
            message: `Indexed ${info.chunkCount} chunks`
          }
        }
      } catch (e: any) {
        if (!disposed) {
          progress = {
            status: 'error',
            done: 0,
            total: 0,
            lastError: e?.message ?? String(e)
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
            return
          }
          await indexPage(ctx, notebook, section, page, settings, (p) => {
            if (!disposed) progress = p
          })
        } catch (e: any) {
          if (!disposed) {
            progress = {
              status: 'error',
              done: 0,
              total: 0,
              lastError: e?.message ?? String(e)
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
      errorMessage = 'Configure a chat model in Settings → AI Provider'
      return
    }
    if (!embedReady()) {
      panelStatus = 'no-embedding-provider'
      errorMessage =
        'Configure an embedding model in Settings → AI Provider to build the index'
      return
    }

    askInFlight = true
    conversation.addUser(q)
    answer = ''
    // Keep prior citations visible until a new answer lands.
    panelStatus = 'asking'
    errorMessage = ''
    let assistantStarted = false

    try {
      const passages = await hybridRetrieve(ctx, q, settings)
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
          messages: messages as any,
          stream: true,
          temperature: 0.3
        })
        activeStream = stream as PluginAIStream
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
      } catch (streamErr: any) {
        // Fallback to non-stream if provider rejects streaming.
        if (
          String(streamErr?.code ?? '').includes('bad-request') ||
          /stream/i.test(String(streamErr?.message ?? ''))
        ) {
          const res = await ctx.ai.complete({
            messages: messages as any,
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
    } catch (e: any) {
      panelStatus = 'error'
      if (e instanceof RetrieveError) {
        errorMessage = e.message
      } else {
        errorMessage = e?.message ?? String(e)
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
      return conversation.getMessages()
    },
    get askInFlight() {
      return askInFlight
    },
    loadSettings,
    setSettings,
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
