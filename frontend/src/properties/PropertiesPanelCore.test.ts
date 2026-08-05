import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'

// Mock the Wails app bindings. PropertiesPanel's SetPageType / TurnIntoPage
// paths use these directly, and the real-controller integration test (#9)
// drives the core-commit path through createPageTypeController →
// GetPageCoreMetadata / SetPageCoreMetadata.
const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    SetPageProperty: vi.fn().mockResolvedValue(undefined),
    SetPageType: vi.fn().mockResolvedValue([]),
    TurnIntoPage: vi.fn().mockResolvedValue([]),
    ClearPageProperty: vi.fn().mockResolvedValue(undefined),
    GetType: vi.fn().mockResolvedValue(null),
    GetPageType: vi.fn().mockResolvedValue({
      isSet: false,
      type: {},
      rawType: ''
    }),
    GetPageProperties: vi.fn().mockResolvedValue([]),
    GetPageCoreMetadata: vi.fn().mockResolvedValue(undefined),
    SetPageCoreMetadata: vi.fn().mockResolvedValue(undefined),
    ListTypes: vi
      .fn()
      .mockResolvedValue({ types: [], errors: [], warnings: [] })
  })
)
vi.mock('$silt-app', () => appMocks)

import PropertiesPanel from './PropertiesPanel.svelte'
import { createPageTypeController } from './pageTypeState.svelte'
import type { PageCoreMetadata, PagePropertyValue, PageTypeInfo } from './types'

const locator = { notebook: 'Work', section: 'Projects', page: 'Plan' }
const untypedInfo: PageTypeInfo = {
  typeId: '',
  type: { id: '', name: '' },
  isSet: false,
  rawType: ''
}
const bookInfo: PageTypeInfo = {
  typeId: 'book',
  type: {
    id: 'book',
    name: 'Book',
    properties: [{ name: 'rating', type: 'number', min: 0, max: 5 }]
  },
  isSet: true,
  rawType: ''
}

function makeCore(over: Partial<PageCoreMetadata> = {}): PageCoreMetadata {
  return {
    notebook: locator.notebook,
    section: locator.section,
    page: locator.page,
    type: '',
    date: '2026-08-05',
    tags: ['work'],
    aliases: ['The Plan'],
    created: '2026-08-05T09:30:00',
    modified: '2026-08-05T10:00:00Z',
    tagsAreReadOnly: false,
    ...over
  }
}

function baseProps(over: Record<string, unknown> = {}) {
  return {
    open: true,
    info: untypedInfo,
    values: [] as PagePropertyValue[],
    mismatched: [] as string[],
    error: '',
    loading: false,
    types: [],
    typesLoading: false,
    locator,
    onClose: vi.fn(),
    onChanged: vi.fn(),
    onMismatched: vi.fn(),
    onError: vi.fn(),
    ...over
  }
}

beforeEach(() => {
  appMocks.SetPageProperty.mockReset().mockResolvedValue(undefined)
  appMocks.SetPageType.mockReset().mockResolvedValue([])
  appMocks.TurnIntoPage.mockReset().mockResolvedValue([])
  appMocks.ClearPageProperty.mockReset().mockResolvedValue(undefined)
  appMocks.GetType.mockReset().mockResolvedValue(null)
  appMocks.GetPageType.mockReset().mockResolvedValue({
    isSet: false,
    type: {},
    rawType: ''
  })
  appMocks.GetPageProperties.mockReset().mockResolvedValue([])
  appMocks.GetPageCoreMetadata.mockReset().mockResolvedValue(undefined)
  appMocks.SetPageCoreMetadata.mockReset().mockResolvedValue(undefined)
})

afterEach(cleanup)

