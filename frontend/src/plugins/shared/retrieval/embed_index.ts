// Shared incremental vector index helpers (plugin-owned SQLite + sqlite-vec).
// Factory isolates in-memory state so QA and agent each own a separate instance.

import type { PluginContext } from '../../sdk'
import { asString } from '../../../lib/asString'
import { chunksFromBlocks, type BlockInput, type ChunkRecord } from './chunk'
import type { RankedHit } from './hybrid'

const META_MIGRATION = 1

/**
 * Index tables owned by the embed-index schema. All statements use
 * IF NOT EXISTS so a migration bundle that includes other schemas can
 * re-apply this text idempotently (user_version is one counter per DB).
 */
export const INDEX_TABLES_SQL = `
CREATE TABLE IF NOT EXISTS index_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS chunks (
  chunk_id TEXT PRIMARY KEY,
  block_id TEXT NOT NULL,
  notebook TEXT NOT NULL,
  section TEXT NOT NULL,
  page TEXT NOT NULL,
  line_number INTEGER NOT NULL DEFAULT 0,
  text TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  model TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chunks_page ON chunks(notebook, section, page);
CREATE INDEX IF NOT EXISTS idx_chunks_hash ON chunks(content_hash);
`

export type IndexStatus =
  'idle' | 'indexing' | 'ready' | 'error' | 'unconfigured'

export interface IndexProgress {
  status: IndexStatus
  done: number
  total: number
  message?: string
  model?: string
  dimensions?: number
  chunkCount?: number
  lastError?: string
}

/** Minimal settings for index ops (QASettings satisfies structurally). */
export interface EmbedIndexSettings {
  notebook_scope: string[]
}

export type ProgressCb = (p: IndexProgress) => void

/**
 * Default minimum cosine similarity for vector hits.
 * vec0 distance_metric=cosine stores distance ≈ 1 − similarity.
 */
export const DEFAULT_MIN_COSINE_SIMILARITY = 0.5

const BATCH = 16

function vecLiteral(v: number[]): string {
  return `[${v.join(',')}]`
}

export interface EmbedIndex {
  resetIndexState(): void
  migrateIndex(ctx: PluginContext): Promise<void>
  ensureIndexReady(ctx: PluginContext): Promise<void>
  needsFullRebuildForModel(
    ctx: PluginContext,
    configuredModel: string,
    configuredDims?: number
  ): Promise<boolean>
  metaGet(ctx: PluginContext, key: string): Promise<string | null>
  metaSet(ctx: PluginContext, key: string, value: string): Promise<void>
  rebuildIndex(
    ctx: PluginContext,
    settings: EmbedIndexSettings,
    onProgress?: ProgressCb
  ): Promise<void>
  dropPageIndex(
    ctx: PluginContext,
    notebook: string,
    section: string,
    page: string
  ): Promise<void>
  indexPage(
    ctx: PluginContext,
    notebook: string,
    section: string,
    page: string,
    settings: EmbedIndexSettings,
    onProgress?: ProgressCb
  ): Promise<void>
  vectorSearch(
    ctx: PluginContext,
    query: string,
    topK: number,
    queryVec?: number[],
    minCosineSimilarity?: number
  ): Promise<RankedHit[]>
  getIndexInfo(
    ctx: PluginContext
  ): Promise<{ model: string; dimensions: number; chunkCount: number }>
}

export interface EmbedIndexOptions {
  /**
   * Migration identity this instance stamps. Only override when the plugin
   * DB has another schema owner folding this SQL into a higher version
   * (e.g. silt-ai-agent v2); otherwise version 1 is correct (silt-ai-qa).
   */
  migrationVersion?: number
  migrationSql?: string
}

