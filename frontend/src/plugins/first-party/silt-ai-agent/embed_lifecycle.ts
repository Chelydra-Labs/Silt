// Agent-owned vec0 index lifecycle (#923).
// Isolated createEmbedIndex() instance on silt-ai-agent plugin.db.

import type { PluginContext } from '../../sdk'
import { asString } from '../../../lib/asString'
import { getAIAvailability } from '../../shared/ai-chat/availability'
import { embeddingProviderNeedsSetup } from '../../../settings/ai-setup'
import { settings } from '../../../settings/store.svelte'
import {
  createEmbedIndex,
  type EmbedIndexSettings
} from '../../shared/retrieval/embed_index'
import type { RankedHit } from '../../shared/retrieval/hybrid'
import type { VectorSearchFn } from '../../shared/retrieval/retrieve'

const DEFAULT_SETTINGS: EmbedIndexSettings = {
  notebook_scope: []
}

const REINDEX_DEBOUNCE_MS = 2000

const agentIndex = createEmbedIndex()

let started = false
let activeCtx: PluginContext | null = null
let unsubs: Array<() => void> = []
let debounceTimer: ReturnType<typeof setTimeout> | null = null
const pendingPages = new Map<
  string,
  { notebook: string; section: string; page: string }
>()
let indexChain: Promise<void> = Promise.resolve()
/** True while a full rebuild is in flight — index is not "warm" for fallback gating. */
let fullRebuildInProgress = false
/** Set after a successful full rebuild (or confirmed up-to-date index) this session. */
let fullRebuildCompleted = false

/** Test hook: override vector search without touching plugin.db. */
let vectorSearchOverride: VectorSearchFn | null = null
/** Test hook: override warm check. */
let warmOverride: boolean | null = null

export function setAgentVectorSearchForTests(fn: VectorSearchFn | null): void {
  vectorSearchOverride = fn
}

export function setAgentIndexWarmForTests(warm: boolean | null): void {
  warmOverride = warm
}

/** Reset process flags for tests (does not touch durable plugin.db). */
export function resetAgentEmbedLifecycleForTests(): void {
  stopAgentEmbedIndex()
  vectorSearchOverride = null
  warmOverride = null
  fullRebuildInProgress = false
  fullRebuildCompleted = false
  indexChain = Promise.resolve()
}

/**
 * Vector search for hybridRetrieve. Errors propagate so the shared pipeline
 * can fail-open to FTS and emit onDegraded / search_degraded (do not swallow).
 */
export function getAgentVectorSearch(): VectorSearchFn {
  if (vectorSearchOverride) return vectorSearchOverride
  return (ctx, query, topK, queryVec) =>
    agentIndex.vectorSearch(ctx, query, topK, queryVec)
}

/**
 * Warm = durable index has dims+chunks AND we are not mid full-rebuild AND
 * (this session completed a rebuild or confirmed the existing index is current).
 * Partial backfill must not skip semantic fallback.
 */
export async function isAgentIndexWarm(ctx: PluginContext): Promise<boolean> {
  if (warmOverride !== null) return warmOverride
  if (fullRebuildInProgress) return false
  try {
    const info = await agentIndex.getIndexInfo(ctx)
    if (!(info.dimensions > 0 && info.chunkCount > 0)) return false
    // Existing durable index from a prior session counts as warm once start
    // has confirmed it (fullRebuildCompleted) or after rebuild finishes.
    return fullRebuildCompleted
  } catch {
    return false
  }
}

function configuredEmbedModel(): string {
  return String(settings.config?.ai?.embedding?.model ?? '').trim()
}

function embedConfigured(): boolean {
  return !embeddingProviderNeedsSetup(
    settings.config?.ai?.embedding as
      { provider_type?: string; model?: string; has_key?: boolean } | undefined
  )
}

function pageKey(notebook: string, section: string, page: string): string {
  return `${notebook}\0${section}\0${page}`
}

function schedulePageIndex(
  notebook: string,
  section: string,
  page: string
): void {
  if (!notebook || !page) return
  pendingPages.set(pageKey(notebook, section, page), {
    notebook,
    section,
    page
  })
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    debounceTimer = null
    void flushPendingPages()
  }, REINDEX_DEBOUNCE_MS)
}

