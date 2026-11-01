// SaveConflictBanner (#1005): the non-blocking resolution surface shown when a
// whole-page save is refused because the file changed on disk. Contract: names
// the file, offers exactly the three keep-mine/theirs/both actions (keyboard
// operable buttons), announces assertively, and shows resolution failures
// without dropping the actions.

import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    ResolveExternalConflict: vi.fn().mockResolvedValue(undefined)
  })
)

import SaveConflictBanner from './SaveConflictBanner.svelte'

describe('SaveConflictBanner', () => {
  it('announces the conflict and names the file', () => {
    render(SaveConflictBanner, {
      page: 'Meeting Notes',
      onResolve: () => {}
    })
    const alert = screen.getByRole('alert')
    expect(alert.textContent).toContain('Meeting Notes.md')
    expect(alert.textContent).toContain('changed on disk')
  })

  it('offers the three resolutions and reports the chosen one', async () => {
    const onResolve = vi.fn()
    render(SaveConflictBanner, { page: 'PG', onResolve })
    for (const label of ['Keep mine', 'Keep theirs', 'Keep both']) {
      expect(screen.getByRole('button', { name: label })).toBeTruthy()
    }
    await fireEvent.click(screen.getByRole('button', { name: 'Keep mine' }))
    expect(onResolve).toHaveBeenCalledExactlyOnceWith('keep_mine')

    await fireEvent.click(screen.getByRole('button', { name: 'Keep theirs' }))
    expect(onResolve).toHaveBeenCalledWith('keep_theirs')

    await fireEvent.click(screen.getByRole('button', { name: 'Keep both' }))
    expect(onResolve).toHaveBeenCalledWith('keep_both')
  })

  it('disables the actions while a resolution is in flight', () => {
    render(SaveConflictBanner, { page: 'PG', busy: true, onResolve: () => {} })
    for (const label of ['Keep mine', 'Keep theirs', 'Keep both']) {
      expect(
        (screen.getByRole('button', { name: label }) as HTMLButtonElement)
          .disabled
      ).toBe(true)
    }
  })

  it('surfaces a failed resolution and keeps the actions offered', () => {
    render(SaveConflictBanner, {
      page: 'PG',
      error: 'vault closed',
      onResolve: () => {}
    })
    expect(screen.getByRole('alert').textContent).toContain('vault closed')
    expect(screen.getByRole('button', { name: 'Keep mine' })).toBeTruthy()
  })
})
