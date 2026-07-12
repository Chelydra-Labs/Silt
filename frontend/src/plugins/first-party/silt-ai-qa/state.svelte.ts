// Reactive controller for silt-ai-qa panel + index status (#227).

import type { PluginContext, PluginAIStream } from '../../sdk'
import {
  aiProviderNeedsSetup,
  embeddingProviderNeedsSetup
} from '../../../settings/ai-setup'
import { settings as appSettings } from '../../../settings/store.svelte'
import { createConversation, type Conversation } from './conversation'
import { hybridRetrieve } from './retrieve'
import { buildRAGMessages, NO_RESULTS_MESSAGE, parseCitations } from './rag'
import { resolveSettings } from './settings'
import type { Citation, IndexProgress, QASettings } from './types'
import {
  getIndexInfo,
  indexPage,
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
  let indexTimer: ReturnType<typeof setTimeout> | null = null

  function loadSettings(ctx: PluginContext) {
    void ctx.getSetting?.('' as any) // keep ctx warm
    // Settings live in plugin_settings via getPluginSettings path on the host;
    // the panel/settings page push updates through updatePluginSetting.
    // Controller reads from app settings store when available.
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

  async function refreshIndexInfo(ctx: PluginContext) {
    try {
      const info = await getIndexInfo(ctx)
      progress = {
        ...progress,
        status: info.chunkCount > 0 ? 'ready' : progress.status,
        model: info.model,
        dimensions: info.dimensions,
        chunkCount: info.chunkCount
      }
    } catch {
      /* ignore */
    }
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
    try {
      await rebuildIndex(ctx, settings, (p) => {
        progress = p
      })
    } catch (e: any) {
      progress = {
        status: 'error',
        done: 0,
        total: 0,
        lastError: e?.message ?? String(e)
      }
    }
  }

  function schedulePageIndex(
    ctx: PluginContext,
    notebook: string,
    section: string,
    page: string
  ) {
    if (!settings.auto_reembed || !embedReady()) return
    if (indexTimer) clearTimeout(indexTimer)
    indexTimer = setTimeout(() => {
      void indexPage(ctx, notebook, section, page, settings, (p) => {
        progress = p
      }).catch((e: any) => {
        progress = {
          status: 'error',
          done: 0,
          total: 0,
          lastError: e?.message ?? String(e)
        }
      })
    }, settings.reindex_debounce_ms)
  }

  async function ask(ctx: PluginContext, question: string) {
    const q = question.trim()
    if (!q) return
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

    conversation.addUser(q)
    answer = ''
    citations = []
    panelStatus = 'asking'
    errorMessage = ''

    try {
      const passages = await hybridRetrieve(ctx, q, settings)
      if (passages.length === 0) {
        panelStatus = 'no-results'
        answer = NO_RESULTS_MESSAGE
        conversation.addAssistant(NO_RESULTS_MESSAGE)
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
          conversation.addAssistant(answer, citations)
          panelStatus = 'idle'
          return
        }
        throw streamErr
      } finally {
        activeStream = null
      }
    } catch (e: any) {
      panelStatus = 'error'
      errorMessage = e?.message ?? String(e)
      conversation.addAssistant(`Error: ${errorMessage}`)
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
      panelStatus = 'idle'
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
    loadSettings,
    setSettings,
    chatReady,
    embedReady,
    refreshIndexInfo,
    rebuild,
    schedulePageIndex,
    ask,
    stop,
    clear,
    dispose
  }
}

export type QAController = ReturnType<typeof createQAController>

let controller: QAController | null = null

export function getQAController(): QAController | null {
  return controller
}

export function setQAController(c: QAController | null) {
  controller = c
}
