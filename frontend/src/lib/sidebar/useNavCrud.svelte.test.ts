import { beforeEach, describe, expect, it, vi, type Mock } from 'vitest'
import { useNavCrud, type UseNavCrudDeps } from './useNavCrud.svelte'
import type { NavigationTree } from './types'
import type { ActiveTriple } from './useNavLoader.svelte'

// Canonical IPC-boundary mock (AGENTS.md): the Wails bindings are stubbed so
// the inline-create flow runs without real IPC.
const mocks = vi.hoisted(() => ({
  CreatePage: vi.fn().mockResolvedValue('vault://Work/Notes/Untitled.md')
}))
vi.mock('$silt-app', () =>
  createAppIpcMocks({
    CreatePage: mocks.CreatePage
  })
)

function makeDeps(openResult: boolean): UseNavCrudDeps & {
  onSelectPage: Mock
  setActive: Mock
  setActiveView: Mock
  onSelectView: Mock
} {
  const active: ActiveTriple = { notebook: 'Work', section: '', page: '' }
  const tree: NavigationTree = {
    notebooks: [
      {
        name: 'Work',
        sections: [{ name: '', path: '', pages: [] }]
      }
    ]
  } as unknown as NavigationTree
  return {
    getActive: () => active,
    setActive: vi.fn((patch: Partial<ActiveTriple>) =>
      Object.assign(active, patch)
    ),
    getTree: () => tree,
    getActiveView: () => 'notes',
    setActiveView: vi.fn(),
    onSelectView: vi.fn(),
    onSelectNotebook: vi.fn(),
    onSelectSection: vi.fn(),
    // The #1005 refused-open contract: false = every tab conflict-busy.
    onSelectPage: vi.fn().mockReturnValue(openResult),
    getExpandedSections: () => new Set<string>(),
    setExpandedSections: vi.fn(),
    toggleSection: vi.fn(),
    setLocalExpansion: vi.fn(),
    loadNavigation: vi.fn().mockResolvedValue(undefined),
    loadNavigationPreferences: vi.fn().mockResolvedValue(undefined),
    handleSelectNotebook: vi.fn(),
    setShowNotebookDropdown: vi.fn()
  }
}

// #1005: an inline page-create whose open is refused (all tabs conflict-busy)
// must not run any follow-through — worst case, the deferred focus-page-title
// dispatch would land on the STILL-ACTIVE page's contenteditable h1 and the
// user's typing renames the wrong, conflict-held page.
describe('useNavCrud handleCreatePageInline refused-open gating (#1005)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.CreatePage.mockResolvedValue('vault://Work/Notes/Untitled.md')
  })

  it('refused open: page is created but no nav/view follow-through and no focus-page-title', async () => {
    const deps = makeDeps(false)
    const crud = useNavCrud(deps)

    const focusSpy = vi.fn()
    window.addEventListener('focus-page-title', focusSpy)
    try {
      await crud.handleCreatePageInline('Notes')
    } finally {
      window.removeEventListener('focus-page-title', focusSpy)
    }

    // The page creation itself is NOT rolled back — it already happened.
    expect(mocks.CreatePage).toHaveBeenCalledWith(
      'Work',
      'Notes',
      'Untitled',
      ''
    )
    expect(deps.onSelectPage).toHaveBeenCalledWith('Work', 'Notes', 'Untitled')
    // No follow-through against the still-active page.
    expect(deps.setActive).not.toHaveBeenCalled()
    expect(deps.setActiveView).not.toHaveBeenCalled()
    expect(deps.onSelectView).not.toHaveBeenCalled()
    expect(focusSpy).not.toHaveBeenCalled()
  })

  it('successful open: nav triple, view switch, and focus-page-title dispatch all run', async () => {
    const deps = makeDeps(true)
    const crud = useNavCrud(deps)

    const focusSpy = vi.fn()
    window.addEventListener('focus-page-title', focusSpy)
    try {
      await crud.handleCreatePageInline('Notes')
      // The focus dispatch is deferred (setTimeout 100ms) — flush it while
      // the listener is still attached.
      await new Promise((resolve) => setTimeout(resolve, 150))
    } finally {
      window.removeEventListener('focus-page-title', focusSpy)
    }

    expect(deps.onSelectPage).toHaveBeenCalledWith('Work', 'Notes', 'Untitled')
    expect(deps.setActive).toHaveBeenCalledWith({
      section: 'Notes',
      page: 'Untitled'
    })
    expect(deps.setActiveView).toHaveBeenCalledWith('notes')
    expect(deps.onSelectView).toHaveBeenCalledWith('notes')
    expect(focusSpy).toHaveBeenCalledOnce()
  })
})
