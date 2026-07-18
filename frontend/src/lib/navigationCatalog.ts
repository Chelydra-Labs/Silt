import type {
  NavNotebook,
  NavSection,
  NavigationPageRef,
  NavigationTree,
  RecentPageRef
} from './sidebar/types'
import { locatorKey } from './sidebar/navigationPreferences'

export interface NavigationCatalogItem extends NavigationPageRef {
  key: string
  label: string
  pathLabel: string
  source: string
  linked: boolean
  disconnected: boolean
  order: number
}

export interface NotebookNavigationMetadata {
  linked: boolean
  disconnected: boolean
  source: string
}

export function notebookNavigationMetadata(
  tree: NavigationTree
): Record<string, NotebookNavigationMetadata> {
  return Object.fromEntries(
    tree.notebooks.map((notebook) => [
      notebook.name,
      {
        linked: !!notebook.source && notebook.source !== 'vault',
        disconnected: !!notebook.disconnected,
        source: notebook.source ?? 'vault'
      }
    ])
  )
}

export function flattenNavigation(
  tree: NavigationTree
): NavigationCatalogItem[] {
  const result: NavigationCatalogItem[] = []
  const addSection = (notebook: NavNotebook, section: NavSection) => {
    for (const page of section.pages) {
      const ref = {
        notebook: notebook.name,
        section: section.path,
        page: page.name
      }
      result.push({
        ...ref,
        key: locatorKey(ref),
        label: page.name,
        pathLabel: [notebook.name, section.path, page.name]
          .filter(Boolean)
          .join(' / '),
        source: notebook.source ?? 'vault',
        linked: !!notebook.source && notebook.source !== 'vault',
        disconnected: !!notebook.disconnected,
        order: result.length
      })
    }
    for (const child of section.children ?? []) addSection(notebook, child)
  }
  for (const notebook of tree.notebooks) {
    for (const section of notebook.sections) addSection(notebook, section)
  }
  return result
}

export function normalizeSearch(value: string): string {
  return value
    .normalize('NFKD')
    .replace(/\p{M}/gu, '')
    .toLocaleLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, ' ')
    .trim()
}

export function fuzzyScore(query: string, candidate: string): number | null {
  const q = normalizeSearch(query)
  const value = normalizeSearch(candidate)
  if (!q) return 0
  if (value === q) return 0
  if (value.startsWith(q)) return 100 + (value.length - q.length)
  const wordIndex = value.split(' ').findIndex((word) => word.startsWith(q))
  if (wordIndex >= 0) return 200 + wordIndex
  const substring = value.indexOf(q)
  if (substring >= 0) return 300 + substring
  let queryIndex = 0
  let first = -1
  let last = -1
  for (let index = 0; index < value.length && queryIndex < q.length; index++) {
    if (value[index] === q[queryIndex]) {
      if (first < 0) first = index
      last = index
      queryIndex++
    }
  }
  if (queryIndex !== q.length) return null
  return 400 + (last - first - q.length) + first
}

export function rankNavigation(
  catalog: readonly NavigationCatalogItem[],
  query: string,
  recents: readonly RecentPageRef[] = [],
  limit = 40
): NavigationCatalogItem[] {
  const recentOrder = new Map(
    recents.map((recent, index) => [locatorKey(recent), index])
  )
  return catalog
    .map((item) => {
      const scores = [
        fuzzyScore(query, item.page),
        fuzzyScore(query, item.section),
        fuzzyScore(query, item.notebook),
        fuzzyScore(query, `${item.page} ${item.section} ${item.notebook}`)
      ].filter((score): score is number => score !== null)
      const score = scores.length ? Math.min(...scores) : null
      return { item, score, recent: recentOrder.get(item.key) ?? Infinity }
    })
    .filter((entry) => entry.score !== null)
    .sort(
      (a, b) =>
        a.recent - b.recent ||
        (a.score ?? 0) - (b.score ?? 0) ||
        a.item.order - b.item.order
    )
    .slice(0, limit)
    .map((entry) => entry.item)
}