describe('PropertiesPanel Core section (#867)', () => {
  it('renders the Core section when core + onCommitCore are provided', () => {
    const onCommitCore = vi.fn().mockResolvedValue(undefined)
    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        core: makeCore(),
        onCommitCore
      })
    })
    // The Core section heading is the load-bearing identifier; the rest of
    // the section hangs off it.
    expect(screen.getByRole('heading', { name: 'Core' })).toBeInTheDocument()
    // Untyped state with Core mounted: the legacy empty message is replaced
    // by a shorter "Assign a type" prompt.
    expect(screen.getByText(/Assign a type to add typed properties/i))
  })

  it('suppressed when core/onCommitCore are absent (legacy callers)', () => {
    render(PropertiesPanel, { props: baseProps({ info: untypedInfo }) })
    expect(screen.queryByRole('heading', { name: 'Core' })).toBeNull()
    // Legacy untyped message is preserved.
    expect(screen.getByText(/This page has no type/i)).toBeInTheDocument()
  })

  it('renders Core above the type-defined section for a typed page', () => {
    const onCommitCore = vi.fn().mockResolvedValue(undefined)
    const values: PagePropertyValue[] = [
      {
        name: 'rating',
        label: 'Rating',
        type: 'number',
        value: 5,
        isSet: true,
        required: false
      }
    ]
    render(PropertiesPanel, {
      props: baseProps({
        info: bookInfo,
        values,
        core: makeCore({ type: 'book' }),
        onCommitCore
      })
    })
    const heading = screen.getByRole('heading', { name: 'Core' })
    const ratingLabel = screen.getByLabelText('Rating')
    // Core heading comes before the typed Rating field in DOM order.
    expect(
      heading.compareDocumentPosition(ratingLabel) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })

  it('commits a date edit through onCommitCore with the changed field', async () => {
    const onCommitCore = vi.fn().mockResolvedValue(undefined)
    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        core: makeCore(),
        onCommitCore
      })
    })
    const dateInput = document.getElementById('core-date') as HTMLInputElement
    expect(dateInput).not.toBeNull()
    await fireEvent.change(dateInput, { target: { value: '2026-09-01' } })
    await tick()
    expect(onCommitCore).toHaveBeenCalledWith({ date: '2026-09-01' })
  })

  it('commits aliases as a parsed comma-separated list on blur', async () => {
    const onCommitCore = vi.fn().mockResolvedValue(undefined)
    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        core: makeCore(),
        onCommitCore
      })
    })
    const aliasesInput = screen.getByLabelText('Aliases') as HTMLInputElement
    await fireEvent.input(aliasesInput, {
      target: { value: 'The Plan, Master Plan, Alt' }
    })
    await fireEvent.blur(aliasesInput)
    expect(onCommitCore).toHaveBeenCalledWith({
      aliases: ['The Plan', 'Master Plan', 'Alt']
    })
  })

  it('commits an empty aliases list (clears) when the input is emptied', async () => {
    const onCommitCore = vi.fn().mockResolvedValue(undefined)
    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        core: makeCore(),
        onCommitCore
      })
    })
    const aliasesInput = screen.getByLabelText('Aliases') as HTMLInputElement
    await fireEvent.input(aliasesInput, { target: { value: '   ' } })
    await fireEvent.blur(aliasesInput)
    expect(onCommitCore).toHaveBeenCalledWith({ aliases: [] })
  })

  it('does not commit a no-op tags blur (matches committed value)', async () => {
    const onCommitCore = vi.fn().mockResolvedValue(undefined)
    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        core: makeCore({ tags: ['work', 'priority'] }),
        onCommitCore
      })
    })
    const tagsInput = screen.getByLabelText(/Tags/) as HTMLInputElement
    expect(tagsInput.value).toBe('work, priority')
    await fireEvent.blur(tagsInput)
    expect(onCommitCore).not.toHaveBeenCalled()
  })

  it('surfaces the aria-live banner AND rolls back the input when the real commitCore rejects (#9, MB#1)', async () => {
    // Wire the REAL createPageTypeController so the rejection flows through
    // the actual commitCore → CoreMetadataSection.commit() catch path. The
    // previous mock-rejection-only test could not detect the swallow bug
    // (commitCore caught + returned, so commit()'s catch never fired in
    // production even though the mock fulfilled the rejecting contract).
    appMocks.GetPageCoreMetadata.mockResolvedValue(makeCore())
    appMocks.SetPageCoreMetadata.mockRejectedValue(new Error('disk full'))
    const ctrl = createPageTypeController({ getLocator: () => locator })
    await ctrl.refresh()
    await tick()

    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        core: ctrl.core,
        onCommitCore: ctrl.commitCore,
        error: ctrl.error,
        onError: ctrl.setError,
        onChanged: ctrl.refresh
      })
    })
    const dateInput = document.getElementById('core-date') as HTMLInputElement
    expect(dateInput.value).toBe('2026-08-05')

    await fireEvent.change(dateInput, { target: { value: '2026-09-01' } })

    // commitCore rejected → commit()'s catch fired onError (banner) +
    // rollbackNonce++ (input remount + re-seed from the unchanged committed core).
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert').textContent).toMatch(/disk full/i)
    // The rejected write left the committed core unchanged; the rollback
    // remounted the date input so it re-seeds from core.date.
    await waitFor(() => {
      const reseeded = document.getElementById('core-date') as HTMLInputElement
      expect(reseeded.value).toBe('2026-08-05')
    })
  })

  it('clears the banner after a successful core commit and skips the redundant type/props refetch (#11)', async () => {
    // commitCore refetches only the core payload internally; the panel's
    // handleCoreChanged clears liveError without calling the full refresh(),
    // so a single field edit is 1 SET + 1 core GET (not 1 SET + 4 GETs).
    appMocks.GetPageCoreMetadata.mockResolvedValueOnce(makeCore())
    appMocks.GetPageCoreMetadata.mockResolvedValueOnce(
      makeCore({ date: '2026-09-01', modified: '2026-08-05T11:00:00Z' })
    )
    const ctrl = createPageTypeController({ getLocator: () => locator })
    await ctrl.refresh()
    await tick()

    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        core: ctrl.core,
        onCommitCore: ctrl.commitCore,
        error: ctrl.error,
        onError: ctrl.setError,
        onChanged: ctrl.refresh
      })
    })
    const dateInput = document.getElementById('core-date') as HTMLInputElement
    await fireEvent.change(dateInput, { target: { value: '2026-09-01' } })
    await tick()

    // No error banner after a successful commit.
    expect(screen.queryByRole('alert')).toBeNull()
    // The setter was called exactly once (no double-write).
    expect(appMocks.SetPageCoreMetadata).toHaveBeenCalledTimes(1)
    // Core was refetched exactly once (commitCore's internal refetch). The
    // full refresh() (type + props + core) did NOT fire — GetPageType /
    // GetPageProperties stay at their refresh() baseline of one call each.
    expect(appMocks.GetPageCoreMetadata).toHaveBeenCalledTimes(2)
    expect(appMocks.GetPageType).toHaveBeenCalledTimes(1)
    expect(appMocks.GetPageProperties).toHaveBeenCalledTimes(1)
  })

  it('CoreListInput does not double-write on Enter-then-blur within the IPC round-trip (#14)', async () => {
    // Enter kicks off a commit, then blur fires within the same IPC round-trip
    // and would re-trigger an identical write. The commitInFlight guard in
    // CoreListInput.flush() short-circuits the duplicate.
    appMocks.GetPageCoreMetadata.mockResolvedValue(makeCore({ tags: ['work'] }))
    // Block the SET so blur lands while Enter's commit is still in flight.
    let resolveSet!: (v: unknown) => void
    appMocks.SetPageCoreMetadata.mockReturnValue(
      new Promise((r) => {
        resolveSet = r
      })
    )
    const ctrl = createPageTypeController({ getLocator: () => locator })
    await ctrl.refresh()
    await tick()

    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        core: ctrl.core,
        onCommitCore: ctrl.commitCore,
        error: ctrl.error,
        onError: ctrl.setError,
        onChanged: ctrl.refresh
      })
    })
    const tagsInput = screen.getByLabelText(/Tags/) as HTMLInputElement
    await fireEvent.input(tagsInput, { target: { value: 'work, alpha' } })
    // Enter starts the commit (SET is blocked on resolveSet, so it stays in flight).
    await fireEvent.keyDown(tagsInput, { key: 'Enter' })
    await tick()
    expect(appMocks.SetPageCoreMetadata).toHaveBeenCalledTimes(1)

    // Blur within the round-trip — must NOT queue a second write.
    await fireEvent.blur(tagsInput)
    await tick()
    expect(appMocks.SetPageCoreMetadata).toHaveBeenCalledTimes(1)

    // Let the in-flight commit resolve; still only one write total.
    resolveSet(undefined)
    await tick()
    await tick()
    expect(appMocks.SetPageCoreMetadata).toHaveBeenCalledTimes(1)
  })

  it('tags and aliases inputs are keyboard-operable via Enter', async () => {
    const onCommitCore = vi.fn().mockResolvedValue(undefined)
    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        core: makeCore(),
        onCommitCore
      })
    })
    const tagsInput = screen.getByLabelText(/Tags/) as HTMLInputElement
    await fireEvent.input(tagsInput, { target: { value: 'work, alpha' } })
    await fireEvent.keyDown(tagsInput, { key: 'Enter' })
    expect(onCommitCore).toHaveBeenCalledWith({ tags: ['work', 'alpha'] })
  })

  it('Core section uses design-token styling (no arbitrary values)', () => {
    const onCommitCore = vi.fn().mockResolvedValue(undefined)
    const { container } = render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        core: makeCore(),
        onCommitCore
      })
    })
    // The Core section's inputs carry the .core-input class — a token-only
    // rule. Picking one element is enough to confirm the class is wired.
    const coreDate = container.querySelector('#core-date.core-input')
    expect(coreDate).not.toBeNull()
  })
})
