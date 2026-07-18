import { describe, expect, it } from 'vitest'
import {
  flattenNavigation,
  fuzzyScore,
  normalizeSearch,
  rankNavigation
} from './navigationCatalog'
import type { NavigationTree } from './sidebar/types'

const tree: NavigationTree = {
  notebooks: [
    {
      name: 'Work',
      sections: [
        { name: '', path: '', pages: [{ name: 'Inbox', count: 1 }] },
        {
          name: 'Projects',
          path: 'Projects',
          pages: [],
          children: [
            {
              name: 'Active',
              path: 'Projects/Active',
              pages: [{ name: 'Café Launch', count: 2 }]
            }
          ]
        }
      ]
    },
    {
      name: 'Cloud',
      source: 'linked:x',
      disconnected: true,
      sections: [
        { name: 'Notes', path: 'Notes', pages: [{ name: 'Roadmap', count: 1 }] }
      ]
    }
  ]
}

describe('navigation catalog', () => {
  it('flattens nested, root, and linked pages with canonical source identity', () => {
    const catalog = flattenNavigation(tree)
    expect(
      catalog.map((item) => [item.notebook, item.section, item.page])
    ).toEqual([
      ['Work', '', 'Inbox'],
      ['Work', 'Projects/Active', 'Café Launch'],
      ['Cloud', 'Notes', 'Roadmap']
    ])
    expect(catalog[2]).toMatchObject({ linked: true, disconnected: true })
  })

  it('normalizes Unicode and diacritics deterministically', () => {
    expect(normalizeSearch('  CAFÉ—東京  ')).toBe('cafe 東京')
    expect(fuzzyScore('cafe', 'Café Launch')).toBeLessThan(200)
  })

  it('orders exact, prefix, word-prefix, substring, then subsequence matches', () => {
    expect(fuzzyScore('plan', 'plan')).toBe(0)
    expect(fuzzyScore('plan', 'planning')).toBeLessThan(200)
    expect(fuzzyScore('plan', 'release plan')).toBeLessThan(300)
    expect(fuzzyScore('plan', 'airplane')).toBeGreaterThanOrEqual(300)
    expect(fuzzyScore('plan', 'airplane')).toBeLessThan(400)
    expect(fuzzyScore('pln', 'planning')).toBeGreaterThanOrEqual(400)
    expect(fuzzyScore('zzz', 'planning')).toBeNull()
  })

  it('ranks recents first and alphabetizes equal-score non-recents', () => {
    const catalog = flattenNavigation(tree)
    const ranked = rankNavigation(catalog, '', [
      {
        notebook: 'Work',
        section: 'Projects/Active',
        page: 'Café Launch',
        opened_at: 2
      }
    ])
    expect(ranked[0].page).toBe('Café Launch')
    expect(ranked.slice(1).map((item) => item.page)).toEqual([
      'Inbox',
      'Roadmap'
    ])
  })

  it('uses normalized alphabetical order for an empty query', () => {
    const ranked = rankNavigation(flattenNavigation(tree), '')
    expect(ranked.map((item) => item.page)).toEqual([
      'Café Launch',
      'Inbox',
      'Roadmap'
    ])
  })

  it('uses the normalized path as a deterministic fallback for equal labels', () => {
    const duplicateTree: NavigationTree = {
      notebooks: [
        {
          name: 'Zulu',
          sections: [
            {
              name: 'Notes',
              path: 'Notes',
              pages: [{ name: 'Same', count: 1 }]
            }
          ]
        },
        {
          name: 'Álpha',
          sections: [
            {
              name: 'Notes',
              path: 'Notes',
              pages: [{ name: 'Same', count: 1 }]
            }
          ]
        }
      ]
    }

    expect(
      rankNavigation(flattenNavigation(duplicateTree), '').map(
        (item) => item.notebook
      )
    ).toEqual(['Álpha', 'Zulu'])
  })
})
