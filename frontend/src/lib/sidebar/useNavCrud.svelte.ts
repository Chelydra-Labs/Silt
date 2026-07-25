import { SvelteSet } from 'svelte/reactivity'
import {
  CreateNotebook,
  CreatePage,
  CreateSection,
  DeleteNotebook,
  DeletePage,
  DeleteSection,
  DuplicatePage,
  PickLinkedNotebook,
  PickNotebookFolder,
  RenameNotebook,
  RenamePage,
  RenameSection,
  SetNavigationSectionExpanded,
  UnlinkNotebook
} from '../../../bindings/silt/app.js'
import { coerceIPCError } from '../ipcError'
import {
  deleteDisposition,
  deleteTargetLabel,
  findNotebook,
  linkedNotebookId,
  reconcileActiveAfterDelete,
  type DeleteTarget
} from './navActions'
import { generateUniquePageName as generateUniquePageNameHelper } from './navTree'
import type { NavigationTree } from './types'
import type { ActiveTriple } from './useNavLoader.svelte'

export interface RenameCtx {
  level: 'notebook' | 'section' | 'page'
  notebook: string
  section?: string
  page?: string
}

export interface ActionPrompt {
  kind: 'duplicate' | 'child-section'
  notebook: string
  section: string
  page?: string
  initialValue: string
}

export type CreateMode = '' | 'notebook' | 'section' | 'page'

/**
 * A staged delete target with its human-readable label resolved once, at
 * open time, from `deleteTargetLabel`. Named (rather than an inline
 * `DeleteTarget & { label: string }`) so the Svelte template type bridge
 * resolves it cleanly across the module boundary.
 */
export interface LabeledDeleteTarget extends DeleteTarget {
  label: string
}

/**
 * Map a thrown IPC error to a stable, human-facing message for the navigation
 * action that failed. Pure — shared by the CRUD state machine and the context
 * menu's Reveal handler.
 */
export function navigationActionError(
  error: unknown,
  action: 'duplicate' | 'reveal' | 'create'
): string {
  const parsed = coerceIPCError(error)
  switch (parsed.code) {
    case 'page_exists':
    case 'navigation_conflict':
      return action === 'duplicate'
        ? 'A page with that name already exists in this section.'
        : 'An item with that name already exists here.'
    case 'invalid_navigation_path':
      return 'That name cannot be used here.'
    case 'navigation_not_found':
      return 'That page is no longer available.'
    case 'navigation_unavailable':
      return 'This linked notebook is offline or unavailable.'
    case 'navigation_reveal_failed':
      return 'The item could not be revealed in the file manager.'
    case 'navigation_duplicate':
      return 'The page could not be duplicated.'
    default:
      return (
        parsed.message ||
        (action === 'reveal'
          ? 'The item could not be revealed in the file manager.'
          : 'The action could not be completed.')
      )
  }
}

export interface UseNavCrudDeps {
  getActive: () => ActiveTriple
  setActive: (patch: Partial<ActiveTriple>) => void
  getTree: () => NavigationTree
  getActiveView: () => string
  setActiveView: (view: string) => void
  onSelectView: (view: string) => void
  onSelectNotebook: (notebook: string) => void
  onSelectSection: (section: string) => void
  onSelectPage: (notebook: string, section: string, page: string) => void
  getExpandedSections: () => Set<string>
  setExpandedSections: (next: SvelteSet<string>) => void
  toggleSection: (path: string) => void
  setLocalExpansion: (notebook: string, path: string, expanded: boolean) => void
  loadNavigation: () => Promise<void>
  loadNavigationPreferences: () => Promise<void>
  /** Hybrid handler owned by the host (writes active props + expansion). */
  handleSelectNotebook: (notebook: string) => void
  setShowNotebookDropdown: (visible: boolean) => void
}

/**
 * Owns the create/rename/delete state machine for the sidebar nav tree:
 * the shared NamePromptDialog (create/rename), the action-prompt dialog
 * (duplicate / new child section), the delete-confirmation dialog, and the
 * inline page-create flow.
 *
 * State atoms are exposed as getters; read them as properties
 * (`crud.deleteTarget`) to preserve Svelte 5 reactivity.
 */