async function flushPendingPages(): Promise<void> {
  const ctx = activeCtx
  if (!ctx || pendingPages.size === 0) return
  const batch = [...pendingPages.values()]
  pendingPages.clear()
  indexChain = indexChain
    .then(async () => {
      for (const loc of batch) {
        if (!activeCtx) break
        try {
          await agentIndex.indexPage(
            ctx,
            loc.notebook,
            loc.section,
            loc.page,
            DEFAULT_SETTINGS
          )
        } catch (e) {
          console.warn('silt-ai-agent: indexPage failed:', e)
        }
      }
    })
    .catch(() => {})
  await indexChain
}

function onBlockChanged(payload: unknown): void {
  const p = payload as {
    notebook?: string
    section?: string
    page?: string
  } | null
  if (!p) return
  schedulePageIndex(asString(p.notebook), asString(p.section), asString(p.page))
}

/**
 * Start agent embed index when RAG is on. Non-blocking; errors are logged.
 * Idempotent while already started for the same session.
 * Rebuilds when empty or embedding model/dim no longer matches durable meta.
 */
export function startAgentEmbedIndex(ctx: PluginContext): void {
  if (!getAIAvailability().ragEnabled) return
  if (started && activeCtx === ctx) return
  stopAgentEmbedIndex()
  activeCtx = ctx
  started = true
  fullRebuildInProgress = false
  fullRebuildCompleted = false

  unsubs.push(ctx.on('block:changed', onBlockChanged))
  // editor:save if the host emits it (optional freshness signal).
  try {
    unsubs.push(ctx.on('editor:save', onBlockChanged))
  } catch {
    /* event may be unregistered on older hosts */
  }

  void (async () => {
    try {
      if (!embedConfigured()) {
        console.warn(
          'silt-ai-agent: embed index skipped — configure a search model in Settings → AI'
        )
        return
      }
      await agentIndex.migrateIndex(ctx)
      await agentIndex.ensureIndexReady(ctx)
      const model = configuredEmbedModel()
      const info = await agentIndex.getIndexInfo(ctx)
      const mustRebuild =
        info.chunkCount === 0 ||
        (await agentIndex.needsFullRebuildForModel(
          ctx,
          model,
          info.dimensions || undefined
        ))
      if (mustRebuild) {
        fullRebuildInProgress = true
        try {
          await agentIndex.rebuildIndex(ctx, DEFAULT_SETTINGS)
          if (activeCtx === ctx) {
            fullRebuildCompleted = true
          }
        } finally {
          if (activeCtx === ctx) fullRebuildInProgress = false
        }
      } else if (activeCtx === ctx) {
        // Durable index matches configured model — treat as warm.
        fullRebuildCompleted = true
      }
    } catch (e) {
      fullRebuildInProgress = false
      console.warn('silt-ai-agent: embed index start failed:', e)
    }
  })()
}

/** Tear down subscriptions and in-memory index flags (not durable rows). */
export function stopAgentEmbedIndex(): void {
  if (debounceTimer) {
    clearTimeout(debounceTimer)
    debounceTimer = null
  }
  pendingPages.clear()
  for (const off of unsubs) {
    try {
      off()
    } catch {
      /* ignore */
    }
  }
  unsubs = []
  activeCtx = null
  started = false
  fullRebuildInProgress = false
  fullRebuildCompleted = false
  agentIndex.resetIndexState()
}

/** Re-check RAG flag after settings change; start or stop accordingly. */
export function reconcileAgentEmbedIndex(ctx: PluginContext): void {
  if (getAIAvailability().ragEnabled) {
    startAgentEmbedIndex(ctx)
  } else {
    stopAgentEmbedIndex()
  }
}

/** Expose for diagnostics / tests. */
export function agentVectorSearchDirect(
  ctx: PluginContext,
  query: string,
  topK: number,
  queryVec?: number[]
): Promise<RankedHit[]> {
  return agentIndex.vectorSearch(ctx, query, topK, queryVec)
}

/** Test/diagnostics: whether a full rebuild is currently running. */
export function isAgentFullRebuildInProgress(): boolean {
  return fullRebuildInProgress
}
