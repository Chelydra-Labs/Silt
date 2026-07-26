// Component-level coverage for the shared ToggleSwitch primitive.
//
// ToggleSwitch is now the single source of truth for 11 toggle sites across
// Settings → AI and the AI plugins, and it normalized a latent dark-mode
// regression (the old AIProviderTab knob hardcoded #ffffff). These tests lock
// the a11y + visual contract so a future caller cannot silently re-diverge it.
//
// The load-bearing assertion is the trackOn decoupling: the keyring-availability
// site renders checked=true with trackOn=false so the knob reads "off" while the
// checkbox stays selected. Without trackOn that decoupling collapses.

import { describe, it, expect } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import ToggleSwitch from './ToggleSwitch.svelte'

// The visible track is aria-hidden (pure decoration over the native checkbox),
// so it has no role — query it through the container.
function track(container: HTMLElement): HTMLElement {
  const el = container.querySelector<HTMLElement>('.toggle-switch-track')
  if (!el) throw new Error('toggle-switch-track not rendered')
  return el
}
function checkbox(container: HTMLElement): HTMLInputElement {
  const el = container.querySelector<HTMLInputElement>('input.toggle-switch')
  if (!el) throw new Error('toggle-switch checkbox not rendered')
  return el
}

describe('ToggleSwitch', () => {
  it('reflects the checked state on the track via the .on class', () => {
    const on = render(ToggleSwitch, { props: { checked: true } })
    expect(track(on.container).classList.contains('on')).toBe(true)
    const off = render(ToggleSwitch, { props: { checked: false } })
    expect(track(off.container).classList.contains('on')).toBe(false)
  })

  it('disabled dims the track and disables the native checkbox', () => {
    const { container } = render(ToggleSwitch, {
      props: { checked: true, disabled: true }
    })
    expect(track(container).classList.contains('disabled')).toBe(true)
    expect(checkbox(container).disabled).toBe(true)
  })

  // trackOn decouples the track's "on" look from `checked`. The
  // keyring-availability site (keyring selected but unavailable) passes
  // checked=true + trackOn=false so the knob renders "off" — collapsing
  // trackOn back onto checked would silently re-introduce the divergence.
  it('trackOn overrides checked for the track appearance', () => {
    const offDespiteChecked = render(ToggleSwitch, {
      props: { checked: true, trackOn: false }
    })
    expect(track(offDespiteChecked.container).classList.contains('on')).toBe(
      false
    )

    const onDespiteUnchecked = render(ToggleSwitch, {
      props: { checked: false, trackOn: true }
    })
    expect(track(onDespiteUnchecked.container).classList.contains('on')).toBe(
      true
    )
  })

  it('forwards aria-labelledby to the underlying input via restProps', () => {
    const { container } = render(ToggleSwitch, {
      props: { checked: false, 'aria-labelledby': 'lbl-x' }
    })
    expect(checkbox(container).getAttribute('aria-labelledby')).toBe('lbl-x')
  })

  it('toggling the checkbox flips the track to on', async () => {
    const { container } = render(ToggleSwitch, {
      props: { checked: false }
    })
    expect(track(container).classList.contains('on')).toBe(false)
    await fireEvent.click(checkbox(container))
    expect(track(container).classList.contains('on')).toBe(true)
  })

  it('the visible track is aria-hidden (the native checkbox carries semantics)', () => {
    const { container } = render(ToggleSwitch, { props: { checked: false } })
    expect(track(container).getAttribute('aria-hidden')).toBe('true')
  })
})
