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

    const tableSlash = searchProductDocs(
      'how to create a table using a slash command'
    )
    expect(tableSlash.length).toBeGreaterThan(0)
    expect(
      tableSlash.some(
        (h) =>
          h.docId === 'tables' ||
          h.docId === 'slash-commands' ||
          /table/i.test(h.excerpt + h.displayTitle)
      )
    ).toBe(true)
    expect(tableSlash[0].excerpt + tableSlash[0].displayTitle).toMatch(
      /table|slash|\/\s*|3×3|Custom table/i
    )

    const goldens: Array<{ q: string; docId: string }> = [
      {
        q: 'how do wiki links and backlinks work',
        docId: 'wiki-links-and-backlinks'
      },
      { q: 'tasks hub board calendar list view', docId: 'tasks-hub' },
      {
        q: 'page properties assign type Ctrl',
        docId: 'page-properties-and-types'
      },
      { q: 'link external folder linked notebook', docId: 'linked-notebooks' },
      { q: 'attach file image attachments folder', docId: 'attachments' },
      { q: 'search vault vs semantic search AI', docId: 'search-and-find' },
      {
        q: 'search task owner status priority due overdue',
        docId: 'search-and-find'
      },
      { q: 'tasks hub find task search field saved view', docId: 'tasks-hub' },
      { q: 'undo task change ctrl z', docId: 'tasks-hub' },
      { q: 'preview tab pin double click page', docId: 'navigation-and-tabs' }
    ]
    for (const { q, docId } of goldens) {
      const hits = searchProductDocs(q)
      expect(hits.length, q).toBeGreaterThan(0)
      expect(
        hits.some((h) => h.docId === docId),
        `${q} → expect ${docId}, got ${hits.map((h) => h.docId).join(',')}`
      ).toBe(true)
    }
  })

  it('documents the #1010 task-attribute filters and transient hub search', () => {
    resetProductDocCorpusCache()
    const sections = loadProductDocCorpus()
    const searchDoc = sections
      .filter((s) => s.docId === 'search-and-find')
      .map((s) => s.body)
      .join('\n')
    expect(searchDoc).toMatch(/priority floor/i)
    // Only type/tag/task filters suppress page titles; notebook/scope narrow
    // page rows too (backend applies those predicates inside the title leg).
    expect(searchDoc).toMatch(/type, tag, or task filter hides/gi)
    expect(searchDoc).toMatch(/do \*\*not\*\* hide page titles/i)
    expect(searchDoc).toMatch(/notebook\/section names/i)
    expect(searchDoc).toMatch(/need a query/i)

    const hubDoc = sections
      .filter((s) => s.docId === 'tasks-hub')
      .map((s) => s.body)
      .join('\n')
    expect(hubDoc).toMatch(/Find task…/)
    expect(hubDoc).toMatch(/transient/i)
    expect(hubDoc).toMatch(/saved-view dimensions/i)
  })

  it('documents session-scoped undo for task changes (#1011)', () => {
    resetProductDocCorpusCache()
    const sections = loadProductDocCorpus()
    const hubDoc = sections
      .filter((s) => s.docId === 'tasks-hub')
      .map((s) => s.body)
      .join('\n')
    // Honest semantics: the journal is not "last task change", it is the most
    // recent task/block mutation; it is session-only and has no redo.
    expect(hubDoc).toMatch(/most recent task or block change/i)
    expect(hubDoc).toMatch(/session-scoped/i)
    expect(hubDoc).toMatch(/no redo/i)
    // A gesture can journal more than once: cross-file reorders record one
    // entry per file, and a Manual-sort Board drop records the field change and
    // the order separately. A skip drops the entry without emptying the
    // history.
    expect(hubDoc).toMatch(/one entry per file/i)
    expect(hubDoc).toMatch(
      /Board drop with Manual sorting records the\s+field change and the order\s+separately/i
    )
    expect(hubDoc).toMatch(/skipped undo is not the same as an empty history/i)
    expect(hubDoc).toMatch(/Ctrl\+Z/)
    // The tasks article must carry the same exception the keyboard article
    // states, so both PR-added docs describe hub undo with equal precision.
    expect(hubDoc).toMatch(
      /editable fields and open popovers keep their own\s+undo/i
    )

    // The keyboard article keeps the editor/hub ownership split discoverable.
    const keyboardDoc = sections
      .filter((s) => s.docId === 'keyboard-and-help')
      .map((s) => s.body)
      .join('\n')
    expect(keyboardDoc).toMatch(/contextual/i)
    expect(keyboardDoc).toMatch(/task drawer/i)
    // The implementation keeps native text undo in editable fields and leaves
    // nested popovers alone; the article must not over-promise hub undo there.
    expect(keyboardDoc).toMatch(
      /editable fields and open popovers keep their own\s+undo/i
    )
  })
})
