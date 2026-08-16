import {
  RevealNotebookInOS,
  RevealPageInOS
} from '../../../bindings/silt/app.js'
import { copyPagePath, copyPageReference, copyText } from '../pageActions'
import { OPEN_PAGE_HISTORY_EVENT } from '../../components/editor/openPageHistory'
import { findNotebook, isLinkedNotebook, type DeleteTarget } from './navActions'
import { pageNodeId, sectionNodeId } from './navTree'
import { navigationActionError } from './useNavCrud.svelte'
import type { ActionPrompt } from './useNavCrud.svelte'
import type { NavigationTree } from './types'

export interface ContextMenuState {
  x: number
  y: number
  level: 'notebook' | 'section' | 'page'
  notebook: string
  section?: string
  page?: string
  anchorEl: HTMLElement | null
}

export interface UseSidebarContextMenuDeps {
  getTree: () => NavigationTree
  setActive: (patch: { section?: string }) => void
  onSelectSection: (section: string) => void
  /** Begin the rename flow (owned by the CRUD composable). */
  openRename: (
    level: 'notebook' | 'section' | 'page',
    notebook: string,
    section: string | undefined,
    currentName: string,
    page?: string
  ) => void
  /** Stage a delete confirmation (owned by the CRUD composable). */
  requestDelete: (target: DeleteTarget) => void
  /** Open the duplicate / child-section prompt (owned by the CRUD composable). */
  requestActionPrompt: (prompt: ActionPrompt) => void
  /** Inline page-create (owned by the CRUD composable). */
  handleCreatePageInline: (section: string) => void | Promise<void>
  /** Toggle a favorite (owned by the loader composable). */
  toggleFavorite: (ref: {
    notebook: string
    section: string
    page: string
  }) => void | Promise<void>
  /** Surface a transient action error (owned by the CRUD composable). */
  setActionError: (msg: string) => void
}

/**
 * Owns the sidebar context-menu state and its twelve handlers
 * (#62). The menu markup stays in `Sidebar.svelte`; this composable
 * supplies the open/close lifecycle, the visibility-rule deriveds the
 * template branches on, and the per-action dispatch.
 *
 * Rename / delete / duplicate dispositions are delegated to the CRUD
 * composable; reveal and clipboard actions are handled inline.
 */
