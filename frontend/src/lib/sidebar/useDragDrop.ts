import { MovePage } from '../../../bindings/silt/app.js'
import { sortByName, type NavOrderManager } from './navOrder'
import type { NavSection } from './types'
import { findSection } from './navTree'

// `section` is always passed explicitly by callers (`Sidebar.svelte` and
// `SidebarSection.svelte`). The empty string `''` is the sentinel for the
// notebook-root / section-less page group — it is a real, valid value, not
// "missing". Distinguishing it from `undefined` at the type level lets the
// drop handler branch on "section was passed" without conflating absent
// with empty (#369).
export interface DragItem {
  level: string
  name: string
  section: string
}

export interface DropTarget {
  level: string
  name: string
  before: boolean
}

export interface DragDropDeps {
  getActiveNotebook: () => string
  getActiveNotebookSections: () => NavSection[]
  navOrder: NavOrderManager
  onDragItemChange: (item: DragItem | null) => void
  onDropTargetChange: (target: DropTarget | null) => void
  onError: (msg: string) => void
  onMoved: () => Promise<void>
  onPageMoved?: (
    notebook: string,
    fromSection: string,
    toSection: string,
    page: string
  ) => void
}

/**
 * Manages drag-and-drop logic for the sidebar: section reorder, page reorder,
 * and page→section cross-section moves.
 *
 * The component creates one instance and delegates all DnD events to it.
 */
export class DragDropManager {
  private dragItem: DragItem | null = null
  private dropTarget: DropTarget | null = null
  private deps: DragDropDeps

  constructor(deps: DragDropDeps) {
    this.deps = deps
  }

