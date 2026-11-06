import { describe, expect, it, vi } from 'vitest'
import {
  duplicateIdToast,
  type DuplicateIDWarning
} from './duplicateIdToast'

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

  it('uses within-page copy when both occurrences live in one page', () => {
    const n = duplicateIdToast(withinPage, () => {})
    expect(n.message).toContain('“Board”')
    expect(n.message).toContain('twice')
    // One page only — must not claim a second page.
    expect(n.message).not.toContain('Roadmap')
  })

  it('leads with the user-visible impact and avoids internal jargon', () => {
    for (const w of [crossFile, withinPage]) {
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