export function useSidebarContextMenu(deps: UseSidebarContextMenuDeps) {
  let contextMenu = $state<ContextMenuState | null>(null)

  const contextMenuPageRef = $derived.by(() => {
    if (!contextMenu || contextMenu.level !== 'page') return null
    return {
      notebook: contextMenu.notebook,
      section: contextMenu.section ?? '',
      page: contextMenu.page ?? ''
    }
  })
  const contextNotebook = $derived(
    contextMenu ? findNotebook(deps.getTree(), contextMenu.notebook) : undefined
  )
  const contextUnavailable = $derived(!!contextNotebook?.disconnected)
  const contextMenuTargetId = $derived.by(() => {
    if (!contextMenu) return ''
    if (contextMenu.level === 'notebook') {
      return `notebook:${encodeURIComponent(contextMenu.notebook)}`
    }
    if (contextMenu.level === 'section') {
      return sectionNodeId(contextMenu.notebook, contextMenu.section ?? '')
    }
    return pageNodeId({
      notebook: contextMenu.notebook,
      section: contextMenu.section ?? '',
      page: contextMenu.page ?? ''
    })
  })
  const contextMenuUnlink = $derived(
    !!contextMenu &&
      contextMenu.level === 'notebook' &&
      isLinkedNotebook(findNotebook(deps.getTree(), contextMenu.notebook))
  )

  function openContextMenu(
    e: MouseEvent,
    level: 'notebook' | 'section' | 'page',
    notebook: string,
    section: string = '',
    page: string = ''
  ) {
    e.preventDefault()
    contextMenu = {
      x: e.clientX,
      y: e.clientY,
      level,
      notebook,
      section,
      page,
      anchorEl: e.currentTarget as HTMLElement
    }
  }

  function closeContextMenu() {
    contextMenu = null
  }

  function handleContextRename() {
    if (!contextMenu) return
    const { level, notebook, section, page } = contextMenu
    contextMenu = null
    if (level === 'page' && section !== undefined && page !== undefined) {
      deps.openRename('page', notebook, section, page, page)
    } else if (level === 'section' && section !== undefined) {
      deps.openRename('section', notebook, section, section)
    } else if (level === 'notebook') {
      deps.openRename('notebook', notebook, undefined, notebook)
    }
  }

  function handleContextDelete() {
    if (!contextMenu) return
    const { level, notebook, section, page } = contextMenu
    contextMenu = null
    deps.requestDelete({
      level,
      notebook,
      section,
      page
    })
  }

  async function handleContextReveal() {
    if (!contextMenu || contextUnavailable) return
    const target = contextMenu
    contextMenu = null
    try {
      if (target.level === 'notebook') {
        await RevealNotebookInOS(target.notebook)
      } else if (target.level === 'page') {
        await RevealPageInOS(
          target.notebook,
          target.section ?? '',
          target.page ?? ''
        )
      }
      deps.setActionError('')
    } catch (e) {
      deps.setActionError(navigationActionError(e, 'reveal'))
    }
  }

  function handleContextFavorite() {
    const ref = contextMenuPageRef
    contextMenu = null
    if (ref) void deps.toggleFavorite(ref)
  }

  function handleContextNewPage() {
    if (!contextMenu || contextUnavailable) return
    const section = contextMenu.section ?? ''
    contextMenu = null
    deps.setActive({ section })
    deps.onSelectSection(section)
    void deps.handleCreatePageInline(section)
  }

  function handleContextCopyPage(kind: 'path' | 'reference') {
    const ref = contextMenuPageRef
    contextMenu = null
    if (!ref) return
    void (kind === 'path' ? copyPagePath(ref) : copyPageReference(ref))
  }

  function handleContextCopyNotebook() {
    if (!contextMenu || contextMenu.level !== 'notebook') return
    const notebook = contextNotebook
    const text = notebook?.root_path || contextMenu.notebook
    contextMenu = null
    void copyText(text)
  }

  function openDuplicatePrompt() {
    const ref = contextMenuPageRef
    if (!ref || contextUnavailable) return
    deps.requestActionPrompt({
      kind: 'duplicate',
      ...ref,
      initialValue: `${ref.page} copy`
    })
    contextMenu = null
  }

  function handleContextPageHistory() {
    const ref = contextMenuPageRef
    contextMenu = null
    if (!ref) return
    const nonce =
      typeof crypto !== 'undefined' && 'randomUUID' in crypto
        ? crypto.randomUUID()
        : `page-history-${Date.now()}-${Math.random().toString(36).slice(2)}`
    window.dispatchEvent(
      new CustomEvent(OPEN_PAGE_HISTORY_EVENT, {
        detail: {
          notebook: ref.notebook,
          section: ref.section,
          page: ref.page,
          nonce
        }
      })
    )
  }

  function openChildSectionPrompt() {
    if (!contextMenu || contextMenu.level !== 'section' || contextUnavailable)
      return
    deps.requestActionPrompt({
      kind: 'child-section',
      notebook: contextMenu.notebook,
      section: contextMenu.section ?? '',
      initialValue: ''
    })
    contextMenu = null
  }

  return {
    get contextMenu() {
      return contextMenu
    },
    get contextMenuPageRef() {
      return contextMenuPageRef
    },
    get contextNotebook() {
      return contextNotebook
    },
    get contextUnavailable() {
      return contextUnavailable
    },
    get contextMenuTargetId() {
      return contextMenuTargetId
    },
    get contextMenuUnlink() {
      return contextMenuUnlink
    },
    openContextMenu,
    closeContextMenu,
    handleContextRename,
    handleContextDelete,
    handleContextReveal,
    handleContextFavorite,
    handleContextNewPage,
    handleContextCopyPage,
    handleContextCopyNotebook,
    openDuplicatePrompt,
    openChildSectionPrompt,
    handleContextPageHistory
  }
}

export type SidebarContextMenu = ReturnType<typeof useSidebarContextMenu>
