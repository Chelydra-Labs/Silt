import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import HotkeysDefaultsNotice from './HotkeysDefaultsNotice.svelte'

describe('HotkeysDefaultsNotice (#868)', () => {
  it('is hidden when dismissed', () => {
    const { queryByTestId } = render(HotkeysDefaultsNotice, {
      props: {
        dismissed: true,
        onDismiss: () => {},
        hotkeys: {}
      }
    })
    expect(queryByTestId('hotkeys-defaults-notice')).toBeNull()
  })

  it('renders the migration notice when not dismissed', () => {
    const { getByTestId, getByRole } = render(HotkeysDefaultsNotice, {
      props: {
        dismissed: false,
        onDismiss: () => {},
        // Windows-form tab chords: derived text uses the resolved values.
        hotkeys: {
          next_tab: 'Ctrl+Alt+Right',
          prev_tab: 'Ctrl+Alt+Left',
          close_tab: 'Ctrl+Shift+W'
        }
      }
    })
    const banner = getByTestId('hotkeys-defaults-notice')
    // Surfaces the load-bearing chord relocations so a user knows what moved.
    expect(banner.textContent).toContain('Ctrl+B')
    expect(banner.textContent).toContain('Ctrl+\\')
    expect(banner.textContent).toContain('Ctrl+Shift+W')
    expect(banner.textContent).toContain('Ctrl+Alt+R')
    // The tab navigation pair is derived — Windows form shows Ctrl+Alt+Right
    // (next) and Ctrl+Alt+Left (prev), with close called out separately.
    expect(banner.textContent).toContain('Ctrl+Alt+Right')
    expect(banner.textContent).toContain('Ctrl+Alt+Left')
    // Acknowledge button is keyboard-reachable.
    expect(getByRole('button', { name: 'Dismiss notice' })).toBeTruthy()
  })

  it('derives the tab chord from the resolved hotkey map (Linux form)', () => {
    // On Linux/macOS the migration does NOT rewrite next_tab/prev_tab, so the
    // resolved chords stay at Ctrl+Tab / Ctrl+Shift+Tab. The notice must
    // mirror those rather than claim "moved to Ctrl+Alt+Arrow" — that would
    // contradict the table (#863).
    const { getByTestId } = render(HotkeysDefaultsNotice, {
      props: {
        dismissed: false,
        onDismiss: () => {},
        hotkeys: {
          next_tab: 'Ctrl+Tab',
          prev_tab: 'Ctrl+Shift+Tab',
          close_tab: 'Ctrl+Shift+W'
        }
      }
    })
    const banner = getByTestId('hotkeys-defaults-notice')
    expect(banner.textContent).toContain('Ctrl+Tab')
    expect(banner.textContent).toContain('Ctrl+Shift+Tab')
    expect(banner.textContent).not.toContain('Ctrl+Alt+Right')
  })

  it('calls onDismiss when the Got it button is clicked', async () => {
    const onDismiss = vi.fn()
    const { getByRole } = render(HotkeysDefaultsNotice, {
      props: { dismissed: false, onDismiss, hotkeys: {} }
    })
    await fireEvent.click(getByRole('button', { name: 'Dismiss notice' }))
    expect(onDismiss).toHaveBeenCalledOnce()
  })
})
