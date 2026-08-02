import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'

// Schemas for the conversion matrix. The OLD type ("book") has title(text),
// rating(number), and tags(multiselect). The NEW type ("film") reuses title
// (auto), reuses rating but as text (coerced), reuses tags but as number
// (flagged), drops `series` (orphaned), and adds `director` (new).
const OLD_TYPE = {
  id: 'book',
  name: 'Book',
  properties: [
    { name: 'title', label: 'Title', type: 'text' },
    { name: 'rating', label: 'Rating', type: 'number' },
    { name: 'tags', label: 'Tags', type: 'multiselect' },
    { name: 'series', label: 'Series', type: 'text' }
  ]
}
const NEW_TYPE = {
  id: 'film',
  name: 'Film',
  properties: [
    { name: 'title', label: 'Title', type: 'text' },
    { name: 'rating', label: 'Rating', type: 'text' },
    { name: 'tags', label: 'Tags', type: 'number' },
    { name: 'director', label: 'Director', type: 'text' }
  ]
}
// Current values on the page (old schema order; only isSet ones map).
const CURRENT_VALUES = [
  {
    name: 'title',
    label: 'Title',
    type: 'text',
    value: 'Dune',
    isSet: true,
    required: false
  },
  {
    name: 'rating',
    label: 'Rating',
    type: 'number',
    value: 5,
    isSet: true,
    required: false
  },
  {
    name: 'tags',
    label: 'Tags',
    type: 'multiselect',
    value: ['sci-fi'],
    isSet: true,
    required: false
  },
  {
    name: 'series',
    label: 'Series',
    type: 'text',
    value: '',
    isSet: false,
    required: false
  }
]

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    GetType: vi.fn(),
    GetPageProperties: vi.fn()
  })
)
vi.mock('$silt-app', () => appMocks)

import TurnIntoDialog from './TurnIntoDialog.svelte'

const locator = { notebook: 'Work', section: 'Reading', page: 'Dune' }

function resolveType(id: string): typeof OLD_TYPE | null {
  if (id === 'book') return OLD_TYPE
  if (id === 'film') return NEW_TYPE
  return null
}

beforeEach(() => {
  appMocks.GetType.mockReset().mockImplementation((id: string) =>
    Promise.resolve(resolveType(id))
  )
  appMocks.GetPageProperties.mockReset().mockResolvedValue(CURRENT_VALUES)
})

afterEach(cleanup)

async function mountOpen(props: Record<string, unknown> = {}) {
  const onConfirm = vi.fn()
  const onCancel = vi.fn()
  render(TurnIntoDialog, {
    props: {
      open: true,
      locator,
      oldTypeId: 'book',
      newTypeId: 'film',
      newTypeLabel: 'Film',
      onConfirm,
      onCancel,
      ...props
    }
  })
  // Wait for the three fetches to resolve + the matrix to render.
  await waitFor(() => {
    expect(
      screen.getAllByText(
        /Carries over|Compatible|Will be flagged|Won't appear/
      ).length
    ).toBeGreaterThan(0)
  })
  await tick()
  return { onConfirm, onCancel }
}

describe('TurnIntoDialog', () => {
  it('renders the mapping matrix with the right classifications', async () => {
    await mountOpen()
    // title: text→text → Carries over (auto).
    expect(screen.getByText('Carries over')).toBeInTheDocument()
    // rating: number→text → Compatible (coerced).
    expect(screen.getByText('Compatible')).toBeInTheDocument()
    // tags: multiselect→number → Will be flagged.
    expect(screen.getByText('Will be flagged')).toBeInTheDocument()
    // series is unset (isSet:false) → not in the matrix as a value row.
    // director: new property → New (empty).
    expect(screen.getByText('New (empty)')).toBeInTheDocument()
  })

  it('confirm calls onConfirm with the orphan list + the clear flag', async () => {
    // series is unset here; no orphans in the default fixture. Use a fixture
    // where `series` IS set so it becomes an orphan against `film`.
    appMocks.GetPageProperties.mockResolvedValue([
      ...CURRENT_VALUES,
      {
        name: 'series',
        label: 'Series',
        type: 'text',
        value: 'Dune Chronicles',
        isSet: true,
        required: false
      }
    ])
    const { onConfirm } = await mountOpen()
    // Default: clear-orphaned unchecked.
    await fireEvent.click(
      screen.getByRole('button', { name: 'Turn into Film' })
    )
    expect(onConfirm).toHaveBeenCalledWith(['series'], false)
  })

  it('checking "clear orphaned" forwards true', async () => {
    appMocks.GetPageProperties.mockResolvedValue([
      ...CURRENT_VALUES,
      {
        name: 'series',
        label: 'Series',
        type: 'text',
        value: 'Dune Chronicles',
        isSet: true,
        required: false
      }
    ])
    const { onConfirm } = await mountOpen()
    const checkbox = screen.getByRole('checkbox')
    await fireEvent.click(checkbox)
    await fireEvent.click(
      screen.getByRole('button', { name: 'Turn into Film' })
    )
    expect(onConfirm).toHaveBeenCalledWith(['series'], true)
  })

  it('cancel does not call onConfirm', async () => {
    const { onConfirm, onCancel } = await mountOpen()
    // The backdrop and footer both expose Cancel; click the footer button.
    const cancels = screen.getAllByRole('button', { name: 'Cancel' })
    await fireEvent.click(cancels[cancels.length - 1])
    expect(onConfirm).not.toHaveBeenCalled()
    expect(onCancel).toHaveBeenCalled()
  })

  it('Escape cancels', async () => {
    const { onCancel } = await mountOpen()
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onCancel).toHaveBeenCalled()
  })

  it('is a blocking modal (aria-modal=true) with the type in the title', async () => {
    await mountOpen()
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(dialog.getAttribute('aria-labelledby')).toBeTruthy()
  })

  it('clearing the type marks every value as orphaned', async () => {
    appMocks.GetPageProperties.mockResolvedValue([
      {
        name: 'title',
        label: 'Title',
        type: 'text',
        value: 'Dune',
        isSet: true,
        required: false
      }
    ])
    await mountOpen({ newTypeId: '', newTypeLabel: 'No type' })
    expect(screen.getAllByText("Won't appear").length).toBeGreaterThan(0)
    expect(
      screen.getByRole('button', { name: 'Remove type' })
    ).toBeInTheDocument()
  })
})
