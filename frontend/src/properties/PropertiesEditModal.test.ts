import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
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

import PropertiesEditModal from './PropertiesEditModal.svelte'
import type { PagePropertyValue, PageTypeInfo } from './types'

const locator = { notebook: 'Work', section: 'Projects', page: 'Plan' }
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

describe('PropertiesEditModal', () => {
  it('renders as a blocking dialog (aria-modal=true) when open', () => {
    render(PropertiesEditModal, { props: baseProps() })
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(dialog).toHaveAttribute('aria-label', 'Edit page properties')
  })

  it('does not render when closed', () => {
    render(PropertiesEditModal, { props: baseProps({ open: false }) })
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('moves focus to the type <select> on open (first editable control)', async () => {
    render(PropertiesEditModal, {
      props: baseProps({ types: [{ id: 'book', name: 'Book' }] })
    })
    const select = screen.getByRole('combobox', { name: 'Page type' })
    await waitFor(() => {
      expect(document.activeElement).toBe(select)
    })
  })

  it('closes on Escape', async () => {
    const onClose = vi.fn()
    render(PropertiesEditModal, { props: baseProps({ onClose }) })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes on backdrop click', async () => {
    const onClose = vi.fn()
    render(PropertiesEditModal, { props: baseProps({ onClose }) })
    await fireEvent.click(screen.getByTestId('modal-backdrop'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes on the header close button', async () => {
    const onClose = vi.fn()
    render(PropertiesEditModal, { props: baseProps({ onClose }) })
    // The header close button is the visible one (the backdrop sentinel is
    // tabindex=-1). Both share an accessible name; the header button is the
    // second match — click the last one.
    const closes = screen.getAllByRole('button', {
      name: 'Close edit properties'
    })
    await fireEvent.click(closes[closes.length - 1])
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('renders the shared field controls (reuses PropertiesBody)', () => {
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
    render(PropertiesEditModal, { props: baseProps({ values }) })
    expect(screen.getByLabelText('Title')).toBeInTheDocument()
  })

  it('commits a field edit through the write-through path (SetPageProperty)', async () => {
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
    render(PropertiesEditModal, { props: baseProps({ values }) })
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

  it('restores focus to the opener on close', async () => {
    // Create an opener button, focus it, then open the modal (which captures
    // it as previouslyFocused). Closing must return focus to it.
    const opener = document.createElement('button')
    opener.textContent = 'Open modal'
    opener.id = 'opener'
    document.body.appendChild(opener)
    opener.focus()
    expect(document.activeElement).toBe(opener)

    const { rerender } = render(PropertiesEditModal, {
      props: baseProps({
        types: [{ id: 'book', name: 'Book' }],
        onClose: () => {}
      })
    })
    // Modal captured the opener on open; focus moved into the modal.
    await waitFor(() => {
      expect(document.activeElement).not.toBe(opener)
    })

    await rerender(baseProps({ open: false, onClose: () => {} }))
    // Focus restored to the still-connected opener.
    await waitFor(() => {
      expect(document.activeElement).toBe(opener)
    })

    opener.remove()
  })

  it('surfaces the error banner on a rejected field write (aria-live)', async () => {
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
    render(PropertiesEditModal, { props: baseProps({ values }) })
    const input = screen.getByLabelText('Rating') as HTMLInputElement
    await fireEvent.change(input, { target: { value: '99' } })
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert').textContent).toMatch(/out of range/i)
  })
})
