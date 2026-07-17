import { afterEach, describe, expect, it } from 'vitest'
import { setActiveLocation } from './location.svelte'
import {
  captureUiLocation,
  clearSelectionFocus,
  clearSelectionFocusIfPage,
  formatUiLocationForPrompt,
  recordSelectionFocus,
  setOpenTabsProvider
} from './ui-location'

afterEach(() => {
  setOpenTabsProvider(null)
  clearSelectionFocus()
  setActiveLocation('', '', '')
})

describe('ui-location', () => {
  it('captures empty location when nothing is active', () => {
    const snap = captureUiLocation()
    expect(snap.notebook).toBe('')
    expect(snap.section).toBe('')
    expect(snap.page).toBe('')
    expect(snap.blockId).toBeUndefined()
    expect(snap.openTabs).toEqual([])
  })

  it('includes active page triple and open tabs from the provider', () => {
    setActiveLocation('Recipes', 'Baking', 'Pie')
    setOpenTabsProvider(() => [
      {
        notebook: 'Recipes',
        section: 'Baking',
        page: 'Pie',
        active: true
      },
      {
        notebook: 'Recipes',
        section: 'Baking',
        page: 'Bread',
        preview: true,
        active: false
      }
    ])
    const snap = captureUiLocation()
    expect(snap).toMatchObject({
      notebook: 'Recipes',
      section: 'Baking',
      page: 'Pie'
    })
    expect(snap.openTabs).toHaveLength(2)
    expect(snap.openTabs[0].active).toBe(true)
    expect(snap.openTabs[1].preview).toBe(true)
  })

  it('includes block id only when selection is on the active page', () => {
    setActiveLocation('Work', 'Journal', 'Daily')
    recordSelectionFocus({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blockId: 'block-abc'
    })
    expect(captureUiLocation().blockId).toBe('block-abc')

    // Navigate away — block id must not leak onto another page.
    setActiveLocation('Work', 'Journal', 'Other')
    expect(captureUiLocation().blockId).toBeUndefined()
  })

  it('omits block id when selection has none', () => {
    setActiveLocation('Work', '', 'Notes')
    recordSelectionFocus({
      notebook: 'Work',
      section: '',
      page: 'Notes'
    })
    expect(captureUiLocation().blockId).toBeUndefined()
  })

  it('clears block id when a later selection has no block id', () => {
    setActiveLocation('Work', 'Journal', 'Daily')
    recordSelectionFocus({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blockId: 'block-old'
    })
    expect(captureUiLocation().blockId).toBe('block-old')
    recordSelectionFocus({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily'
    })
    expect(captureUiLocation().blockId).toBeUndefined()
  })

  it('clearSelectionFocusIfPage only clears matching page', () => {
    setActiveLocation('Work', 'Journal', 'Daily')
    recordSelectionFocus({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blockId: 'block-1'
    })
    clearSelectionFocusIfPage('Work', 'Journal', 'Other')
    expect(captureUiLocation().blockId).toBe('block-1')
    clearSelectionFocusIfPage('Work', 'Journal', 'Daily')
    expect(captureUiLocation().blockId).toBeUndefined()
  })

  it('formats prompt lines without dumping page bodies', () => {
    const text = formatUiLocationForPrompt({
      notebook: 'Recipes',
      section: 'Baking',
      page: 'Pie',
      blockId: 'b1',
      openTabs: [
        {
          notebook: 'Recipes',
          section: 'Baking',
          page: 'Pie',
          active: true
        }
      ]
    })
    expect(text).toContain('Current page: Recipes/Baking/Pie')
    expect(text).toContain('Focused block id: b1')
    expect(text).toContain('Recipes/Baking/Pie (active)')
    expect(text).toContain('identifiers only')
    expect(text).not.toMatch(/flour|sugar|ingredients/i)
  })

  it('formats missing location explicitly', () => {
    const text = formatUiLocationForPrompt({
      notebook: '',
      section: '',
      page: '',
      openTabs: []
    })
    expect(text).toContain('Current page: (none)')
    expect(text).toContain('Focused block id: (none)')
    expect(text).toContain('Open tabs: (none)')
  })
})
