import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'

// Mock the Wails app bindings. PropertiesPanel itself does not call IPC for
// the Core section (it delegates to the host's onCommitCore prop), but
// CoreMetadataSection's commit path is exercised through that callback. The
// mocks stay because PropertiesPanel's SetPageType / TurnIntoPage paths still
// need them.
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
  appMocks.GetPageProperties.mockReset().mockResolvedValue([])
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

  it('surfaces aria-live banner when onCommitCore rejects', async () => {
    const onCommitCore = vi.fn().mockRejectedValue(new Error('disk full'))
    render(PropertiesPanel, {
      props: baseProps({
        info: untypedInfo,
        core: makeCore(),
        onCommitCore
      })
    })
    const dateInput = document.getElementById('core-date') as HTMLInputElement
    await fireEvent.change(dateInput, { target: { value: '2026-09-01' } })
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert').textContent).toMatch(/disk full/i)
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
