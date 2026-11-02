import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// RecordRecentPage is the only IPC the tab manager invokes directly (recents
// MRU bump on activation / confirmed save). GetOpenTabs/SetOpenTabs back the
// persistence layer's debounced write. All stubbed — never real IPC (#766).
const mocks = vi.hoisted(() => ({
  RecordRecentPage: vi.fn().mockResolvedValue(undefined),
  GetOpenTabs: vi.fn().mockResolvedValue({ open_tabs: [] }),
  SetOpenTabs: vi.fn().mockResolvedValue(undefined)
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    RecordRecentPage: mocks.RecordRecentPage,
    GetOpenTabs: mocks.GetOpenTabs,
    SetOpenTabs: mocks.SetOpenTabs
  })
)

import {
  createTabManager,
  type TabManagerController,
  type TabManagerDeps
} from './useTabManager.svelte'
import type { TabEntry } from '../tabs'

// Shared navigation triple so the getters reflect what the setters write —
// mirrors how App wires real $state. displayedTabs filters by
// getActiveNotebook, and syncActiveFromTab writes back through the setters.
function makeDeps(): TabManagerDeps {
  // $state so displayedTabs' $derived tracks the active notebook (a plain
  // property read inside the getter would NOT be a reactive dependency).
  const nav = $state({ notebook: 'Work', section: '', page: '' })
  return {
    getActiveNotebook: () => nav.notebook,
    setActiveNotebook: (nb: string) => {
      nav.notebook = nb
    },
    setActiveSection: (sec: string) => {
      nav.section = sec
    },
    setActivePage: (pg: string) => {
      nav.page = pg
    },
    getSettings: () => ({ ui: { enable_preview_tabs: false }, editor: {} }),
    confirmTemplateTransition: () => true,
    openTasksView: vi.fn()
  }
}

describe('useTabManager — pageMoved / renameTab / setTabSaveState (#768)', () => {
  let destroy: (() => void) | undefined
  let tabs: TabManagerController

  beforeEach(() => {
    mocks.RecordRecentPage.mockClear()
    mocks.SetOpenTabs.mockClear()
    destroy = $effect.root(() => {
      tabs = createTabManager(makeDeps())
      // Seed two pinned tabs in the same notebook/section.
      tabs.openPage(
        { notebook: 'Work', section: 'Inbox', page: 'Alpha' },
        'pin'
      )
      tabs.openPage({ notebook: 'Work', section: 'Inbox', page: 'Beta' }, 'pin')
    })
  })

  afterEach(() => {
    destroy?.()
    destroy = undefined
  })

  const find = (t: TabManagerController, page: string): TabEntry =>
    t.openTabs.find((x) => x.page === page)!

  it('pageMoved repoints only the matching tab section', () => {
    tabs.pageMoved('Work', 'Inbox', 'Archive', 'Alpha')
    expect(find(tabs, 'Alpha').section).toBe('Archive')
    // Sibling in the same notebook/section is untouched.
    expect(find(tabs, 'Beta').section).toBe('Inbox')
  })

  it('pageMoved ignores a different notebook', () => {
    tabs.pageMoved('Other', 'Inbox', 'Archive', 'Alpha')
    expect(find(tabs, 'Alpha').section).toBe('Inbox')
  })

  it('renameTab repoints the page by tab id', () => {
    const beta = find(tabs, 'Beta')
    tabs.renameTab(beta.id, 'Beta Renamed')
    expect(tabs.openTabs.find((t) => t.id === beta.id)!.page).toBe(
      'Beta Renamed'
    )
    expect(find(tabs, 'Alpha').page).toBe('Alpha')
  })

  it('renameTab leaves pages unchanged for an unknown id', () => {
    const pages = tabs.openTabs.map((t) => t.page).sort()
    tabs.renameTab('does-not-exist', 'Nope')
    expect(tabs.openTabs.map((t) => t.page).sort()).toEqual(pages)
  })

  it('setTabSaveState mirrors dirty / savePhase / saveError onto the header', () => {
    const alpha = find(tabs, 'Alpha')
    tabs.setTabSaveState(alpha.id, {
      phase: 'saving',
      dirty: true,
      error: null
    })
    const after = tabs.openTabs.find((t) => t.id === alpha.id)!
    expect(after.dirty).toBe(true)
    expect(after.savePhase).toBe('saving')
    expect(after.saveError).toBeNull()
    // Sibling untouched (fresh tabs default dirty=false, saveError=null).
    expect(find(tabs, 'Beta').dirty).toBe(false)
    expect(find(tabs, 'Beta').saveError).toBeNull()
  })

  it('setTabSaveState mutates in place — does not reassign openTabs (#814)', () => {
    // Replacing the openTabs array on every autosave state change forced App's
    // editor {#each} to re-render, re-passing equal props to the mounted
    // VirtualScrollContainer and re-invalidating its load $effect — flashing
    // the centered "Loading..." overlay on every save. Save-state must update
    // in place so the array (and thus the {#each}) is not churned.
    const alpha = find(tabs, 'Alpha')
    const arrayBefore = tabs.openTabs
    const entryBefore = tabs.openTabs.find((t) => t.id === alpha.id)!
    tabs.setTabSaveState(alpha.id, {
      phase: 'saving',
      dirty: true,
      error: null
    })
    // Array + entry identity preserved → no {#each} re-render churn.
    expect(tabs.openTabs).toBe(arrayBefore)
    expect(tabs.openTabs.find((t) => t.id === alpha.id)).toBe(entryBefore)
    // Fields still update — the tab strip's dirty/save badge reacts through
    // the deep $state proxy.
    const after = tabs.openTabs.find((t) => t.id === alpha.id)!
    expect(after.dirty).toBe(true)
    expect(after.savePhase).toBe('saving')
  })

  it('setTabSaveState bumps the recents MRU only on a confirmed save', () => {
    // Clear activations recorded during seeding so the assertion is isolated.
    mocks.RecordRecentPage.mockClear()
    const alpha = find(tabs, 'Alpha')
    // dirty → pending enters the tracker, but no save is confirmed yet.
    tabs.setTabSaveState(alpha.id, {
      phase: 'saving',
      dirty: true,
      error: null
    })
    expect(mocks.RecordRecentPage).not.toHaveBeenCalled()
    // A confirmed save resolves the pending entry → MRU bump fires.
    tabs.setTabSaveState(alpha.id, {
      phase: 'saved',
      dirty: false,
      error: null
    })
    expect(mocks.RecordRecentPage).toHaveBeenCalledWith(
      'Work',
      'Inbox',
      'Alpha'
    )
  })

  it('resetTabs clears open tabs + the active id', () => {
    expect(tabs.openTabs).toHaveLength(2)
    expect(tabs.activeTabId).not.toBe('')
    tabs.resetTabs()
    expect(tabs.openTabs).toHaveLength(0)
    expect(tabs.activeTabId).toBe('')
  })
})

