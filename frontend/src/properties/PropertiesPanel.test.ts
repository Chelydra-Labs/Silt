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
    SetPageType: vi.fn().mockResolvedValue([])
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

  it('closes on Escape (deferring to an open type menu first)', async () => {
    const onClose = vi.fn()
    render(PropertiesPanel, { props: baseProps({ onClose }) })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('Escape closes the type menu before closing the panel', async () => {
    const onClose = vi.fn()
    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        onClose,
        types: [{ id: 'book', name: 'Book' }]
      })
    })
    await fireEvent.click(
      screen.getByRole('button', { name: /Assign a type/i })
    )
    expect(screen.getByRole('menu')).toBeInTheDocument()
    // First Esc: menu consumes it; panel stays open.
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('menu')).toBeNull()
    expect(onClose).not.toHaveBeenCalled()
    // Second Esc: panel closes.
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

  it('reverts the field and surfaces an aria-live banner on rejection', async () => {
    appMocks.SetPageProperty.mockRejectedValue(new Error('value out of range'))
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
    // Rejection surfaces the banner.
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert').textContent).toMatch(/out of range/i)
    // The optimistic value reverts to the last accepted value once the rejection settles.
    await waitFor(() => {
      expect((screen.getByLabelText('Rating') as HTMLInputElement).value).toBe(
        '3'
      )
    })
  })

  it('lists types in the menu and calls SetPageType on select', async () => {
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
    await fireEvent.click(
      screen.getByRole('button', { name: /Assign a type/i })
    )
    expect(screen.getByRole('menuitem', { name: /Book/ })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /Movie/ })).toBeInTheDocument()

    await fireEvent.click(screen.getByRole('menuitem', { name: /Book/ }))
    expect(appMocks.SetPageType).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Plan',
      'book'
    )
    // No mismatches → empty list forwarded.
    await tick()
    expect(onMismatched).toHaveBeenCalledWith([])
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

  it('shows an "Unrecognized type" message + remove affordance for a bogus type ref', async () => {
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

    // Open the type menu and clear the bogus ref via the same Remove control.
    await fireEvent.click(
      screen.getByRole('button', { name: /Assign a type/i })
    )
    await fireEvent.click(
      screen.getByRole('menuitem', { name: /Remove type/i })
    )
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