export function useNavCrud(deps: UseNavCrudDeps) {
  // Creation/rename modal state
  let createMode = $state<CreateMode>('')
  let editingMode = $state<'create' | 'rename'>('create')
  let renameCtx = $state<RenameCtx | null>(null)
  let newName = $state('')
  let createError = $state('')
  let creating = $state(false)

  // Action-prompt (duplicate / child section) state
  let actionPrompt = $state<ActionPrompt | null>(null)
  let actionPromptError = $state('')
  let actionBusy = $state(false)
  let actionError = $state('')

  // Delete confirmation dialog state. The label is derived once at open time
  // from deleteTargetLabel so the dialog copy stays stable across re-renders.
  let deleteTarget = $state<LabeledDeleteTarget | null>(null)
  // trash | unlink | permanent — linked page/section hard-delete must not
  // claim vault trash recovery (#100 / #646).
  let deleteTargetDisposition = $derived(
    deleteTarget ? deleteDisposition(deps.getTree(), deleteTarget) : 'trash'
  )

  function openCreate(mode: 'notebook' | 'section') {
    createMode = mode
    editingMode = 'create'
    renameCtx = null
    newName = ''
    createError = ''
  }

  function openRename(
    level: 'notebook' | 'section' | 'page',
    notebook: string,
    section: string | undefined,
    currentName: string,
    page?: string
  ) {
    createMode = level
    editingMode = 'rename'
    renameCtx = { level, notebook, section, page }
    newName = currentName
    createError = ''
  }

  function namePromptTitle(): string {
    const kind =
      createMode === 'page'
        ? 'Page'
        : createMode === 'notebook'
          ? 'Notebook'
          : 'Section'
    return editingMode === 'rename' ? `Rename ${kind}` : `New ${kind}`
  }

  function namePromptLabel(): string {
    if (createMode === 'notebook') return 'Notebook name'
    if (createMode === 'page') return 'Page name'
    return 'Section name'
  }

  function namePromptPlaceholder(): string {
    if (editingMode === 'rename') {
      if (createMode === 'notebook') return 'New notebook name…'
      if (createMode === 'page') return 'New page name…'
      return 'New section name…'
    }
    if (createMode === 'notebook') return 'Notebook name…'
    if (createMode === 'page') return 'Page name…'
    return 'Section name…'
  }

  function namePromptConfirmLabel(): string {
    if (creating) {
      return editingMode === 'rename' ? 'Renaming…' : 'Creating…'
    }
    return editingMode === 'rename' ? 'Rename' : 'Create'
  }

  function closeNamePrompt() {
    if (creating) return
    createMode = ''
    createError = ''
    renameCtx = null
    newName = ''
  }

  async function handleOpenNotebookFolder() {
    try {
      creating = true
      createError = ''
      const name = await PickNotebookFolder()
      if (!name) {
        // user cancelled
        deps.setShowNotebookDropdown(false)
        return
      }
      await deps.loadNavigation()
      deps.handleSelectNotebook(name)
      deps.setShowNotebookDropdown(false)
    } catch (e) {
      createError = e instanceof Error ? e.message : String(e)
      createMode = 'notebook'
      editingMode = 'create'
      renameCtx = null
      newName = ''
    } finally {
      creating = false
    }
  }

  // #100: link an external folder (e.g. a synced SharePoint mount) as a
  // notebook, edited in place. The folder is never copied into the vault.
  async function handleLinkExternalNotebook() {
    try {
      creating = true
      createError = ''
      const ln = await PickLinkedNotebook()
      if (!ln || !ln.id) {
        deps.setShowNotebookDropdown(false)
        return // user cancelled
      }
      await deps.loadNavigation()
      deps.handleSelectNotebook(ln.display_name)
      deps.setShowNotebookDropdown(false)
    } catch (e) {
      createError = e instanceof Error ? e.message : String(e)
      deps.setShowNotebookDropdown(false)
    } finally {
      creating = false
    }
  }

  async function handleCreate(nameFromDialog?: string) {
    const trimmed = (nameFromDialog ?? newName).trim()
    if (trimmed === '') return
    newName = trimmed
    creating = true
    createError = ''
    const active = deps.getActive()
    try {
      if (editingMode === 'rename' && renameCtx) {
        if (renameCtx.level === 'notebook') {
          await RenameNotebook(renameCtx.notebook, trimmed)
          await deps.loadNavigation()
          if (active.notebook === renameCtx.notebook) {
            deps.setActive({ notebook: trimmed })
            deps.handleSelectNotebook(trimmed)
          }
        } else if (renameCtx.level === 'section') {
          const oldPath = renameCtx.section ?? ''
          const parentPath = oldPath.split('/').slice(0, -1).join('/')
          const nextPath = parentPath ? `${parentPath}/${trimmed}` : trimmed
          await RenameSection(renameCtx.notebook, oldPath, nextPath)
          await deps.loadNavigation()
          await deps.loadNavigationPreferences()
          if (active.section === renameCtx.section) {
            deps.setActive({ section: nextPath })
            deps.onSelectSection(nextPath)
          }
        } else if (renameCtx.level === 'page') {
          await RenamePage(
            renameCtx.notebook,
            renameCtx.section ?? '',
            renameCtx.page ?? '',
            trimmed
          )
          await deps.loadNavigation()
          if (active.page === renameCtx.page) {
            deps.setActive({ page: trimmed })
          }
          window.dispatchEvent(
            new CustomEvent('page-renamed', {
              detail: {
                notebook: renameCtx.notebook,
                section: renameCtx.section,
                oldName: renameCtx.page,
                newName: trimmed
              }
            })
          )
        }
      } else if (createMode === 'notebook') {
        await CreateNotebook(trimmed)
        await deps.loadNavigation()
        deps.handleSelectNotebook(trimmed)
      } else if (createMode === 'section') {
        await CreateSection(active.notebook, '', trimmed)
        await deps.loadNavigation()
        deps.setActive({ section: trimmed })
        deps.onSelectSection(trimmed)
        deps.setExpandedSections(
          new SvelteSet([...deps.getExpandedSections(), trimmed])
        )
        deps.setLocalExpansion(active.notebook, trimmed, true)
        await SetNavigationSectionExpanded(active.notebook, trimmed, true)
      }
      createMode = ''
      newName = ''
      renameCtx = null
      createError = ''
    } catch (e) {
      createError = e instanceof Error ? e.message : String(e)
    } finally {
      creating = false
    }
  }

  // --- Inline page creation (#83) ---
  // OneNote model: create "Untitled" immediately and navigate; the editor's
  // title field auto-focuses so the user can type the real name inline.
  async function handleCreatePageInline(sectionName: string) {
    creating = true
    try {
      const pageName = generateUniquePageName(sectionName)
      const active = deps.getActive()
      await CreatePage(active.notebook, sectionName, pageName, '')
      await deps.loadNavigation()
      deps.setActive({ section: sectionName, page: pageName })
      deps.onSelectPage(active.notebook, sectionName, pageName)
      deps.setActiveView('notes')
      deps.onSelectView('notes')
      // Signal the editor to focus the title for inline rename.
      setTimeout(() => {
        window.dispatchEvent(new CustomEvent('focus-page-title'))
      }, 100)
    } catch (e) {
      actionError = navigationActionError(e, 'create')
    } finally {
      creating = false
    }
  }

  function generateUniquePageName(sectionName: string): string {
    const active = deps.getActive()
    return generateUniquePageNameHelper(
      deps.getTree(),
      active.notebook,
      sectionName
    )
  }

  function closeActionPrompt() {
    if (actionBusy) return
    actionPrompt = null
    actionPromptError = ''
  }

  async function confirmActionPrompt(name: string) {
    if (!actionPrompt) return
    const prompt = actionPrompt
    actionBusy = true
    actionPromptError = ''
    try {
      if (prompt.kind === 'duplicate') {
        await DuplicatePage(
          prompt.notebook,
          prompt.section,
          prompt.page ?? '',
          name
        )
        await deps.loadNavigation()
        deps.onSelectPage(prompt.notebook, prompt.section, name)
      } else {
        await CreateSection(prompt.notebook, prompt.section, name)
        const childPath = prompt.section ? `${prompt.section}/${name}` : name
        await deps.loadNavigation()
        deps.setActive({ notebook: prompt.notebook, section: childPath })
        deps.onSelectSection(childPath)
        if (prompt.section && !deps.getExpandedSections().has(prompt.section)) {
          deps.toggleSection(prompt.section)
        }
        if (!deps.getExpandedSections().has(childPath)) {
          deps.toggleSection(childPath)
        }
      }
      actionPrompt = null
      actionError = ''
    } catch (e) {
      actionPromptError = navigationActionError(
        e,
        prompt.kind === 'duplicate' ? 'duplicate' : 'create'
      )
    } finally {
      actionBusy = false
    }
  }

  /**
   * Called by the context menu to stage a delete/unlink confirmation.
   * Computes the human-readable label once, at open time.
   */
  function requestDelete(target: DeleteTarget) {
    deleteTarget = { ...target, label: deleteTargetLabel(target) }
  }

  /** Called by the context menu to open the duplicate / child-section prompt. */
  function requestActionPrompt(prompt: ActionPrompt) {
    actionPrompt = prompt
    actionPromptError = ''
  }

  async function confirmDelete() {
    if (!deleteTarget) return
    const target = deleteTarget
    deleteTarget = null
    try {
      if (target.level === 'page' && target.page !== undefined) {
        await DeletePage(target.notebook, target.section ?? '', target.page)
      } else if (target.level === 'section' && target.section !== undefined) {
        await DeleteSection(target.notebook, target.section)
      } else if (target.level === 'notebook') {
        // #100: a linked notebook is UNLINKED (files untouched), not moved to
        // trash. Vault notebooks are deleted as before.
        const id = linkedNotebookId(
          findNotebook(deps.getTree(), target.notebook)
        )
        if (id !== null) {
          await UnlinkNotebook(id)
        } else {
          await DeleteNotebook(target.notebook)
        }
      }
      await deps.loadNavigation()
      await deps.loadNavigationPreferences()
      const next = reconcileActiveAfterDelete(target, deps.getActive())
      deps.setActive({
        notebook: next.notebook,
        section: next.section,
        page: next.page
      })
    } catch (e) {
      console.error('Delete failed:', e)
    }
  }

  function cancelDelete() {
    deleteTarget = null
  }

  return {
    // create/rename dialog state
    get createMode() {
      return createMode
    },
    get editingMode() {
      return editingMode
    },
    get renameCtx() {
      return renameCtx
    },
    get newName() {
      return newName
    },
    get createError() {
      return createError
    },
    get creating() {
      return creating
    },
    // action prompt state
    get actionPrompt() {
      return actionPrompt
    },
    get actionPromptError() {
      return actionPromptError
    },
    get actionBusy() {
      return actionBusy
    },
    get actionError() {
      return actionError
    },
    // delete dialog state
    get deleteTarget(): LabeledDeleteTarget | null {
      return deleteTarget
    },
    get deleteTargetDisposition() {
      return deleteTargetDisposition
    },
    // name-prompt helpers
    namePromptTitle,
    namePromptLabel,
    namePromptPlaceholder,
    namePromptConfirmLabel,
    closeNamePrompt,
    // create/rename handlers
    openCreate,
    openRename,
    handleCreate,
    handleOpenNotebookFolder,
    handleLinkExternalNotebook,
    handleCreatePageInline,
    // action-prompt handlers
    confirmActionPrompt,
    closeActionPrompt,
    requestActionPrompt,
    // delete handlers
    confirmDelete,
    cancelDelete,
    requestDelete,
    // misc
    setActionError: (msg: string) => {
      actionError = msg
    },
    navigationActionError
  }
}

export type NavCrud = ReturnType<typeof useNavCrud>
