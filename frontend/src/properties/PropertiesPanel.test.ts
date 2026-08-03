import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    SetPageProperty: vi.fn().mockResolvedValue(undefined),
    SetPageType: vi.fn().mockResolvedValue([]),
    TurnIntoPage: vi.fn().mockResolvedValue([]),
    ClearPageProperty: vi.fn().mockResolvedValue(undefined),
    GetType: vi.fn().mockResolvedValue(null),
    GetPageProperties: vi.fn().mockResolvedValue([])
  })
)
vi.mock('$silt-app', () => appMocks)

import PropertiesPanel from './PropertiesPanel.svelte'
import type { PagePropertyValue, PageTypeInfo } from './types'

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
    heroField: 'title',
    properties: [{ name: 'rating', type: 'number', min: 0, max: 5 }]
  },
  isSet: true,
  rawType: ''
}

function baseProps(overrides: Record<string, unknown> = {}) {
  return {
    open: true,
    info: bookInfo,
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
    ...overrides
  }
}

beforeEach(() => {
  appMocks.SetPageProperty.mockReset().mockResolvedValue(undefined)
  appMocks.SetPageType.mockReset().mockResolvedValue([])
  appMocks.TurnIntoPage.mockReset().mockResolvedValue([])
  appMocks.ClearPageProperty.mockReset().mockResolvedValue(undefined)
  appMocks.GetType.mockReset().mockResolvedValue(null)
  appMocks.GetPageProperties.mockReset().mockResolvedValue([])
})

afterEach(cleanup)

