// Agent-owned vec0 index lifecycle (#923).
// Isolated createEmbedIndex() instance on silt-ai-agent plugin.db.
//
// All durable mutations (rebuild, indexPage, dropPageIndex) run on one
// serialized job chain. stop() bumps a generation so in-flight jobs exit
// without writing warm flags or leaving a partial index marked complete.

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
/** Durable meta: "1" while a full rebuild is in progress (survives crash/stop). */
const META_REBUILD_IN_PROGRESS = 'rebuild_in_progress'
/** Durable meta: "1" after a successful full rebuild or confirmed-current index. */
const META_REBUILD_COMPLETE = 'rebuild_complete'

const agentIndex = createEmbedIndex()

let started = false
let activeCtx: PluginContext | null = null
let unsubs: Array<() => void> = []
let debounceTimer: ReturnType<typeof setTimeout> | null = null
const pendingPages = new Map<
  string,
  { notebook: string; section: string; page: string }
>()
/** Serialized mutation queue (mirrors QA runIndexJob). */
let indexChain: Promise<void> = Promise.resolve()
/** Bumped on stop/restart so in-flight jobs observe cancel. */
let generation = 0
/** True while a full rebuild job is the active chain head. */
let fullRebuildInProgress = false
/** Session flag: index confirmed complete this session (or durable meta). */
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
  generation = 0
}

/**
 * Enqueue a durable index mutation. Jobs check generation and abort if stop()
 * ran. Failures do not break the chain.
 */
function runIndexJob(job: () => Promise<void>): Promise<void> {
  const gen = generation
  const next = indexChain.then(async () => {
    if (gen !== generation) return
    await job()
  })
  indexChain = next.catch(() => {})
  return next
}

function isCurrent(gen: number): boolean {
  return gen === generation && started
}

/**
 * Vector search for hybridRetrieve. Returns [] while a full rebuild is in
 * progress so hybrid degrades to pure FTS (no half-built vector ranking).
 * Other errors propagate for onDegraded / search_degraded.
 */
export function getAgentVectorSearch(): VectorSearchFn {
  if (vectorSearchOverride) return vectorSearchOverride
  return async (ctx, query, topK, queryVec) => {
    if (fullRebuildInProgress) return []
    return agentIndex.vectorSearch(ctx, query, topK, queryVec)
  }
}

/**
 * Warm = durable complete marker + dims/chunks + not mid-rebuild.
 * Interrupted rebuilds leave META_REBUILD_IN_PROGRESS and force rebuild on start.
 */