describe('useTabManager — conflict-busy open gating (#1005)', () => {
  it('refuses an open at capacity when every eviction candidate is busy, warning loudly', () => {
    const busy = new Set<string>()
    let tabs: TabManagerController | undefined
    const destroy = $effect.root(() => {
      tabs = createTabManager({
        ...makeDeps(),
        getSettings: () => ({
          ui: { enable_preview_tabs: false, max_open_tabs: 2 },
          editor: {}
        }),
        getConflictBusyTabIds: () => busy
      })
      tabs.openPage(
        { notebook: 'Work', section: 'Inbox', page: 'Alpha' },
        'pin'
      )
      tabs.openPage({ notebook: 'Work', section: 'Inbox', page: 'Beta' }, 'pin')
    })
    const t = tabs!
    try {
      busy.add(t.openTabs[0]!.id)
      busy.add(t.openTabs[1]!.id)
      const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
      // The refusal is REPORTED to the caller: follow-through (type menus,
      // view switches) must be able to skip acting on the still-active page.
      expect(t.openPage({ notebook: 'Work', section: 'Inbox', page: 'Gamma' }, 'pin')).toBe(
        false
      )
      // Refused: the resolving surfaces stay mounted, nothing is evicted,
      // and the cap is not silently exceeded.
      expect(t.openTabs).toHaveLength(2)
      expect(t.openTabs.every((x) => x.page !== 'Gamma')).toBe(true)
      expect(warn).toHaveBeenCalledOnce()
      warn.mockRestore()

      // Once a tab settles, the same open succeeds — and says so.
      busy.delete(t.openTabs[0]!.id)
      expect(t.openPage({ notebook: 'Work', section: 'Inbox', page: 'Gamma' }, 'pin')).toBe(
        true
      )
      expect(t.openTabs).toHaveLength(2)
      expect(t.openTabs.some((x) => x.page === 'Gamma')).toBe(true)
    } finally {
      destroy?.()
    }
  })
})

describe('useTabManager — pageRenamed + displayedTabs scoping', () => {
  let destroy: (() => void) | undefined
  let tabs: TabManagerController

  beforeEach(() => {
    destroy = $effect.root(() => {
      tabs = createTabManager(makeDeps())
      tabs.openPage(
        { notebook: 'Work', section: 'Inbox', page: 'Alpha' },
        'pin'
      )
      tabs.openPage(
        { notebook: 'Personal', section: 'Day', page: 'Diary' },
        'pin'
      )
    })
  })

  afterEach(() => {
    destroy?.()
    destroy = undefined
  })

  it('pageRenamed updates only the matching locator', () => {
    tabs.pageRenamed({
      notebook: 'Work',
      section: 'Inbox',
      oldName: 'Alpha',
      newName: 'Alpha II'
    })
    expect(tabs.openTabs.find((t) => t.page === 'Alpha II')).toBeTruthy()
    expect(tabs.openTabs.find((t) => t.page === 'Alpha')).toBeFalsy()
    // The Personal tab is unaffected.
    expect(tabs.openTabs.find((t) => t.page === 'Diary')).toBeTruthy()
  })

  it('displayedTabs scopes to the active notebook context', () => {
    // Last opened was Personal → that is the active notebook context.
    expect(tabs.displayedTabs.map((t) => t.page)).toEqual(['Diary'])
    // Switching notebook context re-derives the visible set.
    tabs.selectNotebookContext('Work')
    expect(tabs.displayedTabs.map((t) => t.page)).toEqual(['Alpha'])
  })
})
