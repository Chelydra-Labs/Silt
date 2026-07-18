import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import PageBreadcrumb from './PageBreadcrumb.svelte'
import {
  flattenNavigation,
  notebookNavigationMetadata
} from '../lib/navigationCatalog'
import type { NavigationTree } from '../lib/sidebar/types'

const mocks = vi.hoisted(() => ({ resolvePageLink: vi.fn() }))
vi.mock('../../bindings/silt/app.js', () => ({
  ResolvePageLink: mocks.resolvePageLink
}))

const base = {
  notebook: 'Work',
  section: 'Projects/Active',
  page: 'Plan',
  activeView: 'notes',
  onSelectNotebook: vi.fn(),
  onSelectSection: vi.fn(),
  onOpenPage: vi.fn()
}

afterEach(cleanup)

describe('PageBreadcrumb', () => {
  it('renders full clickable context with a shortest-reference label', async () => {
    mocks.resolvePageLink.mockResolvedValue({
      exists: true,
      shortest: 'Active/Plan'
    })
    render(PageBreadcrumb, { props: base })
    await fireEvent.click(screen.getByRole('button', { name: 'Work' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Active' }))
    await fireEvent.click(
      screen.getByRole('button', { name: /Work \/ Projects\/Active \/ Plan/ })
    )
    expect(base.onSelectNotebook).toHaveBeenCalledWith('Work')
    expect(base.onSelectSection).toHaveBeenCalledWith('Projects/Active')
    expect(base.onOpenPage).toHaveBeenCalledOnce()
    expect(await screen.findByTitle(/\[\[Active\/Plan\]\]/)).toBeInTheDocument()
  })

  it('labels linked offline context and disables page activation', () => {
    render(PageBreadcrumb, {
      props: { ...base, linked: true, disconnected: true }
    })
    expect(screen.getByLabelText('Linked notebook offline')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Plan/ })).toBeDisabled()
  })

  it('preserves linked offline state when an empty active page vanishes from fallback rows', async () => {
    const online: NavigationTree = {
      notebooks: [
        {
          name: 'Cloud',
          source: 'linked:x',
          disconnected: false,
          sections: [
            {
              name: 'Notes',
              path: 'Notes',
              pages: [{ name: 'Empty Draft', count: 0 }]
            }
          ]
        }
      ]
    }
    const offline: NavigationTree = {
      notebooks: [
        {
          name: 'Cloud',
          source: 'linked:x',
          disconnected: true,
          sections: []
        }
      ]
    }
    const onlineMeta = notebookNavigationMetadata(online).Cloud
    const { rerender } = render(PageBreadcrumb, {
      props: {
        ...base,
        notebook: 'Cloud',
        section: 'Notes',
        page: 'Empty Draft',
        ...onlineMeta
      }
    })
    expect(screen.getByLabelText('Linked notebook')).toBeInTheDocument()

    const offlineMeta = notebookNavigationMetadata(offline).Cloud
    expect(flattenNavigation(offline)).toEqual([])
    await rerender({
      ...base,
      notebook: 'Cloud',
      section: 'Notes',
      page: 'Empty Draft',
      ...offlineMeta
    })

    expect(screen.getByLabelText('Linked notebook offline')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Empty Draft/ })).toBeDisabled()
  })

  it('renders nothing for blank pages or non-note views', async () => {
    const { container, rerender } = render(PageBreadcrumb, {
      props: { ...base, page: '' }
    })
    expect(container.querySelector('nav')).toBeNull()
    await rerender({ ...base, activeView: 'settings' })
    expect(container.querySelector('nav')).toBeNull()
  })
})