export async function isAgentIndexWarm(ctx: PluginContext): Promise<boolean> {
  if (warmOverride !== null) return warmOverride
  if (fullRebuildInProgress) return false
  try {
    const interrupted = await agentIndex.metaGet(ctx, META_REBUILD_IN_PROGRESS)
    if (interrupted === '1') return false
    const complete = await agentIndex.metaGet(ctx, META_REBUILD_COMPLETE)
    if (complete !== '1' && !fullRebuildCompleted) return false
    const info = await agentIndex.getIndexInfo(ctx)
    return info.dimensions > 0 && info.chunkCount > 0
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

async function indexOnePage(
  ctx: PluginContext,
  notebook: string,
  section: string,
  page: string
): Promise<void> {
  // Empty page (deleted) → drop vectors; otherwise hash-diff index.
  const { rows } = await ctx.sqliteQuery(
    `SELECT COUNT(*) AS n FROM blocks
      WHERE notebook = ? AND section = ? AND page = ?`,
    [notebook, section, page]
  )
  const n = Number(rows[0]?.n ?? 0)
  if (n === 0) {
    await agentIndex.dropPageIndex(ctx, notebook, section, page)
    return
  }
  await agentIndex.indexPage(ctx, notebook, section, page, DEFAULT_SETTINGS)
}

async function flushPendingPages(): Promise<void> {
  if (pendingPages.size === 0) return
  const batch = [...pendingPages.values()]
  pendingPages.clear()
  const ctx = activeCtx
  if (!ctx) return
  const gen = generation
  await runIndexJob(async () => {
    if (!isCurrent(gen) || activeCtx !== ctx) return
    for (const loc of batch) {
      if (!isCurrent(gen) || activeCtx !== ctx) return
      try {
        await indexOnePage(ctx, loc.notebook, loc.section, loc.page)
      } catch (e) {
        console.warn('silt-ai-agent: indexPage failed:', e)
      }
    }
  })
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

function ensureSubscriptions(ctx: PluginContext): void {
  if (unsubs.length > 0) return
  unsubs.push(ctx.on('block:changed', onBlockChanged))
  try {
    unsubs.push(ctx.on('editor:save', onBlockChanged))
  } catch {
    /* event may be unregistered on older hosts */
  }
}

/**
 * Start agent embed index when RAG is on. Non-blocking; errors are logged.
 * Idempotent while already started (swaps activeCtx without tearing down).
 * Rebuilds when empty, interrupted, or embedding model no longer matches.
 */
export function startAgentEmbedIndex(ctx: PluginContext): void {
  if (!getAIAvailability().ragEnabled) return

  // Already running: swap ctx in place (loader rebuilds ctx on every reconcile).
  if (started) {
    activeCtx = ctx
    ensureSubscriptions(ctx)
    return
  }

  activeCtx = ctx
  started = true
  // Do not reset fullRebuildCompleted here if durable meta says complete —
  // ensureIndexJob will set flags correctly.
  fullRebuildInProgress = false
  ensureSubscriptions(ctx)

  const gen = generation
  void runIndexJob(async () => {
    if (!isCurrent(gen) || activeCtx !== ctx) return
    try {
      if (!embedConfigured()) {
        console.warn(
          'silt-ai-agent: embed index skipped — configure a search model in Settings → AI'
        )
        return
      }
      await agentIndex.migrateIndex(ctx)
      if (!isCurrent(gen)) return
      await agentIndex.ensureIndexReady(ctx)
      if (!isCurrent(gen)) return

      const model = configuredEmbedModel()
      const info = await agentIndex.getIndexInfo(ctx)
      const interrupted =
        (await agentIndex.metaGet(ctx, META_REBUILD_IN_PROGRESS)) === '1'
      const mustRebuild =
        interrupted ||
        info.chunkCount === 0 ||
        (await agentIndex.needsFullRebuildForModel(
          ctx,
          model,
          info.dimensions || undefined
        ))

      if (mustRebuild) {
        if (!isCurrent(gen)) return
        fullRebuildInProgress = true
        fullRebuildCompleted = false
        await agentIndex.metaSet(ctx, META_REBUILD_IN_PROGRESS, '1')
        await agentIndex.metaSet(ctx, META_REBUILD_COMPLETE, '0')
        try {
          if (!isCurrent(gen)) return
          await agentIndex.rebuildIndex(ctx, DEFAULT_SETTINGS)
          if (!isCurrent(gen)) return
          await agentIndex.metaSet(ctx, META_REBUILD_IN_PROGRESS, '0')
          await agentIndex.metaSet(ctx, META_REBUILD_COMPLETE, '1')
          fullRebuildCompleted = true
        } finally {
          if (isCurrent(gen)) {
            fullRebuildInProgress = false
          } else {
            // Stopped mid-rebuild: leave META_REBUILD_IN_PROGRESS=1 so next
            // start forces a full rebuild. Clear in-memory flag only.
            fullRebuildInProgress = false
          }
        }
      } else if (isCurrent(gen)) {
        await agentIndex.metaSet(ctx, META_REBUILD_COMPLETE, '1')
        fullRebuildCompleted = true
      }
    } catch (e) {
      if (isCurrent(gen)) {
        fullRebuildInProgress = false
        console.warn('silt-ai-agent: embed index start failed:', e)
      }
    }
  })
}

/**
 * Tear down subscriptions and cancel in-flight jobs via generation bump.
 * Does not wait for the chain; jobs observe generation and exit.
 * Durable META_REBUILD_IN_PROGRESS stays set if a rebuild was interrupted.
 */
export function stopAgentEmbedIndex(): void {
  generation++
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
  // Reset in-memory embed table flags only — durable rows stay for next open.
  // In-flight rebuild may still write until it observes generation; durable
  // META_REBUILD_IN_PROGRESS forces rebuild on next start if interrupted.
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

/** Test/diagnostics: current generation (increments on stop). */
export function getAgentEmbedGeneration(): number {
  return generation
}
