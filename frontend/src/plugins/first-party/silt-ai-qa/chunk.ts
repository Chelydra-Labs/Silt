// Block-aware chunking + content hash for silt-ai-qa (#224).

import type { ChunkRecord } from './types'

/** sha256 hex of content (WebCrypto). */
export async function computeContentHash(content: string): Promise<string> {
  const data = new TextEncoder().encode(content)
  const buf = await crypto.subtle.digest('SHA-256', data)
  return Array.from(new Uint8Array(buf))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

export interface BlockInput {
  id: string
  notebook: string
  section: string
  page: string
  line_number?: number
  lineNumber?: number
  clean_content?: string
  cleanContent?: string
  raw_content?: string
  rawContent?: string
  type?: string
}

/**
 * Turn index blocks into embeddable chunks. One chunk per non-empty block
 * (block-aware; no sliding windows in v1). Skips empty / whitespace-only.
 */
export async function chunksFromBlocks(
  blocks: BlockInput[]
): Promise<ChunkRecord[]> {
  const out: ChunkRecord[] = []
  for (const b of blocks) {
    const text = (
      b.clean_content ??
      b.cleanContent ??
      b.raw_content ??
      b.rawContent ??
      ''
    ).trim()
    if (!text) continue
    const blockId = b.id
    if (!blockId) continue
    const lineNumber = b.line_number ?? b.lineNumber ?? 0
    const contentHash = await computeContentHash(text)
    out.push({
      chunkId: blockId,
      blockId,
      notebook: b.notebook ?? '',
      section: b.section ?? '',
      page: b.page ?? '',
      lineNumber,
      text,
      contentHash
    })
  }
  return out
}

/** True when two chunk lists differ by hash set (for incremental skip). */
export function hashesEqual(
  a: { chunkId: string; contentHash: string }[],
  b: { chunkId: string; contentHash: string }[]
): boolean {
  if (a.length !== b.length) return false
  const map = new Map(a.map((c) => [c.chunkId, c.contentHash]))
  for (const c of b) {
    if (map.get(c.chunkId) !== c.contentHash) return false
  }
  return true
}
