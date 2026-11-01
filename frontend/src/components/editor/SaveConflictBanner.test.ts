// SaveConflictBanner (#1005): the non-blocking resolution surface shown when a
// whole-page save is refused because the file changed on disk. Contract:
// announces assertively, offers exactly the three keep-my/theirs/both actions
// (keyboard operable buttons), exposes each action's disambiguating hint via
// aria-describedby, shows busy/failure states without dropping the actions.

import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    ResolveExternalConflict: vi.fn().mockResolvedValue('')
  })
)

import SaveConflictBanner from './SaveConflictBanner.svelte'
import { CONFLICT_RESOLUTIONS } from '../../lib/editor/externalConflict'

const LABELS = CONFLICT_RESOLUTIONS.map((r) => r.label)

describe('SaveConflictBanner', () => {
  it('announces the conflict assertively with honest keep-both copy', () => {
    render(SaveConflictBanner, { onResolve: () => {} })
    const alert = screen.getByRole('alert')
    // The template wraps the sentence, so compare whitespace-normalized.
    const text = alert.textContent?.replace(/\s+/g, ' ')
    expect(text).toContain('changed by another app')
    expect(text).toContain('save was held')
    // The banner must not promise a singular "other is discarded" framing:
    // every resolution preserves the losing version as a separate page.
    expect(text).toContain('separate page in the same folder')
  })

  it('offers the three resolutions and reports the chosen one', async () => {
    const onResolve = vi.fn()
    render(SaveConflictBanner, { onResolve })
    for (const label of LABELS) {
      expect(screen.getByRole('button', { name: label })).toBeTruthy()
    }
    await fireEvent.click(
      screen.getByRole('button', { name: 'Keep my changes' })
    )
    expect(onResolve).toHaveBeenCalledExactlyOnceWith('keep_mine')

    await fireEvent.click(
      screen.getByRole('button', { name: 'Keep the other version' })
    )
    expect(onResolve).toHaveBeenCalledWith('keep_theirs')

    await fireEvent.click(
      screen.getByRole('button', { name: 'Keep both as pages' })
    )
    expect(onResolve).toHaveBeenCalledWith('keep_both')
  })

  it('exposes each action hint via aria-describedby, not title alone', () => {
    render(SaveConflictBanner, { onResolve: () => {} })
    for (const action of CONFLICT_RESOLUTIONS) {
      const btn = screen.getByRole('button', { name: action.label })
      const describedBy = btn.getAttribute('aria-describedby')
      expect(describedBy).toContain(`conflict-hint-${action.key}`)
      const hint = document.getElementById(`conflict-hint-${action.key}`)
      expect(hint?.textContent).toContain(action.hint)
    }
  })

  it('disables the actions and shows Resolving… while in flight', () => {
    render(SaveConflictBanner, { busy: true, onResolve: () => {} })
    for (const label of LABELS) {
      expect(
        (screen.getByRole('button', { name: label }) as HTMLButtonElement)
          .disabled
      ).toBe(true)
    }
    expect(screen.getByText('Resolving…')).toBeTruthy()
  })

  it('surfaces a failed resolution and keeps the actions offered', () => {
    render(SaveConflictBanner, {
      error: 'vault closed',
      onResolve: () => {}
    })
    expect(screen.getByRole('alert').textContent).toContain(
      "Couldn't resolve the conflict"
    )
    expect(
      screen.getByRole('button', { name: 'Keep my changes' })
    ).toBeTruthy()
  })
})
