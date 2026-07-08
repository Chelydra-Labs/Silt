import { describe, expect, it, vi } from 'vitest'
import { reMintToast, type ReMintWarning } from './reMintToast'

describe('reMintToast', () => {
  const baseWarning: ReMintWarning = {
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    minted_count: 4,
    prior_count: 7
  }

  it('builds a sticky info notification (no auto-dismiss)', () => {
    const n = reMintToast(baseWarning, () => {})
    expect(n.kind).toBe('info')
    expect(n.autoDismissMs).toBe(0)
  })

  it('names the affected page and leads with the user-visible impact (broken links)', () => {
    const n = reMintToast(baseWarning, () => {})
    expect(n.message).toContain('“Daily”')
    // User-facing framing, not internal jargon.
    expect(n.message).toContain('links between notes')
    expect(n.message).not.toContain('((uuid))')
    expect(n.message).not.toContain('block id')
  })

  it('pluralizes "block" for counts > 1', () => {
    expect(
      reMintToast({ ...baseWarning, minted_count: 4 }, () => {}).message
    ).toContain('4 blocks')
  })

  it('uses singular "block" for a count of 1', () => {
    expect(
      reMintToast({ ...baseWarning, minted_count: 1 }, () => {}).message
    ).toContain('1 block ')
  })

  it('offers a "Show file" action', () => {
    const n = reMintToast(baseWarning, () => {})
    expect(n.action?.label).toBe('Show file')
  })

  it('the CTA navigates to the affected page in preview mode', () => {
    const openPage = vi.fn()
    const n = reMintToast(baseWarning, openPage)
    void n.action?.run()
    expect(openPage).toHaveBeenCalledTimes(1)
    expect(openPage).toHaveBeenCalledWith(
      { notebook: 'Work', section: 'Journal', page: 'Daily' },
      'preview'
    )
  })
})
