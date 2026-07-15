import { describe, expect, it, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import InfoTooltip from './InfoTooltip.svelte'

afterEach(() => cleanup())

describe('InfoTooltip', () => {
  it('renders an accessible icon button', () => {
    render(InfoTooltip, {
      props: {
        text: 'Plain explanation',
        technical: 'Technical: Foo (0-1).',
        label: 'What is Foo?'
      }
    })
    expect(
      screen.getByRole('button', { name: 'What is Foo?' })
    ).toBeInTheDocument()
  })

  it('opens on focus and shows plain + technical content', async () => {
    render(InfoTooltip, {
      props: {
        text: 'Plain explanation',
        technical: 'Technical: Foo (0-1).',
        label: 'What is Foo?'
      }
    })
    const btn = screen.getByRole('button', { name: 'What is Foo?' })
    await fireEvent.focus(btn)
    const tip = screen.getByRole('tooltip')
    expect(tip).toHaveTextContent('Plain explanation')
    expect(tip).toHaveTextContent('Technical: Foo (0-1).')
    expect(btn).toHaveAttribute('aria-describedby')
  })

  it('closes on Escape', async () => {
    render(InfoTooltip, {
      props: { text: 'Hello', label: 'What is Hello?' }
    })
    const btn = screen.getByRole('button', { name: 'What is Hello?' })
    await fireEvent.focus(btn)
    expect(screen.getByRole('tooltip')).toBeInTheDocument()
    await fireEvent.keyDown(btn, { key: 'Escape' })
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })

  it('toggles on click and dismisses on outside click', async () => {
    render(InfoTooltip, {
      props: { text: 'Tap me', label: 'What is Tap?' }
    })
    const btn = screen.getByRole('button', { name: 'What is Tap?' })
    await fireEvent.click(btn)
    expect(screen.getByRole('tooltip')).toBeInTheDocument()
    await fireEvent.pointerDown(document.body)
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })

  it('only one tooltip open at a time', async () => {
    const { container } = render(InfoTooltip, {
      props: { text: 'First', label: 'What is First?' }
    })
    // Mount a second instance in the same document.
    const host = document.createElement('div')
    document.body.appendChild(host)
    const second = render(InfoTooltip, {
      target: host,
      props: { text: 'Second', label: 'What is Second?' }
    })

    const firstBtn = screen.getByRole('button', { name: 'What is First?' })
    const secondBtn = screen.getByRole('button', { name: 'What is Second?' })
    await fireEvent.focus(firstBtn)
    expect(screen.getByRole('tooltip')).toHaveTextContent('First')
    await fireEvent.focus(secondBtn)
    const tips = screen.getAllByRole('tooltip')
    expect(tips).toHaveLength(1)
    expect(tips[0]).toHaveTextContent('Second')

    second.unmount()
    host.remove()
    void container
  })

  it('stays open on mouseleave after click-pinning', async () => {
    render(InfoTooltip, {
      props: { text: 'Pinned', label: 'What is Pinned?' }
    })
    const btn = screen.getByRole('button', { name: 'What is Pinned?' })
    // Click opens and pins.
    await fireEvent.click(btn)
    expect(screen.getByRole('tooltip')).toBeInTheDocument()
    // Mouse leaving should NOT close a click-pinned tooltip.
    await fireEvent.mouseLeave(btn)
    expect(screen.getByRole('tooltip')).toBeInTheDocument()
    // A second click closes it.
    await fireEvent.click(btn)
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })
})
