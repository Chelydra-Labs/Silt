// Regression coverage for the TitleBar wordmark token (#138).
//
// The "Silt" wordmark used the accent token, which masked theme switches on
// the three cool-accent themes (Cyber Forest teal / Graphite blue / Linen
// slate-blue all read similarly). It now follows --color-text-primary so each
// theme's body-text hue is visible in the titlebar chrome. This test pins
// the class so the accent-token relapse is caught.

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup } from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  WindowMinimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowIsMaximised: vi.fn().mockResolvedValue(false),
  Quit: vi.fn()
}))

// Mirror the exact specifier TitleBar.svelte imports (same directory, so the
// relative path resolves to the same absolute module the component sees).
vi.mock('@wailsio/runtime', () => ({
  Window: {
    Minimise: mocks.WindowMinimise,
    ToggleMaximise: mocks.WindowToggleMaximise,
    IsMaximised: mocks.WindowIsMaximised
  },
  Application: {
    Quit: mocks.Quit
  },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {
    then() {
      return this
    }
    catch() {
      return this
    }
    finally() {
      return this
    }
  },
  Create: {
    Nullable: (fn: any) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

import TitleBar from './TitleBar.svelte'

describe('TitleBar', () => {
  beforeEach(() => {
    mocks.WindowMinimise.mockReset()
    mocks.WindowToggleMaximise.mockReset()
    mocks.WindowIsMaximised.mockReset()
    mocks.Quit.mockReset()
    mocks.WindowIsMaximised.mockResolvedValue(false)
  })

  afterEach(() => {
    cleanup()
  })

  it('renders the "Silt" wordmark in text-primary (not accent) per #138', async () => {
    render(TitleBar, {
      props: {
        sidebarCollapsed: false,
        onSearchClick: () => {}
      }
    })
    await tick()

    // getByText('Silt') matches the wordmark <span> only — the adjacent
    // <img alt="Silt"> exposes its name via the alt attribute, not text
    // content, so it is not matched.
    const wordmark = screen.getByText('Silt')
    expect(wordmark).toHaveClass('text-surface-titlebar-text')
    expect(wordmark).not.toHaveClass('text-accent-primary-start')
  })

  it('keeps standard search separate from one unified AI button', async () => {
    const onAI = vi.fn()
    render(TitleBar, {
      props: {
        sidebarCollapsed: false,
        onSearchClick: () => {},
        onAIClick: onAI
      }
    })
    await tick()

    expect(screen.getByRole('button', { name: 'Search' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Silt AI' })).toBeTruthy()
    expect(
      screen.queryByRole('button', { name: 'Writing Assistant' })
    ).toBeNull()
  })

  it('offers a separate discoverable page switcher control', async () => {
    const onSwitcherClick = vi.fn()
    render(TitleBar, {
      props: {
        sidebarCollapsed: false,
        onSearchClick: () => {},
        onSwitcherClick
      }
    })
    await screen.getByRole('button', { name: 'Switch page' }).click()
    expect(onSwitcherClick).toHaveBeenCalledOnce()
  })

  it('offers a discoverable shortcut reference trigger', async () => {
    const onShortcutHelpClick = vi.fn()
    render(TitleBar, {
      props: {
        sidebarCollapsed: false,
        onSearchClick: () => {},
        onShortcutHelpClick
      }
    })
    const trigger = screen.getByRole('button', { name: 'Keyboard shortcuts' })
    expect(trigger).toHaveAttribute('title', expect.stringContaining('Shift+?'))
    trigger.click()
    expect(onShortcutHelpClick).toHaveBeenCalledOnce()
  })
})
