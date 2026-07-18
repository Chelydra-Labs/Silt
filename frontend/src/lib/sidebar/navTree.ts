import type {
  NavNotebook,
  NavSection,
  NavigationPageRef,
  NavigationTree
} from './types'

export type VisibleTreeNode =
  | {
      id: string
      kind: 'section'
      notebook: string
      section: string
      label: string
      parentId: string | null
      expandable: true
    }
  | {
      id: string
      kind: 'page'
      notebook: string
      section: string
      page: string
      label: string
      parentId: string | null
      expandable: false
    }

export function sectionNodeId(notebook: string, path: string): string {
  return `section:${encodeURIComponent(notebook)}:${encodeURIComponent(path)}`
}

export function pageNodeId(ref: NavigationPageRef): string {
  return `page:${encodeURIComponent(ref.notebook)}:${encodeURIComponent(ref.section)}:${encodeURIComponent(ref.page)}`
}

/** Fill canonical paths at the IPC edge; older generated models mark Path optional. */
export function normalizeNavigationTree(input: {
  notebooks?: Array<{
    name?: string
    sections?: any[]
    source?: string
    root_path?: string
    disconnected?: boolean
  }>
}): NavigationTree {
  const normalizeSections = (sections: any[] = [], parent = ''): NavSection[] =>
    sections.map((section) => {
      const path = String(
        section.path ??
          (parent ? `${parent}/${section.name}` : (section.name ?? ''))
      ).replaceAll('\\', '/')
      return {
        name: String(section.name ?? ''),
        path,
        pages: (section.pages ?? []).map((page: any) => ({
          name: String(page.name ?? ''),
          count: Number(page.count ?? 0)
        })),
        children: normalizeSections(section.children ?? [], path)
      }
    })
  return {
    notebooks: (input.notebooks ?? []).map((notebook) => ({
      name: String(notebook.name ?? ''),
      sections: normalizeSections(notebook.sections),
      source: notebook.source,
      root_path: notebook.root_path,
      disconnected: notebook.disconnected
    }))
  }
}

export function findSection(
  sections: NavSection[],
  path: string
): NavSection | undefined {
  for (const section of sections) {
    if (section.path === path) return section
    const child = findSection(section.children ?? [], path)
    if (child) return child
  }
}

export function sectionAncestors(path: string): string[] {
  const parts = path.split('/').filter(Boolean)
  return parts.map((_, index) => parts.slice(0, index + 1).join('/'))
}

export function pageExists(
  tree: NavigationTree,
  ref: NavigationPageRef
): boolean {
  const notebook = tree.notebooks.find((item) => item.name === ref.notebook)
  if (!notebook) return false
  const section =
    ref.section === ''
      ? notebook.sections.find((item) => item.path === '')
      : findSection(notebook.sections, ref.section)
  return !!section?.pages.some((page) => page.name === ref.page)
}

export function visibleTreeNodes(
  notebook: NavNotebook | undefined,
  expanded: ReadonlySet<string>
): VisibleTreeNode[] {
  if (!notebook) return []
  const nodes: VisibleTreeNode[] = []
  const visit = (section: NavSection, parentId: string | null) => {
    if (!section.path) return
    const id = sectionNodeId(notebook.name, section.path)
    nodes.push({
      id,
      kind: 'section',
      notebook: notebook.name,
      section: section.path,
      label: section.name,
      parentId,
      expandable: true
    })
    if (!expanded.has(section.path)) return
    for (const page of section.pages) {
      const ref = {
        notebook: notebook.name,
        section: section.path,
        page: page.name
      }
      nodes.push({
        id: pageNodeId(ref),
        kind: 'page',
        ...ref,
        label: page.name,
        parentId: id,
        expandable: false
      })
    }
    for (const child of section.children ?? []) visit(child, id)
  }
  for (const section of notebook.sections) {
    if (section.path) visit(section, null)
  }
  const root = notebook.sections.find((section) => section.path === '')
  for (const page of root?.pages ?? []) {
    const ref = { notebook: notebook.name, section: '', page: page.name }
    nodes.push({
      id: pageNodeId(ref),
      kind: 'page',
      ...ref,
      label: page.name,
      parentId: null,
      expandable: false
    })
  }
  return nodes
}

/**
 * The active-navigation triple tracked by Sidebar.svelte. Reconciling decides
 * what these should become after a `ListNavigation` refresh — typically when
 * the user (or an external editor) has renamed, moved, or deleted the active
 * item.
 */
export interface ActiveNav {
  notebook: string
  section: string
  page: string
}

/**
 * Decide what the active navigation should become given the freshly-loaded
 * tree and the previously-active triple.
 *
 * Rules (mirrors the inline logic that used to live in Sidebar's `loadNavigation`):
 *  - Empty tree → leave current untouched (Sidebar keeps stale state until
 *    the next refresh; the IPC layer will eventually surface the empty tree).
 *  - Active notebook missing from the tree → fall back to the first notebook.
 *  - Active section missing from the (possibly new) active notebook → clear it.
 *  - Page is NOT reconciled here; page validity is checked at open time. This
 *    preserves the existing behaviour: a stale page string is harmless until
 *    the user navigates away from it.
 *
 * Pure: returns a new object, never mutates `current`.
 */
export function reconcileActive(
  tree: NavigationTree,
  current: ActiveNav
): ActiveNav {
  if (tree.notebooks.length === 0) {
    return { ...current }
  }

  // Pick a sensible active notebook if none selected or the current one is gone.
  let notebook = current.notebook
  if (!notebook || !tree.notebooks.some((nb) => nb.name === notebook)) {
    notebook = tree.notebooks[0].name
  }

  const nb = tree.notebooks.find((n) => n.name === notebook)
  if (!nb) {
    // The first-notebook fallback above guarantees `nb` is found; this branch
    // is defensive in case the tree mutates between the two lookups.
    return { notebook, section: '', page: '' }
  }

  let section = current.section
  if (section && !findSection(nb.sections, section)) {
    section = ''
  }

  return { notebook, section, page: current.page }
}

/**
 * Produce "Untitled", "Untitled 2", "Untitled 3", … skipping any name that
 * already exists in the given section. Used by inline page-create so the user
 * can hit "+" repeatedly without typing.
 */
export function generateUniquePageName(
  tree: NavigationTree,
  activeNotebook: string,
  sectionName: string
): string {
  const base = 'Untitled'
  const nb = tree.notebooks.find((n) => n.name === activeNotebook)
  if (!nb) return base
  const sec =
    sectionName === ''
      ? nb.sections.find((s) => s.path === '')
      : findSection(nb.sections, sectionName)
  if (!sec) return base
  const existing = new Set(sec.pages.map((p) => p.name))
  if (!existing.has(base)) return base
  let i = 2
  while (existing.has(`${base} ${i}`)) i++
  return `${base} ${i}`
}