describe('PropertiesPanel', () => {
  it('renders as a non-blocking dialog (aria-modal=false) when open', () => {
    render(PropertiesPanel, { props: baseProps() })
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAttribute('aria-modal', 'false')
    expect(dialog).toHaveAttribute('aria-label', 'Page properties')
  })

  it('does not render when closed', () => {
    render(PropertiesPanel, { props: baseProps({ open: false }) })
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('closes on Escape', async () => {
    const onClose = vi.fn()
    render(PropertiesPanel, { props: baseProps({ onClose }) })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('renders the right control for each property type', () => {
    const values: PagePropertyValue[] = [
      {
        name: 't',
        label: 'T',
        type: 'text',
        value: '',
        isSet: false,
        required: false
      },
      {
        name: 'n',
        label: 'N',
        type: 'number',
        value: 0,
        isSet: false,
        required: false
      },
      {
        name: 'd',
        label: 'D',
        type: 'date',
        value: '',
        isSet: false,
        required: false
      },
      {
        name: 'dt',
        label: 'DT',
        type: 'datetime',
        value: '',
        isSet: false,
        required: false
      },
      {
        name: 'c',
        label: 'C',
        type: 'checkbox',
        value: false,
        isSet: false,
        required: false
      },
      {
        name: 's',
        label: 'S',
        type: 'select',
        value: '',
        isSet: false,
        required: false,
        options: ['a', 'b']
      },
      {
        name: 'm',
        label: 'M',
        type: 'multiselect',
        value: [],
        isSet: false,
        required: false,
        options: ['x', 'y']
      },
      {
        name: 'p',
        label: 'P',
        type: 'page',
        value: '',
        isSet: false,
        required: false
      },
      {
        name: 'ps',
        label: 'PS',
        type: 'pages',
        value: [],
        isSet: false,
        required: false
      }
    ]
    render(PropertiesPanel, { props: baseProps({ values }) })
    expect(screen.getByRole('switch', { name: 'C' })).toBeInTheDocument()
    // Selects render as <select> plus an empty "—" option each.
    expect(screen.getAllByRole('combobox').length).toBeGreaterThanOrEqual(1)
    // Multiselect with options renders a group of toggle chips.
    expect(screen.getByRole('group', { name: 'M' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'x' })).toBeInTheDocument()
    // Number inputs enforce bounds from the resolved schema.
    const num = document.getElementById('prop-n') as HTMLInputElement
    expect(num).not.toBeNull()
    expect(num.type).toBe('number')
    // Text/date/datetime/page render as text-like inputs.
    expect((document.getElementById('prop-t') as HTMLInputElement).type).toBe(
      'text'
    )
    expect((document.getElementById('prop-d') as HTMLInputElement).type).toBe(
      'date'
    )
    expect((document.getElementById('prop-dt') as HTMLInputElement).type).toBe(
      'datetime-local'
    )
    expect((document.getElementById('prop-p') as HTMLInputElement).type).toBe(
      'text'
    )
  })

  it('commits a text edit through SetPageProperty with the right args', async () => {
    const values: PagePropertyValue[] = [
      {
        name: 'title',
        label: 'Title',
        type: 'text',
        value: 'Dune',
        isSet: true,
        required: false
      }
    ]
    render(PropertiesPanel, { props: baseProps({ values }) })
    const input = screen.getByLabelText('Title') as HTMLInputElement
    await fireEvent.change(input, { target: { value: 'Dune Messiah' } })
    expect(appMocks.SetPageProperty).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Plan',
      'title',
      'Dune Messiah'
    )
  })

  it('surfaces an aria-live banner on rejection and triggers resync (not a blind revert)', async () => {
    appMocks.SetPageProperty.mockRejectedValue(new Error('value out of range'))
    const onChanged = vi.fn()
    const values: PagePropertyValue[] = [
      {
        name: 'rating',
        label: 'Rating',
        type: 'number',
        value: 3,
        isSet: true,
        required: false
      }
    ]
    render(PropertiesPanel, {
      props: baseProps({
        values,
        onChanged,
        info: {
          typeId: 'book',
          type: {
            id: 'book',
            name: 'Book',
            properties: [{ name: 'rating', type: 'number', min: 0, max: 5 }]
          },
          isSet: true,
          rawType: ''
        }
      })
    })
    const input = screen.getByLabelText('Rating') as HTMLInputElement
    await fireEvent.change(input, { target: { value: '99' } })
    // Rejection surfaces the banner (onResync must not clear liveError).
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert').textContent).toMatch(/out of range/i)
    // Resync hook fires so the controller can re-fetch disk truth (write may
    // have landed despite the error). Blind revert to prev is no longer used
    // when onResync is wired.
    await waitFor(() => {
      expect(onChanged).toHaveBeenCalled()
    })
  })

  it('lists types in the native <select> and calls SetPageType on change', async () => {
    const onMismatched = vi.fn()
    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        types: [
          { id: 'book', name: 'Book' },
          { id: 'movie', name: 'Movie' }
        ],
        onMismatched
      })
    })
    // A native <select> (role=combobox) replaces the custom menu — its
    // browser-rendered dropdown is never clipped by the panel's
    // overflow:hidden (the robustness win over the old popover).
    const select = screen.getByRole('combobox', { name: 'Page type' })
    expect(
      (select.querySelector('option[value="book"]') as HTMLOptionElement)
        .textContent
    ).toBe('Book')
    expect(
      (select.querySelector('option[value="movie"]') as HTMLOptionElement)
        .textContent
    ).toBe('Movie')

    await fireEvent.change(select, { target: { value: 'book' } })
    // Untyped → typed has nothing to map, so it assigns via SetPageType
    // directly (no turn-into dialog / TurnIntoPage).
    expect(appMocks.SetPageType).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Plan',
      'book'
    )
    expect(appMocks.TurnIntoPage).not.toHaveBeenCalled()
    // No mismatches → empty list forwarded.
    await tick()
    expect(onMismatched).toHaveBeenCalledWith([])
  })

  it('typed→typed turn-into confirms via atomic TurnIntoPage (not clear-loop + SetPageType)', async () => {
    // When a typed page switches type, TurnIntoDialog opens; confirming must
    // call TurnIntoPage once with (typeID, orphans) so type+clears are atomic.
    // The multi-write clear-then-SetPageType path is the MB-1 data-loss bug.
    appMocks.GetType.mockImplementation(async (id: string) => {
      if (id === 'book') return bookInfo.type
      if (id === 'movie')
        return {
          id: 'movie',
          name: 'Movie',
          properties: [{ name: 'title', type: 'text' }]
        }
      return null
    })
    appMocks.GetPageProperties.mockResolvedValue([
      {
        name: 'rating',
        label: 'Rating',
        type: 'number',
        value: 5,
        isSet: true,
        required: false
      }
    ])

    const onChanged = vi.fn()
    render(PropertiesPanel, {
      props: baseProps({
        info: bookInfo,
        values: [
          {
            name: 'rating',
            label: 'Rating',
            type: 'number',
            value: 5,
            isSet: true,
            required: false
          }
        ],
        types: [
          { id: 'book', name: 'Book' },
          { id: 'movie', name: 'Movie' }
        ],
        onChanged
      })
    })

    const select = screen.getByRole('combobox', { name: 'Page type' })
    await fireEvent.change(select, { target: { value: 'movie' } })

    // Dialog should open (blocking turn-into preview).
    await waitFor(() => {
      expect(screen.getByRole('dialog', { name: /Turn into/i })).toBeTruthy()
    })

    // Confirm without clearing orphans → TurnIntoPage(newId, []).
    const confirm = await screen.findByRole('button', {
      name: /Confirm|Turn into/i
    })
    await fireEvent.click(confirm)

    await waitFor(() => {
      expect(appMocks.TurnIntoPage).toHaveBeenCalled()
    })
    expect(appMocks.ClearPageProperty).not.toHaveBeenCalled()
    // SetPageType is only for untyped→typed / remove-type direct paths.
    expect(appMocks.SetPageType).not.toHaveBeenCalled()
    expect(appMocks.TurnIntoPage).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Plan',
      'movie',
      expect.any(Array)
    )
    await tick()
    expect(onChanged).toHaveBeenCalled()
  })

  it('fires onCreateType when the Create type button is clicked', async () => {
    const onCreateType = vi.fn()
    render(PropertiesPanel, {
      props: baseProps({ info: untypedInfo, onCreateType })
    })
    await fireEvent.click(screen.getByRole('button', { name: /Create type/i }))
    expect(onCreateType).toHaveBeenCalledOnce()
  })

  it('fires onRestoreExamples when the Restore examples button is clicked', async () => {
    const onRestoreExamples = vi.fn()
    render(PropertiesPanel, {
      props: baseProps({ info: untypedInfo, onRestoreExamples })
    })
    await fireEvent.click(
      screen.getByRole('button', { name: /Restore examples/i })
    )
    expect(onRestoreExamples).toHaveBeenCalledOnce()
  })

  it('keeps Create type + Restore examples reachable when the roster is empty (select disabled)', () => {
    render(PropertiesPanel, {
      props: baseProps({ info: untypedInfo, types: [] })
    })
    const select = screen.getByRole('combobox', {
      name: 'Page type'
    }) as HTMLSelectElement
    // Nothing to pick — the select is disabled with a "No types defined"
    // sentinel so the user understands the empty state, but the two
    // escape-hatch buttons stay live so they can recover.
    expect(select.disabled).toBe(true)
    expect(select.querySelector('option')?.textContent).toMatch(
      /No types defined/i
    )
    expect(screen.getByRole('button', { name: /Create type/i })).toBeEnabled()
    expect(
      screen.getByRole('button', { name: /Restore examples/i })
    ).toBeEnabled()
  })

  it('focuses the native <select> when typeMenuRequest bumps on open', async () => {
    // The pill's untyped click (and `/type` slash) bumps typeMenuRequest; the
    // panel responds by focusing the select (and auto-opening via showPicker
    // when supported) instead of opening a clipped popover. Pass a non-empty
    // roster so the select isn't disabled (a disabled select is unfocusable).
    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        types: [{ id: 'book', name: 'Book' }],
        typeMenuRequest: 1
      })
    })
    const select = screen.getByRole('combobox', { name: 'Page type' })
    await waitFor(() => {
      expect(document.activeElement).toBe(select)
    })
  })

  it('surfaces SetPageType mismatched names as field warnings', () => {
    appMocks.SetPageType.mockResolvedValue(['rating'])
    const values: PagePropertyValue[] = [
      {
        name: 'rating',
        label: 'Rating',
        type: 'number',
        value: 9,
        isSet: true,
        required: false
      }
    ]
    render(PropertiesPanel, {
      props: baseProps({
        values,
        mismatched: ['rating']
      })
    })
    expect(screen.getByText(/doesn't fit this type/i)).toBeInTheDocument()
  })

  it('shows a role="status" loading state (and not the empty message) while loading with no values', () => {
    render(PropertiesPanel, {
      props: baseProps({
        info: bookInfo,
        values: [],
        loading: true
      })
    })
    // Loading indicator is present and reachable as a status role.
    const statuses = screen.getAllByRole('status')
    expect(statuses.some((el) => /Loading/i.test(el.textContent ?? ''))).toBe(
      true
    )
    // The misleading "no properties" empty state is suppressed during loading.
    expect(screen.queryByText(/This type has no properties/i)).toBeNull()
  })

  it('shows an "Unrecognized type" message + Remove type button for a bogus type ref', async () => {
    const unknownInfo: PageTypeInfo = {
      typeId: '',
      type: { id: '', name: '' },
      isSet: false,
      rawType: 'wat'
    }
    render(PropertiesPanel, {
      props: baseProps({ info: unknownInfo, values: [] })
    })
    expect(screen.getByText(/Unrecognized type 'wat'/i)).toBeInTheDocument()
    expect(
      screen.getByText(/isn't defined in .system\/types/i)
    ).toBeInTheDocument()

    // The bogus ref can't be cleared via the <select> (info.type.id is
    // already '' so picking "No type" wouldn't fire a change), so an
    // explicit Remove-type action shows for the unknown case.
    await fireEvent.click(screen.getByRole('button', { name: /Remove type/i }))
    expect(appMocks.SetPageType).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Plan',
      ''
    )
  })

  it('marks required field controls with aria-required', () => {
    const values: PagePropertyValue[] = [
      {
        name: 'title',
        label: 'Title',
        type: 'text',
        value: '',
        isSet: false,
        required: true
      }
    ]
    render(PropertiesPanel, { props: baseProps({ values }) })
    const input = document.getElementById('prop-title') as HTMLInputElement
    expect(input).not.toBeNull()
    expect(input.getAttribute('aria-required')).toBe('true')
  })
})
