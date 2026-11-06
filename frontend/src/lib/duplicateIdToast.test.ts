import { describe, expect, it, vi } from 'vitest'
import { duplicateIdToast, type DuplicateIDWarning } from './duplicateIdToast'

describe('duplicateIdToast', () => {
  const crossFile: DuplicateIDWarning = {
    block_id: '11111111-1111-1111-1111-111111111111',
    within_page: false,
    pages: [
      { source: 'vault', notebook: 'Work', section: 'Sprint', page: 'Board' },
      {
        source: 'vault',
        notebook: 'Work',
        section: 'Projects',
        page: 'Roadmap'
      }
    ]
  }

  const withinPage: DuplicateIDWarning = {
    block_id: '11111111-1111-1111-1111-111111111111',
    within_page: true,
    pages: [{ source: 'vault', notebook: 'Work', section: '', page: 'Board' }]
  }

  // Scan-detected combo: two carrier pages AND one of them doubled — the
  // cross-page copy must win.
  const crossAndWithin: DuplicateIDWarning = {
    ...withinPage,
    pages: [withinPage.pages[0], crossFile.pages[1]]
  }

  it('builds a sticky error notification (no auto-dismiss)', () => {
    const n = duplicateIdToast(crossFile, () => {})
    expect(n.kind).toBe('error')
    expect(n.autoDismissMs).toBe(0)
  })

  it('names BOTH carrier pages for a cross-file duplicate', () => {
    const n = duplicateIdToast(crossFile, () => {})
    expect(n.message).toContain('“Sprint/Board”')
    expect(n.message).toContain('“Projects/Roadmap”')
  })

  it('counts further carrier pages instead of silently dropping them', () => {
    const threePages: DuplicateIDWarning = {
      ...crossFile,
      pages: [
        ...crossFile.pages,
        { source: 'vault', notebook: 'Work', section: '', page: 'Notes' }
      ]
    }
    const n = duplicateIdToast(threePages, () => {})
    expect(n.message).toContain('and 1 more page')
  })

  it('uses within-page copy when both occurrences live in one page', () => {
    const n = duplicateIdToast(withinPage, () => {})
    expect(n.message).toContain('“Board”')
    expect(n.message).toContain('twice')
    // One page only — must not claim a second page.
    expect(n.message).not.toContain('Roadmap')
  })

  it('names the marker so diverged copies can be located by text search', () => {
    // The fragment is a real substring of the id, so searching it in the
    // editor finds the `<!-- id: ... -->` comment even when both copies'
    // prose has diverged.
    for (const w of [crossFile, withinPage, crossAndWithin]) {
      const n = duplicateIdToast(w, () => {})
      expect(n.message).toContain('the marker ends …11111111')
      expect(w.block_id).toContain('11111111')
    }
  })

  it('distinct collisions on the same page pair do not collapse into one message', () => {
    // The store dedups visible toasts on kind+message; without the marker
    // fragment two different ids on the same page pair were indistinguishable.
    const a = duplicateIdToast(crossFile, () => {})
    const other: DuplicateIDWarning = {
      ...crossFile,
      block_id: '22222222-2222-2222-2222-222222222222'
    }
    const b = duplicateIdToast(other, () => {})
    expect(a.message).not.toBe(b.message)
  })

  it('prefers the cross-page copy when a two-page collision also has a doubled page', () => {
    const n = duplicateIdToast(crossAndWithin, () => {})
    expect(n.message).toContain('“Board”')
    expect(n.message).toContain('“Projects/Roadmap”')
    expect(n.message).not.toContain('twice')
  })

  it('leads with the user-visible impact and avoids internal jargon', () => {
    for (const w of [crossFile, withinPage, crossAndWithin]) {
      const n = duplicateIdToast(w, () => {})
      expect(n.message).toContain('may open the wrong block')
      expect(n.message).not.toContain('((uuid))')
      expect(n.message).not.toContain('block id')
    }
  })

  it('offers a "Show file" action', () => {
    const n = duplicateIdToast(crossFile, () => {})
    expect(n.action?.label).toBe('Show file')
  })

  it('the CTA navigates to the first carrier page in preview mode', () => {
    const openPage = vi.fn()
    const n = duplicateIdToast(crossFile, openPage)
    void n.action?.run()
    expect(openPage).toHaveBeenCalledTimes(1)
    expect(openPage).toHaveBeenCalledWith(
      { notebook: 'Work', section: 'Sprint', page: 'Board' },
      'preview'
    )
  })
})
