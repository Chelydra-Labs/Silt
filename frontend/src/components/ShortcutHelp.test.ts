import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'

const mockSettings = vi.hoisted(() => ({
  config: {
    hotkeys: {
      new_page: 'Alt+N',
      new_section: '',
      open_search: 'Ctrl+Shift+F',
      close_tab: 'Ctrl+W'
    }
  }
}))
vi.mock('../settings/store.svelte', () => ({ settings: mockSettings }))

import ShortcutHelp from './ShortcutHelp.svelte'

afterEach(cleanup)

describe('ShortcutHelp', () => {
  it('shows live and remapped bindings, hides disabled ones', () => {
    render(ShortcutHelp, { props: { onClose: vi.fn() } })
    expect(
      screen.getByRole('heading', { name: 'Navigation' })
    ).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Search' })).toBeInTheDocument()
    expect(screen.getByText('Alt+N')).toBeInTheDocument()
    expect(screen.getByText('Remapped')).toBeInTheDocument()
    // new_section is bound to '' (disabled) — it must NOT appear (#731).
    expect(screen.queryByText('New section')).not.toBeInTheDocument()
    expect(
      screen.getByRole('region', { name: 'Shortcut list' })
    ).toHaveAttribute('tabindex', '0')
  })

  it('focuses close, closes on Escape, and restores trigger focus', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()
    const onClose = vi.fn()
    const view = render(ShortcutHelp, { props: { onClose } })
    const close = screen
      .getAllByRole('button', { name: 'Close keyboard shortcuts' })
      .at(-1)!
    expect(document.activeElement).toBe(close)
    await fireEvent.keyDown(close, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledOnce()
    view.unmount()
    expect(document.activeElement).toBe(trigger)
    trigger.remove()
  })
})
