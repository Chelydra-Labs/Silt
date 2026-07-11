// Component-level coverage for the theme picker (#50, #512).
// The two-stage-preview redesign (#512) replaces the hover-to-inject list
// with a card grid + details pane: hover only highlights (CSS), a click
// stages a temporary preview (injectTokens + Apply/Revert banner), Apply
// or a double-click commits (applyTheme), Revert/Esc restores the active
// theme. These tests pin that contract at the rendered-component level;
// the injector/store unit tests cover the data pipeline.

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  within
} from '@testing-library/svelte'
// jest-dom matchers are registered via vitest.setup.ts (the /vitest
// entry); no inline import needed here.

// Hoisted, mutable state objects + function mocks. vi.hoisted keeps
// these refs available inside the vi.mock factories (which are
// themselves hoisted above the imports). The objects are PLAIN (not
// $state) — sufficient for assertions against the initial render and
// the component's own $state-driven interactions (focusIndex, cardRefs,
// previewTheme).
const mocks = vi.hoisted(() => ({
  themeState: {
    id: 'cyber_forest',
    name: 'Cyber Forest',
    mode: 'dark' as 'dark' | 'light' | 'system',
    darkTokens: { '--color-surface-app': '#0c0c0e' } as Record<string, string>,
    lightTokens: { '--color-surface-app': '#f8fafc' } as Record<string, string>,
    error: null as string | null
  },
  themesState: {
    items: [
      {
        id: 'cyber_forest',
        name: 'Cyber Forest',
        author: 'System',
        description: 'Default',
        swatches: ['#2dd4bf', '#6366f1'],
        source: 'default'
      },
      {
        id: 'terra-test',
        name: 'Terra Test',
        author: 'Tester',
        description: 'A second theme',
        swatches: ['#c2410c', '#4d7c0f'],
        source: 'disk'
      }
    ],
    flatTokens: {} as Record<
      string,
      { dark: Record<string, string>; light: Record<string, string> }
    >,
    loadError: null as string | null,
    loading: false
  },
  themeStatus: {
    kind: 'info' as const,
    message: '',
    fields: [] as { field: string; message: string }[]
  },
  systemScheme: {
    mode: 'dark' as 'dark' | 'light'
  },
  applyTheme: vi.fn(),
  restoreActiveTheme: vi.fn(),
  injectTokens: vi.fn(),
  loadThemes: vi.fn(),
  clearStatus: vi.fn(),
  exportActiveTheme: vi.fn(),
  importThemeFromPath: vi.fn(),
  pickAndImportTheme: vi.fn(),
  setStatus: vi.fn(),
  // Captured Events.On registrations keyed by event name, plus the disposers
  // returned (so the unmount-disposal and drop-handler contracts can be
  // asserted without hitting the real Wails runtime).
  eventsHandlers: {} as Record<string, (ev: any) => void>,
  eventsDisposers: {} as Record<string, () => void>
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn((name: string, handler: (ev: any) => void) => {
      mocks.eventsHandlers[name] = handler
      const off = vi.fn()
      mocks.eventsDisposers[name] = off
      return off
    })
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
vi.mock('../../theme/inject', () => ({ injectTokens: mocks.injectTokens }))
vi.mock('../../theme/store.svelte', () => ({
  themeState: mocks.themeState,
  themesState: mocks.themesState,
  themeStatus: mocks.themeStatus,
  systemScheme: mocks.systemScheme,
  applyTheme: mocks.applyTheme,
  restoreActiveTheme: mocks.restoreActiveTheme,
  loadThemes: mocks.loadThemes,
  clearStatus: mocks.clearStatus,
  exportActiveTheme: mocks.exportActiveTheme,
  importThemeFromPath: mocks.importThemeFromPath,
  setStatus: mocks.setStatus,
  pickAndImportTheme: mocks.pickAndImportTheme
}))

import AppearanceTab from './AppearanceTab.svelte'

describe('AppearanceTab picker a11y (#50, #512)', () => {
  beforeEach(() => {
    mocks.applyTheme.mockReset()
    mocks.restoreActiveTheme.mockReset()
    mocks.injectTokens.mockReset()
    mocks.importThemeFromPath.mockReset()
    mocks.setStatus.mockReset()
    // Fresh event capture so a prior render's handler/disposer can't leak in.
    mocks.eventsHandlers = {}
    mocks.eventsDisposers = {}
    // Each test starts with no flat-token data; the preview tests opt in.
    mocks.themesState.flatTokens = {}
    // Each test starts from the default dark/system-resolved state so
    // a prior test's mutation doesn't leak across.
    mocks.themeState.mode = 'dark'
    mocks.systemScheme.mode = 'dark'
  })

  afterEach(() => {
    cleanup()
  })

  it('renders a grid group of theme buttons with the Active badge on the saved theme', () => {
    render(AppearanceTab)

    const grid = screen.getByRole('group', { name: 'Available themes' })
    expect(grid).toBeInTheDocument()

    const cards = within(grid).getAllByRole('button')
    expect(cards).toHaveLength(2)

    // The saved theme (themeState.id === 'cyber_forest') shows the Active
    // badge in its accessible name; the other card does not.
    const active = within(grid).getByRole('button', { name: /Cyber Forest/i })
    expect(active.textContent).toMatch(/Active/)
    const inactive = within(grid).getByRole('button', { name: /Terra Test/i })
    expect(inactive.textContent).not.toMatch(/Active/)
  })

  it('renders a radiogroup for Dark/Light/System with aria-checked', () => {
    render(AppearanceTab)

    const group = screen.getByRole('radiogroup', { name: 'Color mode' })
    expect(group).toBeInTheDocument()

    const radios = screen.getAllByRole('radio')
    expect(radios).toHaveLength(3)

    // themeState.mode === 'dark' → the Dark radio is checked.
    const dark = screen.getByRole('radio', { name: /Dark/i })
    expect(dark).toHaveAttribute('aria-checked', 'true')
    const light = screen.getByRole('radio', { name: /Light/i })
    expect(light).toHaveAttribute('aria-checked', 'false')
  })

  it('ArrowDown moves focus to the next card (roving tabindex)', async () => {
    render(AppearanceTab)

    const grid = screen.getByRole('group', { name: 'Available themes' })
    const cards = within(grid).getAllByRole('button')
    // Initially the first card is the roving-tabindex entry point (0).
    cards[0].focus()
    expect(document.activeElement).toBe(cards[0])

    // ArrowDown moves focus to the second card.
    await fireEvent.keyDown(cards[0], { key: 'ArrowDown' })
    expect(document.activeElement).toBe(cards[1])

    // The roving tabindex updated: card 1 is now tabbable (0), card 0 is
    // removed from the tab order (-1).
    expect(cards[1]).toHaveAttribute('tabindex', '0')
    expect(cards[0]).toHaveAttribute('tabindex', '-1')
  })

  it('Home jumps focus to the first card; End to the last', async () => {
    render(AppearanceTab)

    const grid = screen.getByRole('group', { name: 'Available themes' })
    const cards = within(grid).getAllByRole('button')
    cards[1].focus()
    expect(document.activeElement).toBe(cards[1])

    await fireEvent.keyDown(cards[1], { key: 'Home' })
    expect(document.activeElement).toBe(cards[0])

    await fireEvent.keyDown(cards[0], { key: 'End' })
    expect(document.activeElement).toBe(cards[1])
  })

  it('clicking a mode radio calls applyTheme with the new mode', async () => {
    render(AppearanceTab)

    const light = screen.getByRole('radio', { name: /Light/i })
    await fireEvent.click(light)

    expect(mocks.applyTheme).toHaveBeenCalledTimes(1)
    const [id, mode] = mocks.applyTheme.mock.calls[0]
    expect(id).toBe('cyber_forest') // mode change never changes the theme
    expect(mode).toBe('light')
  })

  describe('two-stage preview (#512)', () => {
    beforeEach(() => {
      // Tokens for the second theme so a staged preview is observable via
      // injectTokens; the first theme stays token-less so the active-theme
      // restore path (previewTheme === null) is the only thing that runs
      // for it on mount.
      mocks.themesState.flatTokens = {
        'terra-test': {
          dark: { '--color-surface-app': '#1a0f0a' },
          light: { '--color-surface-app': '#faf6f2' }
        }
      }
    })

    it('hover does NOT inject tokens (regression guard for the strobe)', async () => {
      render(AppearanceTab)
      await tick()
      mocks.injectTokens.mockClear()

      const grid = screen.getByRole('group', { name: 'Available themes' })
      const terra = within(grid).getByRole('button', { name: /Terra Test/i })
      // Hover/focus must only highlight the card — never rewrite :root tokens.
      await fireEvent.mouseEnter(terra)
      await fireEvent.mouseOver(terra)
      await tick()
      expect(mocks.injectTokens).not.toHaveBeenCalled()
    })

    it('single-click stages a preview: injects tokens and shows the banner', async () => {
      render(AppearanceTab)
      await tick()
      mocks.injectTokens.mockClear()

      expect(screen.queryByRole('status')).toBeNull()

      const grid = screen.getByRole('group', { name: 'Available themes' })
      const terra = within(grid).getByRole('button', { name: /Terra Test/i })
      await fireEvent.click(terra)
      await tick()

      // The previewed theme's dark tokens are injected in place of the active.
      expect(mocks.injectTokens).toHaveBeenCalledWith({
        '--color-surface-app': '#1a0f0a'
      })
      // The banner announces the staged theme.
      const banner = screen.getByRole('status')
      expect(banner).toHaveAttribute('aria-live', 'polite')
      expect(banner.textContent).toMatch(/Previewing Terra Test/i)
    })

    it('Apply commits via applyTheme and dismisses the banner', async () => {
      // applyTheme resolves true (success) so the banner clears on commit.
      mocks.applyTheme.mockResolvedValue(true)
      render(AppearanceTab)
      await tick()

      const grid = screen.getByRole('group', { name: 'Available themes' })
      const terra = within(grid).getByRole('button', { name: /Terra Test/i })
      await fireEvent.click(terra)
      await tick()
      expect(screen.getByRole('status')).toBeInTheDocument()

      const apply = screen.getByRole('button', { name: /^Apply$/i })
      await fireEvent.click(apply)
      await tick()

      expect(mocks.applyTheme).toHaveBeenCalledTimes(1)
      expect(mocks.applyTheme).toHaveBeenCalledWith('terra-test', 'dark')
      expect(screen.queryByRole('status')).toBeNull()
    })

    it('double-click commits via applyTheme', async () => {
      mocks.applyTheme.mockResolvedValue(true)
      render(AppearanceTab)
      await tick()
      mocks.applyTheme.mockClear()

      const grid = screen.getByRole('group', { name: 'Available themes' })
      const terra = within(grid).getByRole('button', { name: /Terra Test/i })
      await fireEvent.dblClick(terra)
      await tick()

      expect(mocks.applyTheme).toHaveBeenCalledWith('terra-test', 'dark')
    })

    it('Revert restores the active theme and dismisses the banner', async () => {
      render(AppearanceTab)
      await tick()
      mocks.restoreActiveTheme.mockClear()

      const grid = screen.getByRole('group', { name: 'Available themes' })
      const terra = within(grid).getByRole('button', { name: /Terra Test/i })
      await fireEvent.click(terra)
      await tick()
      expect(screen.getByRole('status')).toBeInTheDocument()

      const revert = screen.getByRole('button', { name: /^Revert$/i })
      await fireEvent.click(revert)
      await tick()

      expect(mocks.restoreActiveTheme).toHaveBeenCalled()
      expect(screen.queryByRole('status')).toBeNull()
    })

    it('Escape restores the active theme and dismisses the banner', async () => {
      render(AppearanceTab)
      await tick()
      mocks.restoreActiveTheme.mockClear()

      const grid = screen.getByRole('group', { name: 'Available themes' })
      const terra = within(grid).getByRole('button', { name: /Terra Test/i })
      await fireEvent.click(terra)
      await tick()
      expect(screen.getByRole('status')).toBeInTheDocument()

      // Esc on the card reverts.
      await fireEvent.keyDown(terra, { key: 'Escape' })
      await tick()

      expect(mocks.restoreActiveTheme).toHaveBeenCalled()
      expect(screen.queryByRole('status')).toBeNull()
    })

    it('unmount restores the active theme when a preview is staged', async () => {
      // Regression guard: onMount cleanup must call restoreActiveTheme
      // directly (not via the preview $effect, which tears down on unmount),
      // so navigating away mid-preview never leaves the workspace locked to
      // the previewed theme.
      const { unmount } = render(AppearanceTab)
      await tick()

      const grid = screen.getByRole('group', { name: 'Available themes' })
      const terra = within(grid).getByRole('button', { name: /Terra Test/i })
      await fireEvent.click(terra)
      await tick()
      // Clear the mount-time restore so the call isolates unmount behavior.
      mocks.restoreActiveTheme.mockClear()

      unmount()
      await tick()

      expect(mocks.restoreActiveTheme).toHaveBeenCalled()
    })

    it('unmount does not restore when no preview is staged', async () => {
      // A clean unmount (no in-flight preview) must not fire a redundant restore.
      const { unmount } = render(AppearanceTab)
      await tick()
      mocks.restoreActiveTheme.mockClear()

      unmount()
      await tick()

      expect(mocks.restoreActiveTheme).not.toHaveBeenCalled()
    })
  })

  it('shows a theme-typography indicator when the active theme overrides fonts (#82)', () => {
    // Give the active theme a typography block: the indicator derives from
    // themeState.darkTokens '--font-*' keys (theme-level, both modes).
    mocks.themeState.darkTokens = {
      '--color-surface-app': '#0c0c0e',
      '--font-body': "'Plus Jakarta Sans', sans-serif",
      '--font-mono': "'JetBrains Mono', monospace",
      '--font-headline': "'Hanken Grotesk', sans-serif"
    }
    render(AppearanceTab)

    const heading = screen.getByRole('heading', { name: /Theme typography/i })
    expect(heading).toBeInTheDocument()
    // Each overridden slot is surfaced with a display name from the registry.
    const region = heading.parentElement!
    expect(region.textContent).toContain('Plus Jakarta Sans')
    expect(region.textContent).toContain('JetBrains Mono')
    expect(region.textContent).toContain('Hanken Grotesk')
  })

  it('hides the theme-typography indicator when the theme defines no fonts (#82)', () => {
    // No '--font-*' tokens → no indicator section.
    mocks.themeState.darkTokens = { '--color-surface-app': '#0c0c0e' }
    render(AppearanceTab)

    expect(
      screen.queryByRole('heading', { name: /Theme typography/i })
    ).toBeNull()
  })

  describe('system-mode resolved-scheme announcement', () => {
    // The visible `· Dark`/`· Light` suffix on the System radio is
    // aria-hidden (avoids colliding with the Dark/Light radio names),
    // so an SR user on System mode would otherwise never learn which
    // scheme is resolved. The sr-only aria-live region is the AT
    // companion. These tests pin the derived expression's contract:
    // announce only in system mode, with the resolved scheme name.

    it('announces "Using dark appearance" when system mode resolves to dark', () => {
      mocks.themeState.mode = 'system'
      mocks.systemScheme.mode = 'dark'
      render(AppearanceTab)

      const live = screen.getByText('Using dark appearance')
      expect(live.closest('[aria-live]')).toHaveAttribute('aria-live', 'polite')
    })

    it('announces "Using light appearance" when system mode resolves to light', () => {
      mocks.themeState.mode = 'system'
      mocks.systemScheme.mode = 'light'
      render(AppearanceTab)

      expect(screen.getByText('Using light appearance')).toBeInTheDocument()
    })

    it('stays silent (empty region) when an explicit Dark/Light mode is active', () => {
      mocks.themeState.mode = 'dark'
      render(AppearanceTab)

      // The region still renders (so future system-mode switches can
      // announce without re-mounting), but holds no announcement text.
      const region = document.querySelector('[aria-live="polite"].sr-only')
      expect(region?.textContent?.trim()).toBe('')
    })
  })

  describe('swatch surface identity (#405)', () => {
    beforeEach(() => {
      mocks.themeState.mode = 'dark'
    })

    it('renders a mini-card chip filled with the theme surface bg + accent dots', () => {
      // Provide flatTokens for both themes so the swatch reads from them
      // rather than falling back to the CSS variable defaults.
      mocks.themesState.flatTokens = {
        cyber_forest: {
          dark: {
            '--color-surface-app': '#0c0c0e',
            '--color-accent-primary-start': '#2dd4bf',
            '--color-accent-secondary-start': '#6366f1'
          },
          light: {
            '--color-surface-app': '#f8fafc',
            '--color-accent-primary-start': '#0d9488',
            '--color-accent-secondary-start': '#4f46e5'
          }
        },
        'terra-test': {
          dark: {
            '--color-surface-app': '#1a0f0a',
            '--color-accent-primary-start': '#c2410c',
            '--color-accent-secondary-start': '#4d7c0f'
          },
          light: {
            '--color-surface-app': '#faf6f2',
            '--color-accent-primary-start': '#7c2d12',
            '--color-accent-secondary-start': '#365314'
          }
        }
      }

      render(AppearanceTab)

      // Each card has one chip + two dots.
      const grid = screen.getByRole('group', { name: 'Available themes' })
      const cards = within(grid).getAllByRole('button')
      expect(cards).toHaveLength(2)

      for (const card of cards) {
        const chip = card.querySelector(
          '.theme-swatch-chip'
        ) as HTMLElement | null
        expect(chip, 'chip must render').toBeTruthy()
        const dots = chip!.querySelectorAll('.theme-swatch-dot')
        expect(dots.length, 'exactly two accent dots').toBe(2)
      }

      // Cyber Forest's chip is filled with the dark surface bg (#0c0c0e).
      // jsdom normalizes hex to rgb() on read-back.
      const cfCard = within(grid).getByRole('button', {
        name: /Cyber Forest/i
      })
      const cfChip = cfCard.querySelector('.theme-swatch-chip') as HTMLElement
      expect(cfChip.style.backgroundColor).toBe('rgb(12, 12, 14)')
      // The first dot carries the primary accent.
      const cfDots = cfChip.querySelectorAll('.theme-swatch-dot')
      expect((cfDots[0] as HTMLElement).style.backgroundColor).toBe(
        'rgb(45, 212, 191)'
      )
      expect((cfDots[1] as HTMLElement).style.backgroundColor).toBe(
        'rgb(99, 102, 241)'
      )
    })

    it('surface fill distinguishes a warm theme from a cool theme at a glance', () => {
      mocks.themesState.flatTokens = {
        cyber_forest: {
          dark: {
            '--color-surface-app': '#0c0c0e',
            '--color-accent-primary-start': '#2dd4bf',
            '--color-accent-secondary-start': '#6366f1'
          },
          light: {}
        },
        'terra-test': {
          dark: {
            '--color-surface-app': '#1a0f0a',
            '--color-accent-primary-start': '#c2410c',
            '--color-accent-secondary-start': '#4d7c0f'
          },
          light: {}
        }
      }

      render(AppearanceTab)

      const grid = screen.getByRole('group', { name: 'Available themes' })
      const cfChip = within(grid)
        .getByRole('button', { name: /Cyber Forest/i })
        .querySelector('.theme-swatch-chip') as HTMLElement
      const terraChip = within(grid)
        .getByRole('button', { name: /Terra Test/i })
        .querySelector('.theme-swatch-chip') as HTMLElement
      // The whole point of #405: the surface fills differ so the user can
      // tell the themes apart by temperature, not just by accent.
      expect(cfChip.style.backgroundColor).not.toBe(
        terraChip.style.backgroundColor
      )
    })
  })

  describe('theme drag-drop import', () => {
    // The backend (main.go) forwards only OS drops that land on
    // #theme-file-drop-target, emitting theme:files-dropped with the paths.
    // The component reuses importThemeFromPath (the picker's import path) so
    // feedback flows through the same themeStatus live region.

    it('marks the list wrapper as a Wails file-drop target', () => {
      render(AppearanceTab)
      const target = document.getElementById('theme-file-drop-target')
      expect(target).not.toBeNull()
      expect(target).toHaveAttribute('data-file-drop-target')
    })

    it('subscribes to theme:files-dropped on mount and disposes on unmount', () => {
      const { unmount } = render(AppearanceTab)
      const handler = mocks.eventsHandlers['theme:files-dropped']
      expect(typeof handler).toBe('function')
      const off = mocks.eventsDisposers['theme:files-dropped']
      expect(off).not.toBeUndefined()
      unmount()
      expect(off).toHaveBeenCalledTimes(1)
    })

    it('imports a single dropped .json path via importThemeFromPath', async () => {
      render(AppearanceTab)
      const handler = mocks.eventsHandlers['theme:files-dropped']
      handler({ data: ['/users/me/themes/dusk.json'] })
      await tick()
      expect(mocks.importThemeFromPath).toHaveBeenCalledTimes(1)
      expect(mocks.importThemeFromPath).toHaveBeenCalledWith(
        '/users/me/themes/dusk.json'
      )
      // Success path delegates feedback to the store, not the component.
      expect(mocks.setStatus).not.toHaveBeenCalled()
    })

    it('rejects a drop of multiple files without importing', async () => {
      render(AppearanceTab)
      const handler = mocks.eventsHandlers['theme:files-dropped']
      handler({ data: ['/a/dusk.json', '/b/frost.json'] })
      await tick()
      expect(mocks.importThemeFromPath).not.toHaveBeenCalled()
      expect(mocks.setStatus).toHaveBeenCalledTimes(1)
      const [status] = mocks.setStatus.mock.calls[0]
      expect(status.kind).toBe('error')
      expect(status.message).toMatch(/one theme file at a time/i)
    })

    it('rejects a drop with no .json path (non-theme file)', async () => {
      render(AppearanceTab)
      const handler = mocks.eventsHandlers['theme:files-dropped']
      handler({ data: ['/users/me/image.png'] })
      await tick()
      expect(mocks.importThemeFromPath).not.toHaveBeenCalled()
      expect(mocks.setStatus).toHaveBeenCalledTimes(1)
      const [status] = mocks.setStatus.mock.calls[0]
      expect(status.kind).toBe('error')
      expect(status.message).toMatch(/drop a theme \.json/i)
    })

    it('ignores a malformed payload without importing or erroring', async () => {
      render(AppearanceTab)
      const handler = mocks.eventsHandlers['theme:files-dropped']
      handler({ data: 'not-an-array' })
      handler(undefined)
      await tick()
      expect(mocks.importThemeFromPath).not.toHaveBeenCalled()
      expect(mocks.setStatus).not.toHaveBeenCalled()
    })
  })
})
