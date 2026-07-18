import {
  ClearNavNotebookOrder,
  ClearNavPageOrder,
  ClearNavSectionOrder,
  GetNavOrder,
  SetNavNotebookOrder,
  SetNavPageOrder,
  SetNavSectionOrder
} from '../../../bindings/silt/app.js'

export interface NavOrderState {
  notebooks: string[]
  sections: Record<string, string[]>
  pages: Record<string, string[]>
}

/**
 * Sort items by a custom order, falling back to alphabetical. Pure function.
 */
export function sortByName<T extends { name: string }>(
  items: T[],
  order: string[] | undefined,
  identity: (item: T) => string = (item) => item.name
): T[] {
  if (!order || order.length === 0) {
    return [...items].sort((a, b) => a.name.localeCompare(b.name))
  }
  const orderMap = new Map(order.map((n, i) => [n, i]))
  return [...items].sort((a, b) => {
    const ai = orderMap.get(identity(a)) ?? Infinity
    const bi = orderMap.get(identity(b)) ?? Infinity
    if (ai !== bi) return ai - bi
    return a.name.localeCompare(b.name)
  })
}

export interface NavOrderDeps {
  onStateChange: (state: NavOrderState) => void
}

/**
 * Manages nav-order persistence for the sidebar. Loads from and saves to
 * config.yaml via IPC.
 *
 * Usage:
 *   const navOrder = new NavOrderManager({ onStateChange: (s) => navOrderState = s })
 *   await navOrder.load()
 *   await navOrder.persistSectionOrder('Work', '', ['Journal', 'Projects'])
 */
export class NavOrderManager {
  private state: NavOrderState = {
    notebooks: [],
    sections: {},
    pages: {}
  }
  private deps: NavOrderDeps
  private loadGen = 0

  constructor(deps: NavOrderDeps) {
    this.deps = deps
  }

  get current(): NavOrderState {
    return this.state
  }

  /** Load nav order from config.yaml. No-op on failure (alphabetical fallback). */
  async load(): Promise<void> {
    const gen = ++this.loadGen
    try {
      const order = await GetNavOrder()
      if (gen !== this.loadGen) return
      this.state = {
        notebooks: order.notebooks ?? [],
        // Per-value `| undefined` is a Wails v3 binding artifact for Go
        // map[string][]string; the values are always present here.
        sections: Object.fromEntries(
          Object.entries(order.sections ?? {})
        ) as Record<string, string[]>,
        pages: Object.fromEntries(Object.entries(order.pages ?? {})) as Record<
          string,
          string[]
        >
      }
      this.deps.onStateChange(this.state)
    } catch {
      // Pre-vault or config not loaded — alphabetical fallback.
    }
  }

  async persistNotebookOrder(notebooks: string[]): Promise<void> {
    const previous = this.state.notebooks
    const attempted = [...notebooks]
    this.state = { ...this.state, notebooks: attempted }
    this.deps.onStateChange(this.state)
    try {
      if (attempted.length === 0) await ClearNavNotebookOrder()
      else await SetNavNotebookOrder(attempted)
    } catch (e) {
      console.error('SetNavNotebookOrder failed:', e)
      if (this.state.notebooks === attempted) {
        this.state = { ...this.state, notebooks: previous }
        this.deps.onStateChange(this.state)
      }
    }
  }

  /** Persist one sibling section order under its canonical parent. */
  async persistSectionOrder(
    notebook: string,
    parentPath: string,
    sections: string[]
  ): Promise<void> {
    const key = parentPath ? `${notebook}/${parentPath}` : notebook
    const previous = this.state.sections[key]
    const attempted = [...sections]
    const nextSections = { ...this.state.sections }
    if (attempted.length === 0) delete nextSections[key]
    else nextSections[key] = attempted
    this.state = {
      ...this.state,
      sections: nextSections
    }
    this.deps.onStateChange(this.state)
    try {
      if (attempted.length === 0) {
        await ClearNavSectionOrder(notebook, parentPath)
      } else {
        await SetNavSectionOrder(notebook, parentPath, attempted)
      }
    } catch (e) {
      console.error('SetNavSectionOrder failed:', e)
      if (
        this.state.sections[key] === attempted ||
        (attempted.length === 0 && this.state.sections[key] === undefined)
      ) {
        const restored = { ...this.state.sections }
        if (previous === undefined) delete restored[key]
        else restored[key] = previous
        this.state = { ...this.state, sections: restored }
        this.deps.onStateChange(this.state)
      }
    }
  }

  /** Persist one page order in a canonical section (empty means root). */
  async persistPageOrder(
    notebook: string,
    sectionPath: string,
    pages: string[]
  ): Promise<void> {
    const key = `${notebook}/${sectionPath}`
    const previous = this.state.pages[key]
    const attempted = [...pages]
    const nextPages = { ...this.state.pages }
    if (attempted.length === 0) delete nextPages[key]
    else nextPages[key] = attempted
    this.state = {
      ...this.state,
      pages: nextPages
    }
    this.deps.onStateChange(this.state)
    try {
      if (attempted.length === 0) {
        await ClearNavPageOrder(notebook, sectionPath)
      } else {
        await SetNavPageOrder(notebook, sectionPath, attempted)
      }
    } catch (e) {
      console.error('SetNavPageOrder failed:', e)
      if (
        this.state.pages[key] === attempted ||
        (attempted.length === 0 && this.state.pages[key] === undefined)
      ) {
        const restored = { ...this.state.pages }
        if (previous === undefined) delete restored[key]
        else restored[key] = previous
        this.state = { ...this.state, pages: restored }
        this.deps.onStateChange(this.state)
      }
    }
  }
}
