import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import QuickSwitcher from './QuickSwitcher.svelte'
import type { NavigationCatalogItem } from '../lib/navigationCatalog'

const catalog: NavigationCatalogItem[] = [
  {
    notebook: 'Work',
    section: '',
    page: 'Inbox',
    key: 'inbox',
    label: 'Inbox',
    pathLabel: 'Work / Inbox',
    source: 'vault',
    linked: false,
    disconnected: false,
    order: 0
  },
  {
    notebook: 'Work',
    section: 'Projects',
    page: 'Roadmap',
    key: 'roadmap',
    label: 'Roadmap',
    pathLabel: 'Work / Projects / Roadmap',
    source: 'vault',
    linked: false,
    disconnected: false,
    order: 1
  }
]

afterEach(cleanup)

describe('QuickSwitcher', () => {
  it('keeps focus in the combobox and opens active options in preview or pin mode', async () => {
    const onOpen = vi.fn()
    render(QuickSwitcher, {
      props: { catalog, onRetry: vi.fn(), onOpen, onClose: vi.fn() }
    })
    const input = screen.getByRole('combobox')
    expect(document.activeElement).toBe(input)
    expect(input).toHaveAttribute(
      'aria-activedescendant',
      'quick-switcher-option-0'
    )
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(input).toHaveAttribute(
      'aria-activedescendant',
      'quick-switcher-option-1'
    )
    await fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true })
    expect(onOpen).toHaveBeenCalledWith(catalog[1], 'pin')
  })

  it('filters, exposes no-results, and supports pointer activation', async () => {
    const onOpen = vi.fn()
    render(QuickSwitcher, {
      props: { catalog, onRetry: vi.fn(), onOpen, onClose: vi.fn() }
    })
    const input = screen.getByRole('combobox')
    await fireEvent.input(input, { target: { value: 'road' } })
    await fireEvent.click(screen.getByRole('option', { name: /Roadmap/ }))
    expect(onOpen).toHaveBeenCalledWith(catalog[1], 'preview')
    await fireEvent.input(input, { target: { value: 'not-present' } })
    expect(screen.getByText(/No pages match/)).toBeInTheDocument()
  })

  it('shows load errors with retry and restores focus on Escape', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()
    const onClose = vi.fn()
    const onRetry = vi.fn()
    const view = render(QuickSwitcher, {
      props: { catalog: [], error: 'failed', onRetry, onOpen: vi.fn(), onClose }
    })
    await fireEvent.keyDown(screen.getByRole('combobox'), { key: 'Tab' })
    expect(document.activeElement).toBe(
      screen.getByRole('button', { name: 'Try again' })
    )
    await fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(onRetry).toHaveBeenCalledOnce()
    await fireEvent.keyDown(screen.getByRole('combobox'), { key: 'Escape' })
    expect(onClose).toHaveBeenCalledOnce()
    view.unmount()
    expect(document.activeElement).toBe(trigger)
    trigger.remove()
  })

  it('keeps linked offline pages visible but unavailable', () => {
    render(QuickSwitcher, {
      props: {
        catalog: [{ ...catalog[0], linked: true, disconnected: true }],
        onRetry: vi.fn(),
        onOpen: vi.fn(),
        onClose: vi.fn()
      }
    })
    expect(screen.getByRole('option', { name: /Inbox/ })).toBeDisabled()
    expect(screen.getByText('Offline')).toBeInTheDocument()
  })
})