  handleDragStart(
    e: DragEvent,
    level: string,
    name: string,
    section: string = ''
  ) {
    this.dragItem = { level, name, section }
    this.deps.onDragItemChange(this.dragItem)
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', name)
    }
  }

  handleDragOver(
    e: DragEvent,
    level: string,
    name: string,
    targetIdentity: string = name
  ) {
    if (!this.dragItem) return
    // Same-level reorder (section↔section, page↔page) is always allowed.
    // Page→section drop (move into section, #177) is also allowed.
    if (
      this.dragItem.level !== level &&
      !(this.dragItem.level === 'page' && level === 'section')
    ) {
      return
    }
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const before = e.clientY < rect.top + rect.height / 2
    this.dropTarget = { level, name: targetIdentity, before }
    this.deps.onDropTargetChange(this.dropTarget)
  }

  handleDragLeave() {
    this.dropTarget = null
    this.deps.onDropTargetChange(null)
  }

  async handleDrop(
    e: DragEvent,
    level: string,
    targetName: string,
    notebook: string = '',
    section: string = ''
  ) {
    e.preventDefault()
    e.stopPropagation()
    if (!this.dragItem) {
      this.dropTarget = null
      this.deps.onDropTargetChange(null)
      return
    }

    const isPageToSection =
      this.dragItem.level === 'page' && level === 'section'
    if (this.dragItem.level !== level && !isPageToSection) {
      this.clear()
      return
    }
    // The notebook-root drop zone (#177) uses targetName='__root__' to mean
    // "move this page out of any section" (target section = ''). It's the
    // page→section path with an empty target section.
    const isRootDrop = isPageToSection && targetName === '__root__'
    if (this.dragItem.name === targetName && !isPageToSection) {
      this.clear()
      return
    }

    if (isRootDrop) {
      const fromSection = this.dragItem.section ?? ''
      if (fromSection === '') {
        // Already at root — no-op.
        this.clear()
        return
      }
      try {
        await MovePage(
          notebook ?? this.deps.getActiveNotebook(),
          fromSection,
          '',
          this.dragItem.name
        )
        await this.deps.onMoved()
        this.deps.onPageMoved?.(
          notebook ?? this.deps.getActiveNotebook(),
          fromSection,
          '',
          this.dragItem.name
        )
      } catch (err) {
        this.deps.onError(
          err instanceof Error ? err.message : 'Failed to move page'
        )
      }
    } else if (isPageToSection && section !== undefined) {
      // Page dropped onto a section header → cross-section move (#177).
      const fromSection = this.dragItem.section ?? ''
      const toSection = section
      if (fromSection === toSection) {
        this.clear()
        return
      }
      try {
        await MovePage(
          notebook ?? this.deps.getActiveNotebook(),
          fromSection,
          toSection,
          this.dragItem.name
        )
        await this.deps.onMoved()
        this.deps.onPageMoved?.(
          notebook ?? this.deps.getActiveNotebook(),
          fromSection,
          toSection,
          this.dragItem.name
        )
      } catch (err) {
        this.deps.onError(
          err instanceof Error ? err.message : 'Failed to move page'
        )
      }
    } else if (level === 'section' && notebook) {
      const parentPath = targetName.split('/').slice(0, -1).join('/')
      const siblings = parentPath
        ? (findSection(this.deps.getActiveNotebookSections(), parentPath)
            ?.children ?? [])
        : this.deps
            .getActiveNotebookSections()
            .filter((section) => section.path !== '')
      const orderKey = parentPath ? `${notebook}/${parentPath}` : notebook
      const sorted = sortByName(
        siblings,
        this.deps.navOrder.current.sections[orderKey]
      )
      const names = sorted.map((s) => s.name)
      const dragLeaf =
        this.dragItem.name.split('/').at(-1) ?? this.dragItem.name
      const fromIdx = names.indexOf(dragLeaf)
      const targetLeaf = targetName.split('/').at(-1) ?? ''
      const toIdx = names.indexOf(targetLeaf)
      if (fromIdx === -1 || toIdx === -1) {
        this.clear()
        return
      }
      names.splice(fromIdx, 1)
      const insertAt = this.dropTarget?.before
        ? names.indexOf(targetLeaf)
        : names.indexOf(targetLeaf) + 1
      names.splice(insertAt, 0, dragLeaf)
      await this.deps.navOrder.persistSectionOrder(notebook, parentPath, names)
    } else if (level === 'page' && section !== undefined) {
      // Reorder among pages within a section. The `section` parameter is
      // `''` for the notebook-root / section-less page group, which is a
      // real value: it maps to the synthetic section whose `name === ''`
      // supplies the root-page list (rendered at Sidebar.svelte:866) and
      // whose persisted key is `${notebook}/` — matching Go's
      // updateNavOrderForMove (app_rename.go:316-318). Use a strict
      // `section !== undefined` check so the falsy `''` is not mistaken
      // for "missing", which used to short-circuit this branch and silently
      // no-op root-page reorders (#369).
      const sec =
        section === ''
          ? this.deps
              .getActiveNotebookSections()
              .find((candidate) => candidate.path === '')
          : findSection(this.deps.getActiveNotebookSections(), section)
      const activeNotebook = notebook ?? this.deps.getActiveNotebook()
      const sectionKey = `${activeNotebook}/${section}`
      const sorted = sortByName(
        sec?.pages ?? [],
        this.deps.navOrder.current.pages[sectionKey]
      )
      const names = sorted.map((p) => p.name)
      const fromIdx = names.indexOf(this.dragItem.name)
      const toIdx = names.indexOf(targetName)
      if (fromIdx === -1 || toIdx === -1) {
        this.clear()
        return
      }
      names.splice(fromIdx, 1)
      const insertAt = this.dropTarget?.before
        ? names.indexOf(targetName)
        : names.indexOf(targetName) + 1
      names.splice(insertAt, 0, this.dragItem.name)
      await this.deps.navOrder.persistPageOrder(activeNotebook, section, names)
    }

    this.clear()
  }

  handleDragEnd() {
    this.clear()
  }

  private clear() {
    this.dragItem = null
    this.dropTarget = null
    this.deps.onDragItemChange(null)
    this.deps.onDropTargetChange(null)
  }
}