/** Create an isolated embed-index instance (no process-global consumer state). */
export function createEmbedIndex(
  opts: EmbedIndexOptions = {}
): EmbedIndex {
  let migrated = false
  let embedTableReady = false
  let currentDims = 0

  function resetIndexState(): void {
    migrated = false
    embedTableReady = false
    currentDims = 0
  }

  async function migrateIndex(ctx: PluginContext): Promise<void> {
    if (migrated) return
    await ctx.pluginDb.migrate(
      opts.migrationVersion ?? META_MIGRATION,
      opts.migrationSql ?? INDEX_TABLES_SQL
    )
    migrated = true
  }

  async function metaGet(
    ctx: PluginContext,
    key: string
  ): Promise<string | null> {
    const { rows } = await ctx.pluginDb.query(
      `SELECT value FROM index_meta WHERE key = ? LIMIT 1`,
      [key]
    )
    const v = rows[0]?.value
    return typeof v === 'string' ? v : null
  }

  async function metaSet(
    ctx: PluginContext,
    key: string,
    value: string
  ): Promise<void> {
    await ctx.pluginDb.exec(
      `INSERT INTO index_meta(key, value) VALUES(?, ?)
       ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
      [key, value]
    )
  }

  async function ensureIndexReady(ctx: PluginContext): Promise<void> {
    await migrateIndex(ctx)
    const dims = Number((await metaGet(ctx, 'dimensions')) ?? 0)
    if (dims > 0) {
      if (!embedTableReady || currentDims !== dims) {
        if (currentDims !== 0 && currentDims !== dims) {
          await ctx.pluginDb.exec(`DROP TABLE IF EXISTS embeddings`)
          embedTableReady = false
        }
        try {
          await ctx.pluginDb.exec(
            `CREATE VIRTUAL TABLE IF NOT EXISTS embeddings USING vec0(
              chunk_id TEXT PRIMARY KEY,
              embedding float[${dims}] distance_metric=cosine
            )`
          )
        } catch {
          await ctx.pluginDb.exec(`DROP TABLE IF EXISTS embeddings`)
          await ctx.pluginDb.exec(
            `CREATE VIRTUAL TABLE IF NOT EXISTS embeddings USING vec0(
              chunk_id TEXT PRIMARY KEY,
              embedding float[${dims}] distance_metric=cosine
            )`
          )
        }
        embedTableReady = true
        currentDims = dims
      }
    }
  }

  async function needsFullRebuildForModel(
    ctx: PluginContext,
    configuredModel: string,
    configuredDims?: number
  ): Promise<boolean> {
    await migrateIndex(ctx)
    const storedModel = (await metaGet(ctx, 'model')) ?? ''
    const storedDims = Number((await metaGet(ctx, 'dimensions')) ?? 0)
    const n = await countChunks(ctx)
    if (n === 0) return true
    if (configuredModel && storedModel && configuredModel !== storedModel) {
      return true
    }
    if (
      configuredDims &&
      configuredDims > 0 &&
      storedDims > 0 &&
      configuredDims !== storedDims
    ) {
      return true
    }
    return false
  }

  async function ensureVecTable(
    ctx: PluginContext,
    dims: number
  ): Promise<void> {
    if (embedTableReady && currentDims === dims) return
    const metaDims = Number((await metaGet(ctx, 'dimensions')) ?? 0)
    const mustDrop =
      (metaDims > 0 && metaDims !== dims) ||
      (currentDims > 0 && currentDims !== dims)
    if (mustDrop) {
      await ctx.pluginDb.exec(`DROP TABLE IF EXISTS embeddings`)
      embedTableReady = false
      currentDims = 0
    } else if (metaDims === dims && dims > 0) {
      try {
        await ctx.pluginDb.exec(
          `CREATE VIRTUAL TABLE IF NOT EXISTS embeddings USING vec0(
            chunk_id TEXT PRIMARY KEY,
            embedding float[${dims}] distance_metric=cosine
          )`
        )
        embedTableReady = true
        currentDims = dims
        await metaSet(ctx, 'dimensions', String(dims))
        return
      } catch {
        await ctx.pluginDb.exec(`DROP TABLE IF EXISTS embeddings`)
        embedTableReady = false
        currentDims = 0
      }
    }
    try {
      await ctx.pluginDb.exec(
        `CREATE VIRTUAL TABLE IF NOT EXISTS embeddings USING vec0(
          chunk_id TEXT PRIMARY KEY,
          embedding float[${dims}] distance_metric=cosine
        )`
      )
    } catch {
      await ctx.pluginDb.exec(`DROP TABLE IF EXISTS embeddings`)
      await ctx.pluginDb.exec(
        `CREATE VIRTUAL TABLE IF NOT EXISTS embeddings USING vec0(
          chunk_id TEXT PRIMARY KEY,
          embedding float[${dims}] distance_metric=cosine
        )`
      )
    }
    embedTableReady = true
    currentDims = dims
    await metaSet(ctx, 'dimensions', String(dims))
  }

  async function countChunks(ctx: PluginContext): Promise<number> {
    const { rows } = await ctx.pluginDb.query(
      `SELECT COUNT(*) AS n FROM chunks`
    )
    return Number(rows[0]?.n ?? 0)
  }

  async function indexChunks(
    ctx: PluginContext,
    chunks: ChunkRecord[],
    onProgress?: ProgressCb
  ): Promise<void> {
    if (chunks.length === 0) {
      onProgress?.({
        status: 'ready',
        done: 0,
        total: 0,
        chunkCount: await countChunks(ctx)
      })
      return
    }

    let model = ''
    let dims = 0
    let done: number
    const total = chunks.length
    onProgress?.({
      status: 'indexing',
      done: 0,
      total,
      message: 'Building search index…'
    })

    for (let i = 0; i < chunks.length; i += BATCH) {
      const batch = chunks.slice(i, i + BATCH)
      /** Chunk ids written in this batch — rolled back if the batch fails mid-way. */
      const writtenIds: string[] = []
      try {
        const res = await ctx.ai.embed({
          texts: batch.map((c) => c.text),
          taskType: 'RETRIEVAL_DOCUMENT'
        })
        model = res.model || model
        dims = res.dimensions || dims
        if (!dims && res.embeddings[0]) dims = res.embeddings[0].length
        if (!dims)
          throw new Error('embedding provider returned zero dimensions')
        await ensureVecTable(ctx, dims)

        const now = new Date().toISOString()
        for (let j = 0; j < batch.length; j++) {
          const c = batch[j]
          const vec = res.embeddings[j]
          if (!vec) continue
          await ctx.pluginDb.exec(
            `INSERT INTO chunks(chunk_id, block_id, notebook, section, page, line_number, text, content_hash, model, updated_at)
             VALUES(?,?,?,?,?,?,?,?,?,?)
             ON CONFLICT(chunk_id) DO UPDATE SET
               block_id=excluded.block_id, notebook=excluded.notebook,
               section=excluded.section, page=excluded.page,
               line_number=excluded.line_number, text=excluded.text,
               content_hash=excluded.content_hash, model=excluded.model,
               updated_at=excluded.updated_at`,
            [
              c.chunkId,
              c.blockId,
              c.notebook,
              c.section,
              c.page,
              c.lineNumber,
              c.text,
              c.contentHash,
              model,
              now
            ]
          )
          writtenIds.push(c.chunkId)
          try {
            await ctx.pluginDb.exec(
              `DELETE FROM embeddings WHERE chunk_id = ?`,
              [c.chunkId]
            )
          } catch {
            /* first insert */
          }
          await ctx.pluginDb.exec(
            `INSERT INTO embeddings(chunk_id, embedding) VALUES(?, vec_f32(?))`,
            [c.chunkId, vecLiteral(vec)]
          )
        }
      } catch (err) {
        // Roll back this batch's chunk rows so a retry re-embeds them instead
        // of treating partial hashes as up-to-date (stale/missing vectors).
        if (writtenIds.length > 0) {
          const ph = writtenIds.map(() => '?').join(',')
          try {
            await ctx.pluginDb.exec(
              `DELETE FROM chunks WHERE chunk_id IN (${ph})`,
              writtenIds
            )
          } catch {
            /* best-effort */
          }
          try {
            await ctx.pluginDb.exec(
              `DELETE FROM embeddings WHERE chunk_id IN (${ph})`,
              writtenIds
            )
          } catch {
            /* table may be missing */
          }
        }
        throw err
      }
      done = Math.min(total, i + batch.length)
      onProgress?.({
        status: 'indexing',
        done,
        total,
        model,
        dimensions: dims,
        message: `Indexed ${done}/${total}`
      })
    }

    await metaSet(ctx, 'model', model)
    await metaSet(ctx, 'updated_at', new Date().toISOString())
    const n = await countChunks(ctx)
    onProgress?.({
      status: 'ready',
      done: total,
      total,
      model,
      dimensions: dims,
      chunkCount: n,
      message: `Indexed ${n} notes`
    })
  }

  async function rebuildIndex(
    ctx: PluginContext,
    settings: EmbedIndexSettings,
    onProgress?: ProgressCb
  ): Promise<void> {
    await migrateIndex(ctx)
    onProgress?.({
      status: 'indexing',
      done: 0,
      total: 0,
      message: 'Scanning…'
    })

    const scope = settings.notebook_scope
    let sql = `SELECT id, notebook, section, page, line_number, clean_content, type
                 FROM blocks WHERE clean_content IS NOT NULL AND trim(clean_content) != ''`
    const params: unknown[] = []
    if (scope.length > 0) {
      sql += ` AND notebook IN (${scope.map(() => '?').join(',')})`
      params.push(...scope)
    }
    const { rows } = await ctx.sqliteQuery(sql, params)
    const blocks = rows as unknown as BlockInput[]
    const chunks = await chunksFromBlocks(blocks)

    await ctx.pluginDb.exec(`DELETE FROM chunks`)
    try {
      await ctx.pluginDb.exec(`DROP TABLE IF EXISTS embeddings`)
    } catch {
      /* ignore */
    }
    embedTableReady = false
    currentDims = 0

    await indexChunks(ctx, chunks, onProgress)
  }

  async function dropPageIndex(
    ctx: PluginContext,
    notebook: string,
    section: string,
    page: string
  ): Promise<void> {
    await migrateIndex(ctx)
    const { rows: existing } = await ctx.pluginDb.query(
      `SELECT chunk_id FROM chunks
        WHERE notebook = ? AND section = ? AND page = ?`,
      [notebook, section, page]
    )
    if (existing.length === 0) return
    const ids = existing.map((r) => String(r.chunk_id))
    const placeholders = ids.map(() => '?').join(',')
    await ctx.pluginDb.exec(
      `DELETE FROM chunks WHERE chunk_id IN (${placeholders})`,
      ids
    )
    try {
      await ctx.pluginDb.exec(
        `DELETE FROM embeddings WHERE chunk_id IN (${placeholders})`,
        ids
      )
    } catch {
      /* table missing or vec0 not ready — chunks already dropped */
    }
  }

  async function indexPage(
    ctx: PluginContext,
    notebook: string,
    section: string,
    page: string,
    settings: EmbedIndexSettings,
    onProgress?: ProgressCb
  ): Promise<void> {
    await migrateIndex(ctx)
    if (
      settings.notebook_scope.length > 0 &&
      !settings.notebook_scope.includes(notebook)
    ) {
      return
    }
    const { rows } = await ctx.sqliteQuery(
      `SELECT id, notebook, section, page, line_number, clean_content, type
         FROM blocks
        WHERE notebook = ? AND section = ? AND page = ?
          AND clean_content IS NOT NULL AND trim(clean_content) != ''`,
      [notebook, section, page]
    )
    const chunks = await chunksFromBlocks(rows as unknown as BlockInput[])

    const { rows: existing } = await ctx.pluginDb.query(
      `SELECT chunk_id, content_hash FROM chunks
        WHERE notebook = ? AND section = ? AND page = ?`,
      [notebook, section, page]
    )
    const oldMap = new Map(
      existing.map((r) => [String(r.chunk_id), String(r.content_hash)])
    )
    const newIds = new Set(chunks.map((c) => c.chunkId))

    for (const [id] of oldMap) {
      if (!newIds.has(id)) {
        await ctx.pluginDb.exec(`DELETE FROM chunks WHERE chunk_id = ?`, [id])
        if (embedTableReady) {
          try {
            await ctx.pluginDb.exec(
              `DELETE FROM embeddings WHERE chunk_id = ?`,
              [id]
            )
          } catch {
            /* ignore */
          }
        }
      }
    }

    const dirty = chunks.filter((c) => oldMap.get(c.chunkId) !== c.contentHash)
    if (dirty.length === 0) {
      onProgress?.({
        status: 'ready',
        done: 0,
        total: 0,
        message: 'Up to date',
        chunkCount: await countChunks(ctx)
      })
      return
    }
    await indexChunks(ctx, dirty, onProgress)
  }

  async function vectorSearch(
    ctx: PluginContext,
    query: string,
    topK: number,
    queryVec?: number[],
    minCosineSimilarity: number = DEFAULT_MIN_COSINE_SIMILARITY
  ): Promise<RankedHit[]> {
    await migrateIndex(ctx)
    const dimsStr = await metaGet(ctx, 'dimensions')
    const dims = Number(dimsStr ?? 0)
    if (!dims) return []
    await ensureVecTable(ctx, dims)

    const vec =
      queryVec ??
      (
        await ctx.ai.embed({
          texts: [query],
          taskType: 'RETRIEVAL_QUERY'
        })
      ).embeddings[0]
    if (!vec) return []

    const floor = Math.min(1, Math.max(0, minCosineSimilarity))
    const fetchK = Math.max(topK * 10, topK)
    const maxDistance = 1 - floor

    const { rows } = await ctx.pluginDb.query(
      `SELECT e.chunk_id AS chunk_id, e.distance AS distance,
              c.block_id AS block_id, c.notebook AS notebook,
              c.section AS section, c.page AS page,
              c.line_number AS line_number, c.text AS text
         FROM embeddings e
         JOIN chunks c ON c.chunk_id = e.chunk_id
        WHERE e.embedding MATCH vec_f32(?)
          AND k = ?
        ORDER BY e.distance`,
      [vecLiteral(vec), fetchK]
    )

    return rows
      .map((r) => ({
        blockId: asString(r.block_id ?? r.chunk_id),
        notebook: asString(r.notebook),
        section: asString(r.section),
        page: asString(r.page),
        lineNumber: Number(r.line_number ?? 0),
        text: asString(r.text),
        score: Number(r.distance ?? 0)
      }))
      .filter((h) => Number.isFinite(h.score) && h.score <= maxDistance)
      .slice(0, topK)
  }

  async function getIndexInfo(
    ctx: PluginContext
  ): Promise<{ model: string; dimensions: number; chunkCount: number }> {
    await migrateIndex(ctx)
    return {
      model: (await metaGet(ctx, 'model')) ?? '',
      dimensions: Number((await metaGet(ctx, 'dimensions')) ?? 0),
      chunkCount: await countChunks(ctx)
    }
  }

  return {
    resetIndexState,
    migrateIndex,
    ensureIndexReady,
    needsFullRebuildForModel,
    metaGet,
    metaSet,
    rebuildIndex,
    dropPageIndex,
    indexPage,
    vectorSearch,
    getIndexInfo
  }
}
