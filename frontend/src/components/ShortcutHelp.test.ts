import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'

const mockSettings = vi.hoisted(() => ({
  config: {
    hotkeys: {
      new_page: 'Alt+N',
      new_section: '',
      open_search: 'Ctrl+Shift+F',
      close_tab: 'Ctrl+Shift+W',
      // The legend derives these from the resolved config (#863): the footer
      // must mirror whatever the table shows rather than hardcoding Windows
      // chords. Use the Windows defaults here so the legend's Ctrl+Alt+Arrow
      // caveat surfaces; the next test pins the Linux-style Ctrl+Tab path.
      next_tab: 'Ctrl+Alt+Right',
      prev_tab: 'Ctrl+Alt+Left'
    } as Record<string, string>
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

  it('derives the tab-chord legend from the resolved config (Windows form)', () => {
    // When next_tab resolves to the Ctrl+Alt+Arrow form, the legend shows
    // those chords verbatim AND surfaces the WebView2/Ctrl+Tab caveat.
    render(ShortcutHelp, { props: { onClose: vi.fn() } })
    const footer = document.querySelector('footer')!
    // Chord tokens render as separate <kbd> elements; assert the footer text
    // contains the resolved chord tokens and the caveat.
    expect(footer.textContent).toContain('Alt')
    expect(footer.textContent).toContain('Right')
    expect(footer.textContent).toContain('Left')
    expect(footer.textContent).toContain('Shift')
    expect(footer.textContent).toContain('W')
    // Caveat appears because next_tab matched the Ctrl+Alt+Arrow form.
    expect(footer.textContent).toContain('Ctrl')
    expect(footer.textContent).toContain('Tab')
    expect(footer.textContent).toContain('cannot reliably relay')
  })

  it('omits the WebView2 caveat when next_tab uses Ctrl+Tab (non-Windows form)', () => {
    // Simulate the Linux/macOS default: next_tab=Ctrl+Tab works there, so the
    // legend must NOT claim "the webview cannot reliably relay Ctrl+Tab".
    mockSettings.config.hotkeys = {
      ...mockSettings.config.hotkeys,
      next_tab: 'Ctrl+Tab',
      prev_tab: 'Ctrl+Shift+Tab'
    }
    render(ShortcutHelp, { props: { onClose: vi.fn() } })
    const footer = document.querySelector('footer')!
    expect(footer.textContent).toContain('Tab')
    expect(footer.textContent).not.toContain('cannot reliably relay')
  })

  it('does not badge the platform-conditional tab chords as Remapped on non-Windows defaults', () => {
    // next_tab/prev_tab have NO static frontend default (their chord is
    // platform-conditional), so ShortcutHelp must not show "Remapped" when the
    // backend resolves them to the Linux/macOS Ctrl+Tab form. Previously the
    // hardcoded Windows defaultBinding (Ctrl+Alt+Arrow) mismatched the
    // resolved Ctrl+Tab on every non-Windows vault, false-firing the badge.
    mockSettings.config.hotkeys = {
      next_tab: 'Ctrl+Tab',
      prev_tab: 'Ctrl+Shift+Tab',
      close_tab: 'Ctrl+Shift+W'
    }
    render(ShortcutHelp, { props: { onClose: vi.fn() } })
    // The Tabs group renders with real chords but no value differs from a
    // static default (there is none for the cycle chords), so no badge.
    expect(screen.queryByText('Remapped')).not.toBeInTheDocument()
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
