import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'

// The nav tree lives inside the hoisted factory — vi.hoisted runs before any
// module-level const is initialized, so the mock's resolved value must be in
// the same hoisted scope.
const appMocks = vi.hoisted(() => {
  const navTree = {
    notebooks: [
      {
        name: 'Work',
        source: 'vault',
        sections: [
          {
            name: 'People',
            path: 'People',
            pages: [
              { name: 'Alice', count: 0 },
              { name: 'Bob', count: 0 }
            ],
            children: []
          },
          {
            name: 'Projects',
            path: 'Projects',
            pages: [{ name: 'Dune', count: 0 }],
            children: []
          }
        ]
      }
    ]
  }
  return createAppIpcMocks({
    navTree,
    ListNavigation: vi.fn(),
    QueryPagesByType: vi.fn().mockResolvedValue([])
  })
})
vi.mock('$silt-app', () => appMocks)

import PageLinkField from './PageLinkField.svelte'

beforeEach(() => {
  appMocks.ListNavigation.mockReset().mockResolvedValue(appMocks.navTree)
  appMocks.QueryPagesByType.mockReset().mockResolvedValue([])
})

afterEach(cleanup)

async function focusInput() {
  const input = screen.getByRole('combobox')
  await fireEvent.focus(input)
  // ListNavigation resolves + the dropdown opens.
  await waitFor(() => {
    expect(appMocks.ListNavigation).toHaveBeenCalled()
  })
  await tick()
  return input
}

describe('PageLinkField — single (page)', () => {
  it('renders a combobox input', () => {
    render(PageLinkField, {
      props: {
        value: '',
        multi: false,
        label: 'Author',
        fieldId: 'f-author',
        onCommit: vi.fn()
      }
    })
    expect(screen.getByRole('combobox', { name: 'Author' })).toBeInTheDocument()
  })

  it('shows matching pages on focus and stores the ref on select', async () => {
    const onCommit = vi.fn()
    render(PageLinkField, {
      props: {
        value: '',
        multi: false,
        label: 'Author',
        fieldId: 'f-author',
        onCommit
      }
    })
    await focusInput()
    // All three pages appear (capped at 10).
    expect(screen.getByRole('option', { name: /Alice/i })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Bob/i })).toBeInTheDocument()

    await fireEvent.click(screen.getByRole('option', { name: /Alice/i }))
    // Stored as the full canonical ref notebook / section / page.
    expect(onCommit).toHaveBeenCalledWith('Work/People/Alice')
  })

  it('filters the dropdown by the typed query', async () => {
    render(PageLinkField, {
      props: {
        value: '',
        multi: false,
        label: 'Author',
        fieldId: 'f-author',
        onCommit: vi.fn()
      }
    })
    const input = await focusInput()
    await fireEvent.input(input, { target: { value: 'ali' } })
    await tick()
    expect(screen.getByRole('option', { name: /Alice/i })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /Bob/i })).toBeNull()
  })

  it('keyboard nav: Enter selects the active option; ArrowDown moves it', async () => {
    const onCommit = vi.fn()
    render(PageLinkField, {
      props: {
        value: '',
        multi: false,
        label: 'Author',
        fieldId: 'f-author',
        onCommit
      }
    })
    const input = await focusInput()
    // After focus the first option (Alice) is active; Enter selects it.
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(onCommit).toHaveBeenCalledWith('Work/People/Alice')
    // ArrowDown moves the highlight to the next option (Bob).
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(input).toHaveAttribute(
      'aria-activedescendant',
      expect.stringContaining('opt-1')
    )
  })

  it('Escape closes the dropdown without committing', async () => {
    render(PageLinkField, {
      props: {
        value: '',
        multi: false,
        label: 'Author',
        fieldId: 'f-author',
        onCommit: vi.fn()
      }
    })
    const input = await focusInput()
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    await fireEvent.keyDown(input, { key: 'Escape' })
    await tick()
    expect(screen.queryByRole('listbox')).toBeNull()
  })

  it('renders a dangling value with a strikethrough chip once the list loads', async () => {
    render(PageLinkField, {
      props: {
        value: 'Work/People/Ghost',
        multi: false,
        label: 'Author',
        fieldId: 'f-author',
        onCommit: vi.fn()
      }
    })
    await focusInput()
    // Ghost is not in the nav list → dangling indicator present.
    expect(screen.getByText('Ghost')).toBeInTheDocument()
    expect(screen.getByText('Ghost').closest('.chip, .dangling')).toBeTruthy()
  })
})

describe('PageLinkField — multi (pages)', () => {
  it('renders selected pages as removable chips', () => {
    const onCommit = vi.fn()
    render(PageLinkField, {
      props: {
        value: ['Work/People/Alice'],
        multi: true,
        label: 'Authors',
        fieldId: 'f-authors',
        onCommit
      }
    })
    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Remove Alice' })
    ).toBeInTheDocument()
  })

  it('adds a chip on select and keeps the picker open', async () => {
    const onCommit = vi.fn()
    render(PageLinkField, {
      props: {
        value: ['Work/People/Alice'],
        multi: true,
        label: 'Authors',
        fieldId: 'f-authors',
        onCommit
      }
    })
    await focusInput()
    // Alice is already selected → not in the dropdown. Pick Bob.
    await fireEvent.click(screen.getByRole('option', { name: /Bob/i }))
    expect(onCommit).toHaveBeenCalledWith([
      'Work/People/Alice',
      'Work/People/Bob'
    ])
  })

  it('removing a chip commits the reduced list', async () => {
    const onCommit = vi.fn()
    render(PageLinkField, {
      props: {
        value: ['Work/People/Alice', 'Work/People/Bob'],
        multi: true,
        label: 'Authors',
        fieldId: 'f-authors',
        onCommit
      }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Remove Alice' }))
    expect(onCommit).toHaveBeenCalledWith(['Work/People/Bob'])
  })

  it('Backspace on an empty input removes the last chip', async () => {
    const onCommit = vi.fn()
    render(PageLinkField, {
      props: {
        value: ['Work/People/Alice', 'Work/People/Bob'],
        multi: true,
        label: 'Authors',
        fieldId: 'f-authors',
        onCommit
      }
    })
    const input = screen.getByRole('combobox')
    await fireEvent.keyDown(input, { key: 'Backspace' })
    expect(onCommit).toHaveBeenCalledWith(['Work/People/Alice'])
  })
})
