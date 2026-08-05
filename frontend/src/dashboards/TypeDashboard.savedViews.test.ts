// Component-level coverage for the typed-notes dashboard saved-view chrome
// (#863): the confirmSaveView / updateActiveView / deleteActiveView /
// persistAll paths driven through the rendered saved-views bar. The pure
// coerce/load/strip helpers are covered in dashboardSavedViews.test.ts; the
// read/write IO surface is covered in dashboardSettings.test.ts. This file
// pins the component wiring — specifically the `savedViewsBusy` mid-flight
// guard (#868) that prevents a double-Enter from minting two ids and
// last-writer-win'ing the first created view away, plus the aria-live error
// surfacing.
//
// Mock pattern per AGENTS.md: vi.hoisted + createAppIpcMocks over the
// `$silt-app` alias (registered on globalThis by vitest.setup.ts so it is
// safe inside the hoisted factory). The alias resolves to the same physical
// bindings file the production module imports, so mocking `$silt-app`
// intercepts ListTypes / QueryPagesByType / SetTypedNotesSavedViews.

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'

// Two pages of a "book" type so the table + sort/filter paths render. The
// saved-view bar is independent of row content, but the dashboard only shows
// its chrome once a type is loaded and the query resolves.
const BOOK_ROWS = [
  {
    source: 'vault',
    notebook: 'Work',
    section: 'Reading',
    page: 'Dune',
    properties: [
      { name: 'title', valueText: 'Dune', valueType: 'text' },
      { name: 'status', valueText: 'read', valueType: 'select' }
    ]
  },
  {
    source: 'vault',
    notebook: 'Work',
    section: 'Reading',
    page: 'Neuromancer',
    properties: [
      { name: 'title', valueText: 'Neuromancer', valueType: 'text' },
      { name: 'status', valueText: 'reading', valueType: 'select' }
    ]
  }
]

const BOOK_TYPE = {
  id: 'book',
  name: 'Book',
  icon: 'menu_book',
  heroField: 'title',
  properties: [
    { name: 'title', label: 'Title', type: 'text' },
    {
      name: 'status',
      label: 'Status',
      type: 'select',
      options: ['read', 'reading']
    }
  ]
}

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    ListTypes: vi.fn(),
    QueryPagesByType: vi.fn(),
    SetTypedNotesSavedViews: vi.fn()
  })
)
vi.mock('$silt-app', () => appMocks)

// store.svelte.ts imports Events at module load — stub the wails runtime so
// importing the real settings store doesn't touch IPC. The dashboard also
// subscribes to `types:changed` on mount; On returns an unsub stub.
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => () => {})
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
    Nullable: <T>(fn: T) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

// Imports AFTER mocks.
import { settings } from '../settings/store.svelte'
import TypeDashboard from './TypeDashboard.svelte'
import type { DashboardSavedView } from './dashboardSavedViews'

// Seed the live settings snapshot so loadTypedNotesSavedViews sees a known
// saved-views slice. Cast through unknown — only the nested path the module
// walks is populated, not the full SystemConfig shape.
function seedConfig(savedViews: unknown[]): void {
  settings.config = {
    ui: { dashboards: { typed_notes: { saved_views: savedViews } } },
    hotkeys: {}
  } as unknown as typeof settings.config
}

// A book-typed view with every dashboard dim populated, so selecting it makes
// the live state match (dirty=false); toggling viewMode then dirties it.
function bookView(
  overrides: Partial<DashboardSavedView> = {}
): DashboardSavedView {
  return {
    id: 'v1',
    name: 'Reading List',
    typeId: 'book',
    filter: {},
    sort: { property: '', desc: false },
    groupBy: '',
    viewMode: 'list',
    ...overrides
  }
}

async function mount(): Promise<void> {
  render(TypeDashboard, {
    props: { typeName: 'book', onOpenPage: vi.fn(), onBack: vi.fn() }
  })
  await waitFor(() => {
    expect(appMocks.QueryPagesByType).toHaveBeenCalled()
  })
  await tick()
}

