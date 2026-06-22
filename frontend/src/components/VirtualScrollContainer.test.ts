// Component test for VirtualScrollContainer's inline title editing (#259).
// Verifies that typing into the contenteditable `<h1>` title does NOT have
// its caret collapse to position 0 when the debounced rename round-trip
// flows back through the `page` prop. The editor body has an isFocused guard
// (TipTapEditor.svelte); the title needs the same protection.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, cleanup, waitFor } from '@testing-library/svelte'
import { tick } from 'svelte'

const mocks = vi.hoisted(() => ({
  fetchPageBlocks: vi.fn(),
  renamePage: vi.fn(),
  eventsOn: vi.fn(() => () => {})
}))

vi.mock('../../wailsjs/go/main/App.js', () => ({
  FetchPageBlocks: mocks.fetchPageBlocks,
  RenamePage: mocks.renamePage
}))

vi.mock('../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: mocks.eventsOn
}))

// Stub the heavy editor and utility bar — the title-editing contract is
// independent of their internals.
vi.mock('./TipTapEditor.svelte', async () => {
  const mod = await import('./TipTapEditor.stub.svelte')
  return { default: mod.default }
})

vi.mock('./editor/EditorUtilityBar.svelte', async () => {
  const mod = await import('./editor/EditorUtilityBar.stub.svelte')
  return { default: mod.default }
})

vi.mock('../lib/viewMode.svelte', () => ({
  getViewMode: vi.fn(() => 'edit' as const),
  toggleViewMode: vi.fn()
}))

import VirtualScrollContainer from './VirtualScrollContainer.svelte'

function makeProps(overrides: Record<string, unknown> = {}) {
  return {
    notebook: 'Work',
    section: 'Projects',
    page: 'Untitled',
    isActive: true,
    onPageRenamed: vi.fn(),
    ...overrides
  }
}

describe('VirtualScrollContainer — inline title editing (#259)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.fetchPageBlocks.mockResolvedValue([])
    mocks.renamePage.mockResolvedValue(undefined)
  })

  afterEach(() => {
    cleanup()
  })

  it('does NOT overwrite title text when page prop changes during focus', async () => {
    const props = makeProps()
    const { rerender } = render(VirtualScrollContainer, { props })

    await waitFor(() => {
      expect(mocks.fetchPageBlocks).toHaveBeenCalled()
    })

    const h1 = document.querySelector(
      'h1[contenteditable]'
    ) as HTMLHeadingElement
    expect(h1).toBeTruthy()
    expect(h1.textContent).toBe('Untitled')

    // Focus the title (simulates user starting to edit the page name).
    h1.focus()
    await tick()

    // Simulate the rename round-trip: the debounced doRename fires,
    // RenamePage IPC resolves, onPageRenamed is called, and the parent
    // re-renders with a new page value flowing back into the prop.
    //
    // On the BUGGY code, `{page}` is reactively bound in the template, so
    // Svelte patches the `<h1>` text to whatever the new `page` is —
    // overwriting whatever the user is typing and collapsing the caret.
    //
    // On the FIXED code, a focus guard prevents the reactive patch while
    // the user is editing, so the title text is preserved.
    rerender({ ...props, page: 'Different Value' })
    await tick()

    // The displayed title should NOT have been overwritten while focused.
    expect(h1.textContent).toBe('Untitled')
  })

  it('updates title text when page prop changes and title is NOT focused', async () => {
    const props = makeProps()
    const { rerender } = render(VirtualScrollContainer, { props })

    await waitFor(() => {
      expect(mocks.fetchPageBlocks).toHaveBeenCalled()
    })

    const h1 = document.querySelector(
      'h1[contenteditable]'
    ) as HTMLHeadingElement
    expect(h1.textContent).toBe('Untitled')

    // Title is NOT focused — external page changes should sync into the DOM
    // (e.g. navigating to a different page, or a rename committed elsewhere).
    rerender({ ...props, page: 'Renamed Externally' })
    await tick()

    expect(h1.textContent).toBe('Renamed Externally')
  })
})
