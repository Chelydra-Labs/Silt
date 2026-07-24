import { describe, expect, it } from 'vitest'
import {
  expandActiveAncestors,
  expandedPathsForNotebook,
  locatorKey,
  reconcilePageRefs
} from './navigationPreferences'
import type { NavigationPreferences, NavigationTree } from './types'

const preferences: NavigationPreferences = {
  expanded_sections: [
    { notebook: 'Work', path: 'Projects' },
    { notebook: 'Personal', path: 'Home' }
  ],
  recent_pages: [],
  favorites: [],
  sidebar_view: 'tree'
}

const tree: NavigationTree = {
  notebooks: [
    {
      name: 'Work',
      sections: [
        {
          name: 'Projects',
          path: 'Projects',
          pages: [],
          children: [
            {
              name: 'Active',
              path: 'Projects/Active',
              pages: [{ name: 'Roadmap', count: 1 }]
            }
          ]
        },
        { name: '', path: '', pages: [{ name: 'Inbox', count: 1 }] }
      ]
    }
  ]
}

describe('navigation preferences helpers', () => {
  it('scopes expansion by notebook and adds every active ancestor', () => {
    const current = expandedPathsForNotebook(preferences, 'Work')
    expect([...current]).toEqual(['Projects'])
    expect([
      ...expandActiveAncestors(current, 'Projects/Active/Launch')
    ]).toEqual(['Projects', 'Projects/Active', 'Projects/Active/Launch'])
  })

  it('uses all locator fields, including the section-less root', () => {
    expect(
      locatorKey({ notebook: 'Work', section: '', page: 'Inbox' })
    ).not.toBe(
      locatorKey({ notebook: 'Work', section: 'Archive', page: 'Inbox' })
    )
  })

  it('reconciles nested and section-less pages without confusing siblings', () => {
    const refs = [
      { notebook: 'Work', section: 'Projects/Active', page: 'Roadmap' },
      { notebook: 'Work', section: '', page: 'Inbox' },
      { notebook: 'Work', section: 'Archive/Active', page: 'Roadmap' }
    ]
    const result = reconcilePageRefs(tree, refs)
    expect(result.available).toEqual(refs.slice(0, 2))
    expect(result.stale).toEqual([refs[2]])
  })
})