describe('TypeDashboard — saved-view chrome (#863/#868)', () => {
  beforeEach(() => {
    appMocks.ListTypes.mockReset()
    appMocks.QueryPagesByType.mockReset()
    appMocks.SetTypedNotesSavedViews.mockReset()
    appMocks.ListTypes.mockResolvedValue({ types: [BOOK_TYPE] })
    appMocks.QueryPagesByType.mockResolvedValue(BOOK_ROWS)
    appMocks.SetTypedNotesSavedViews.mockResolvedValue(undefined)
    // Empty saved-views slice by default; tests that need a pre-seeded view
    // call seedConfig themselves after this reset.
    seedConfig([])
  })

  afterEach(() => {
    cleanup()
    // Avoid cross-test leakage through the singleton settings store.
    settings.config = null
  })

  it('savedViewsBusy disables the Save affordance during a pending save-new IPC', async () => {
    // Hold the IPC pending so savedViewsBusy stays true until we resolve.
    let resolveIpc: () => void = () => {}
    appMocks.SetTypedNotesSavedViews.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolveIpc = resolve
        })
    )

    await mount()
    // No saved views seeded → the bar shows "Save current".
    await fireEvent.click(screen.getByRole('button', { name: 'Save current' }))
    const input = screen.getByRole('textbox', { name: 'New saved view name' })
    await fireEvent.input(input, { target: { value: 'Pending View' } })

    // Commit the save — the dialog closes and the IPC is now in-flight.
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    // The new view isn't mirrored into config until the IPC resolves, so the
    // bar still shows "Save current" — and it must be disabled while busy.
    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: 'Save current' })
      ).toBeDisabled()
    })

    // Resolving the IPC flips savedViewsBusy back to false. The optimistic
    // mirror also lands the new view, so the affordance becomes "Save as new"
    // (activeSavedView is now set) — and it must be re-enabled.
    resolveIpc()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save as new' })).toBeEnabled()
    })
  })

  it('savedViewsBusy disables Update / Delete / Save-as-new during a pending update IPC', async () => {
    // Pre-seed a book view and select it so Update/Delete render.
    seedConfig([bookView()])
    let resolveIpc: () => void = () => {}
    appMocks.SetTypedNotesSavedViews.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolveIpc = resolve
        })
    )

    await mount()
    // Select the seeded view → applySavedView sets activeSavedViewId.
    const select = screen.getByRole('combobox', { name: 'Saved views' })
    await fireEvent.change(select, { target: { value: 'v1' } })

    // Dirty the view (toggle to Board); the view snapshots list mode, so the
    // Update button appears.
    await fireEvent.click(screen.getByRole('radio', { name: 'Board view' }))
    const updateBtn = await screen.findByRole('button', {
      name: /Update .Reading List./
    })
    expect(updateBtn).toBeEnabled()

    // Kick off the update — the IPC is now pending.
    await fireEvent.click(updateBtn)

    // All three mutating affordances are disabled while the update is
    // in-flight: Update, Delete, and Save-as-new.
    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: /Update .Reading List./ })
      ).toBeDisabled()
    })
    expect(
      screen.getByRole('button', { name: 'Delete saved view Reading List' })
    ).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Save as new' })).toBeDisabled()

    resolveIpc()
  })

  it('double Enter during a pending save triggers only one SetTypedNotesSavedViews call', async () => {
    // A never-resolving mock keeps the first save in-flight so the only thing
    // standing between the user and a second mint+write is the savedViewsBusy
    // guard at the top of confirmSaveView.
    let resolveIpc: () => void = () => {}
    appMocks.SetTypedNotesSavedViews.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolveIpc = resolve
        })
    )

    await mount()
    await fireEvent.click(screen.getByRole('button', { name: 'Save current' }))
    const input = screen.getByRole('textbox', { name: 'New saved view name' })
    await fireEvent.input(input, { target: { value: 'My View' } })

    // Two Enters dispatched back-to-back with no await between: the first
    // confirmSaveView runs synchronously through persistAll (savedViewsBusy
    // flips true before yielding at the IPC await); the second Enter's
    // confirmSaveView reads savedViewsBusy=true and returns early — no second
    // id mint, no second write.
    fireEvent.keyDown(input, { key: 'Enter' })
    fireEvent.keyDown(input, { key: 'Enter' })
    await tick()

    expect(appMocks.SetTypedNotesSavedViews).toHaveBeenCalledTimes(1)

    // Release the pending IPC so teardown is clean.
    resolveIpc()
  })

  it('double Save-click during a pending save also triggers only one SetTypedNotesSavedViews call', async () => {
    // Same guard, exercised via the Save button instead of the Enter key —
    // the button's disabled flag is applied on re-render, so two synchronous
    // clicks both dispatch; the busy guard is what blocks the second.
    let resolveIpc: () => void = () => {}
    appMocks.SetTypedNotesSavedViews.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolveIpc = resolve
        })
    )

    await mount()
    await fireEvent.click(screen.getByRole('button', { name: 'Save current' }))
    const input = screen.getByRole('textbox', { name: 'New saved view name' })
    await fireEvent.input(input, { target: { value: 'My View' } })
    const saveBtn = screen.getByRole('button', { name: 'Save' })

    fireEvent.click(saveBtn)
    fireEvent.click(saveBtn)
    await tick()

    expect(appMocks.SetTypedNotesSavedViews).toHaveBeenCalledTimes(1)

    resolveIpc()
  })

  it('SetTypedNotesSavedViews rejection surfaces a non-success message in the savedViewsMessage aria-live region', async () => {
    appMocks.SetTypedNotesSavedViews.mockRejectedValue(
      new Error('vault not loaded')
    )

    await mount()
    await fireEvent.click(screen.getByRole('button', { name: 'Save current' }))
    const input = screen.getByRole('textbox', { name: 'New saved view name' })
    await fireEvent.input(input, { target: { value: 'Doomed View' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    // The saved-views-bar aria-live region carries the error string (fail-
    // loud) rather than the success message that would land on resolve.
    // The text only exists in that <span aria-live="polite">.
    const liveRegion = await screen.findByText('vault not loaded')
    expect(liveRegion).toHaveAttribute('aria-live', 'polite')
    // And NOT the success copy that the resolved branch would surface.
    expect(screen.queryByText('Saved view')).toBeNull()
  })

  it('failed confirmSaveView leaves activeSavedViewId pointing at no ghost', async () => {
    // Pre-seed + select a real view so the <select> renders and its bound
    // value reflects activeSavedViewId — that's the only DOM reflection of
    // the state, so the assertion has to go through it. A rejecting persist
    // must NOT adopt the would-be-new id; activeSavedViewId stays at the
    // prior 'v1' rather than dangling at a never-persisted ghost.
    appMocks.SetTypedNotesSavedViews.mockRejectedValue(
      new Error('vault not loaded')
    )
    seedConfig([bookView()])

    await mount()
    const select = screen.getByRole('combobox', { name: 'Saved views' })
    await fireEvent.change(select, { target: { value: 'v1' } })
    expect(select).toHaveValue('v1')

    // "Save as new" path: a fresh id is minted, but the persist rejects, so
    // the new id must not stick.
    await fireEvent.click(screen.getByRole('button', { name: 'Save as new' }))
    const input = screen.getByRole('textbox', { name: 'New saved view name' })
    await fireEvent.input(input, { target: { value: 'Doomed View' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    // activeSavedViewId is unchanged from its prior 'v1' — no ghost. The
    // select's bound value is its DOM reflection; without this guard it
    // would carry the never-persisted uuid (and show the placeholder).
    await waitFor(() => {
      expect(select).toHaveValue('v1')
    })
  })
})
