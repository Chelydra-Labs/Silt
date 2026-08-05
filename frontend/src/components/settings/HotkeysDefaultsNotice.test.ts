import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import HotkeysDefaultsNotice from './HotkeysDefaultsNotice.svelte'

describe('HotkeysDefaultsNotice (#868)', () => {
  it('is hidden when dismissed', () => {
    const { queryByTestId } = render(HotkeysDefaultsNotice, {
      props: { dismissed: true, onDismiss: () => {} }
    })
    expect(queryByTestId('hotkeys-defaults-notice')).toBeNull()
  })

  it('renders the migration notice when not dismissed', () => {
    const { getByTestId, getByRole } = render(HotkeysDefaultsNotice, {
      props: { dismissed: false, onDismiss: () => {} }
    })
    const banner = getByTestId('hotkeys-defaults-notice')
    // Surfaces the load-bearing chord relocations so a user knows what moved.
    expect(banner.textContent).toContain('Ctrl+B')
    expect(banner.textContent).toContain('Ctrl+\\')
    expect(banner.textContent).toContain('Ctrl+Shift+W')
    expect(banner.textContent).toContain('Ctrl+Alt+R')
    // Acknowledge button is keyboard-reachable.
    expect(getByRole('button', { name: 'Dismiss notice' })).toBeTruthy()
  })

  it('calls onDismiss when the Got it button is clicked', async () => {
    const onDismiss = vi.fn()
    const { getByRole } = render(HotkeysDefaultsNotice, {
      props: { dismissed: false, onDismiss }
    })
    await fireEvent.click(getByRole('button', { name: 'Dismiss notice' }))
    expect(onDismiss).toHaveBeenCalledOnce()
  })
})
