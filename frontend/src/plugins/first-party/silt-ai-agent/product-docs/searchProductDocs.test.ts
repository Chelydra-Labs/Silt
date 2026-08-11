import { describe, expect, it } from 'vitest'
import {
  buildCorpusFromRaw,
  loadProductDocCorpus,
  resetProductDocCorpusCache
} from './loadCorpus'
import { NO_MATCH_MESSAGE, searchProductDocs } from './searchProductDocs'

const SAMPLE = {
  './getting-started.md': `---
id: getting-started
title: Getting started with Silt AI
---

## Enable AI

Turn on Enable AI under Settings → AI and configure a chat model.

## Semantic search

Semantic search powers hybrid note search when embeddings are configured.
`,
  './backup.md': `---
id: backup
title: Backup and migrate your vault
---

## Portable archive

Export vault creates a .silt-vault archive from Settings → General.
`,
  './README.md': `# ignore me`
}

const corpus = buildCorpusFromRaw(SAMPLE)

describe('buildCorpusFromRaw', () => {
  it('skips README and splits ## sections', () => {
    expect(corpus.every((s) => s.docId !== 'README')).toBe(true)
    expect(corpus.some((s) => s.sectionHeading === 'Enable AI')).toBe(true)
    expect(corpus.some((s) => s.helpId.startsWith('help:'))).toBe(true)
  })
})

describe('searchProductDocs', () => {
  it('returns ranked hits for a product how-to query', () => {
    const hits = searchProductDocs('how to enable AI settings', 5, corpus)
    expect(hits.length).toBeGreaterThan(0)
    expect(hits[0].displayTitle).toMatch(/^Silt help:/)
    expect(hits[0].excerpt.toLowerCase()).toMatch(/enable ai|settings/)
  })

  it('returns empty array for empty query', () => {
    expect(searchProductDocs('   ', 5, corpus)).toEqual([])
    expect(searchProductDocs('', 5, corpus)).toEqual([])
  })

  it('returns empty array when nothing matches', () => {
    const hits = searchProductDocs(
      'quantum teleportation flux capacitor xyzzy',
      5,
      corpus
    )
    expect(hits).toEqual([])
  })

  it('prefers title/heading matches for backup query', () => {
    const hits = searchProductDocs('export vault backup archive', 5, corpus)
    expect(hits.length).toBeGreaterThan(0)
    expect(hits[0].docId).toBe('backup')
  })

  it('respects top_k cap', () => {
    const hits = searchProductDocs('AI settings search', 1, corpus)
    expect(hits.length).toBeLessThanOrEqual(1)
  })
})

describe('NO_MATCH_MESSAGE', () => {
  it('is the stable empty-result copy', () => {
    expect(NO_MATCH_MESSAGE).toBe('No matching Silt help topics.')
  })
})

describe('shipped product-docs corpus', () => {
  it('loads real articles via import.meta.glob and answers golden queries', () => {
    resetProductDocCorpusCache()
    const sections = loadProductDocCorpus()
    expect(sections.length).toBeGreaterThan(5)
    expect(sections.every((s) => s.helpId.startsWith('help:'))).toBe(true)

    const enable = searchProductDocs('how to enable AI settings')
    expect(enable.length).toBeGreaterThan(0)
    expect(enable[0].displayTitle).toMatch(/Silt help:/i)

    const backup = searchProductDocs('export vault backup archive')
    expect(backup.length).toBeGreaterThan(0)
    expect(backup[0].docId).toBe('backup')

    const templates = searchProductDocs('page templates custom')
    expect(templates.length).toBeGreaterThan(0)
    expect(templates.some((h) => h.docId === 'templates')).toBe(true)
  })
})
