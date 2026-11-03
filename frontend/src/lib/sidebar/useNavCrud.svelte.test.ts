import { beforeEach, describe, expect, it, vi, type Mock } from 'vitest'
import { useNavCrud, type UseNavCrudDeps } from './useNavCrud.svelte'
import type { NavigationTree } from './types'
import type { ActiveTriple } from './useNavLoader.svelte'

// Canonical IPC-boundary mock (AGENTS.md): the Wails bindings are stubbed so
// the inline-create flow runs without real IPC.
const mocks = vi.hoisted(() => ({
  CreatePage: vi.fn().mockResolvedValue('vault://Work/Notes/Untitled.md'),
  RenamePage: vi.fn().mockResolvedValue(undefined)
}))
vi.mock('$silt-app', () =>
  createAppIpcMocks({
    CreatePage: mocks.CreatePage,
    RenamePage: mocks.RenamePage
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

// #1009: navigationActionError must pass load-bearing prose through — a
// duplicate whose write failed verification carries the manual recovery path
// (where the retained copies live) in its message; a fixed string would drop
// it, and the raw JSON envelope would bury it.
describe('navigationActionError prose passthrough', () => {
  const typedError = (code: string, message: string) =>
    new Error(JSON.stringify({ code, message }))

  it('maps a generic navigation_duplicate wrap to the fixed string (no Go errno prose)', () => {
    const deps = makeDeps(true)
    const { navigationActionError } = useNavCrud(deps)
    const msg = navigationActionError(
      typedError(
        'navigation_duplicate',
        'duplicate page cannot be written: open /vault/Work/Copy.md: permission denied'
      ),
      'duplicate'
    )
    // The generic wrap embeds raw Go error prose (paths, errno) that carries
    // no recovery information — the dialog keeps the calm fixed string.
    expect(msg).toBe('The page could not be duplicated.')
    expect(msg).not.toContain('permission denied')
  })

  it('keeps the fixed duplicate string as the empty-message floor', () => {
    const deps = makeDeps(true)
    const { navigationActionError } = useNavCrud(deps)
    const msg = navigationActionError(
      typedError('navigation_duplicate', ''),
      'duplicate'
    )
    expect(msg).toBe('The page could not be duplicated.')
  })

  it('renders verified_write_failed prose via the default branch', () => {
    const deps = makeDeps(true)
    const { navigationActionError } = useNavCrud(deps)
    const msg = navigationActionError(
      typedError(
        'verified_write_failed',
        'Note.md could not be written safely and its previous version could not be restored'
      ),
      'duplicate'
    )
    expect(msg).toBe(
      'Note.md could not be written safely and its previous version could not be restored'
    )
  })
})

// #1009: the rename dialog must render a typed failure's recovery prose —
// the raw e.message is the JSON envelope, which buries the named recovery
// copies.
describe('handleCreate rename typed-error prose (#1009)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows coerced recovery prose, not the JSON envelope, when RenamePage fails', async () => {
    mocks.RenamePage.mockRejectedValueOnce(
      new Error(
        JSON.stringify({
          code: 'verified_write_failed',
          message:
            'Note.md could not be written safely and the previous version was restored. Your unsaved text is kept at /vault/.system/writebackups/Note.md.pending'
        })
      )
    )
    const deps = makeDeps(true)
    const crud = useNavCrud(deps)
    crud.openRename('page', 'Work', 'Notes', 'Note', 'Note')

    await crud.handleCreate('Renamed')

    expect(mocks.RenamePage).toHaveBeenCalledWith(
      'Work',
      'Notes',
      'Note',
      'Renamed'
    )
    expect(crud.createError).toContain('Your unsaved text is kept at')
    expect(crud.createError).not.toContain('{"code"')
  })
})
