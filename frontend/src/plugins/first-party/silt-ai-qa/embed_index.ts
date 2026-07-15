// Incremental vector index for silt-ai-qa (#224).
// Plugin-owned SQLite + sqlite-vec vec0. Markdown remains source of truth.

import type { PluginContext } from '../../sdk'
import { chunksFromBlocks, type BlockInput } from './chunk'
import type { IndexProgress, QASettings } from './types'
import type { RankedHit } from './hybrid'

const META_MIGRATION = 1
const META_SQL = `
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

let migrated = false
let embedTableReady = false
let currentDims = 0

export function resetIndexState(): void {
  migrated = false
  embedTableReady = false
  currentDims = 0
}

export async function migrateIndex(ctx: PluginContext): Promise<void> {
  if (migrated) return
  await ctx.pluginDb.migrate(META_MIGRATION, META_SQL)
  migrated = true
}

/**
 * Hydrate in-memory dim lock from durable meta after vault open / process
 * restart so CREATE IF NOT EXISTS cannot leave a wrong-dim vec0 table.
 */
export async function ensureIndexReady(ctx: PluginContext): Promise<void> {
  await migrateIndex(ctx)
  const dims = Number((await metaGet(ctx, 'dimensions')) ?? 0)
  if (dims > 0) {
    // Force reconcile: if meta dims differ from what we last created in-process
    // (or process memory was reset), drop+recreate when needed.
    if (!embedTableReady || currentDims !== dims) {
      if (currentDims !== 0 && currentDims !== dims) {
        await ctx.pluginDb.exec(`DROP TABLE IF EXISTS embeddings`)
        embedTableReady = false
      }
      // Probe whether embeddings exists with the expected schema by attempting
      // CREATE IF NOT EXISTS at the meta dimension. If a prior table was created
      // at a different dim, sqlite-vec will error on mismatch — drop and retry.
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

/**
 * True when the configured embedding model (or its dimensions) no longer
 * matches the durable index — caller should full-rebuild.
 */
export async function needsFullRebuildForModel(
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

export async function metaGet(
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

async function ensureVecTable(ctx: PluginContext, dims: number): Promise<void> {
  if (embedTableReady && currentDims === dims) return
  // Always reconcile against durable meta — after restart currentDims is 0
  // even when a fixed-dim vec0 table already exists (PR #540 review).
  const metaDims = Number((await metaGet(ctx, 'dimensions')) ?? 0)
  const mustDrop =
    (metaDims > 0 && metaDims !== dims) ||
    (currentDims > 0 && currentDims !== dims)
  if (mustDrop) {
    await ctx.pluginDb.exec(`DROP TABLE IF EXISTS embeddings`)
    embedTableReady = false
    currentDims = 0
  } else if (metaDims === dims && dims > 0) {
    // Same dims as meta — try create-if-not-exists without drop.
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
    // Existing table at wrong dim (meta missing/stale) — force drop.
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

function vecLiteral(v: number[]): string {
  return `[${v.join(',')}]`
}

const BATCH = 16

export type ProgressCb = (p: IndexProgress) => void

/**
 * Full rebuild: clear chunks + embeddings, re-scan all blocks in scope.
 */
export async function rebuildIndex(
  ctx: PluginContext,
  settings: QASettings,
  onProgress?: ProgressCb
): Promise<void> {
  await migrateIndex(ctx)
  onProgress?.({ status: 'indexing', done: 0, total: 0, message: 'Scanning…' })

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

  // Clear prior index rows. Always DROP the vec0 table so a dim/model change
  // cannot leave CREATE IF NOT EXISTS no-op'ing against a stale schema.
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

/**
 * Incremental: re-index one page's blocks (hash-diff).
 */
export async function indexPage(
  ctx: PluginContext,
  notebook: string,
  section: string,
  page: string,
  settings: QASettings,
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

  // Existing hashes for this page.
  const { rows: existing } = await ctx.pluginDb.query(
    `SELECT chunk_id, content_hash FROM chunks
      WHERE notebook = ? AND section = ? AND page = ?`,
    [notebook, section, page]
  )
  const oldMap = new Map(
    existing.map((r) => [String(r.chunk_id), String(r.content_hash)])
  )
  const newIds = new Set(chunks.map((c) => c.chunkId))

  // Delete removed blocks.
  for (const [id] of oldMap) {
    if (!newIds.has(id)) {
      await ctx.pluginDb.exec(`DELETE FROM chunks WHERE chunk_id = ?`, [id])
      if (embedTableReady) {
        try {
          await ctx.pluginDb.exec(`DELETE FROM embeddings WHERE chunk_id = ?`, [
            id
          ])
        } catch {
          /* ignore */
        }
      }
    }
  }

  // Only re-embed changed / new.
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

async function countChunks(ctx: PluginContext): Promise<number> {
  const { rows } = await ctx.pluginDb.query(`SELECT COUNT(*) AS n FROM chunks`)
  return Number(rows[0]?.n ?? 0)
}

async function indexChunks(
  ctx: PluginContext,
  chunks: Awaited<ReturnType<typeof chunksFromBlocks>>,
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
  let done = 0
  const total = chunks.length
  onProgress?.({
    status: 'indexing',
    done: 0,
    total,
    message: 'Building search index…'
  })

  for (let i = 0; i < chunks.length; i += BATCH) {
    const batch = chunks.slice(i, i + BATCH)
    const res = await ctx.ai.embed({
      texts: batch.map((c) => c.text),
      taskType: 'RETRIEVAL_DOCUMENT'
    })
    model = res.model || model
    dims = res.dimensions || dims
    if (!dims && res.embeddings[0]) dims = res.embeddings[0].length
    if (!dims) throw new Error('embedding provider returned zero dimensions')
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
      // vec0: delete+insert (UPDATE of primary key vector row).
      try {
        await ctx.pluginDb.exec(`DELETE FROM embeddings WHERE chunk_id = ?`, [
          c.chunkId
        ])
      } catch {
        /* first insert */
      }
      await ctx.pluginDb.exec(
        `INSERT INTO embeddings(chunk_id, embedding) VALUES(?, vec_f32(?))`,
        [c.chunkId, vecLiteral(vec)]
      )
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
  // Marks the index as built with document/query task-type asymmetry (#610).
  await metaSet(ctx, 'task_type_version', '1')
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

/** KNN vector search for a query string. */
export async function vectorSearch(
  ctx: PluginContext,
  query: string,
  topK: number
): Promise<RankedHit[]> {
  await migrateIndex(ctx)
  const dimsStr = await metaGet(ctx, 'dimensions')
  const dims = Number(dimsStr ?? 0)
  if (!dims) return []
  await ensureVecTable(ctx, dims)

  const emb = await ctx.ai.embed({
    texts: [query],
    taskType: 'RETRIEVAL_QUERY'
  })
  const vec = emb.embeddings[0]
  if (!vec) return []

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
    [vecLiteral(vec), topK]
  )

  return rows.map((r) => ({
    blockId: String(r.block_id ?? r.chunk_id ?? ''),
    notebook: String(r.notebook ?? ''),
    section: String(r.section ?? ''),
    page: String(r.page ?? ''),
    lineNumber: Number(r.line_number ?? 0),
    text: String(r.text ?? ''),
    score: Number(r.distance ?? 0)
  }))
}

export async function getIndexInfo(
  ctx: PluginContext
): Promise<{ model: string; dimensions: number; chunkCount: number }> {
  await migrateIndex(ctx)
  return {
    model: (await metaGet(ctx, 'model')) ?? '',
    dimensions: Number((await metaGet(ctx, 'dimensions')) ?? 0),
    chunkCount: await countChunks(ctx)
  }
}
