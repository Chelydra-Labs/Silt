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
  // A second tree with a linked notebook sharing notebook/section/page names
  // with the vault — used to verify source disambiguation (NB-6). Kept separate
  // from the default tree so the shared tests don't hit duplicate each-keys.
  const navTreeWithLinkedDupe = {
    notebooks: [
      ...navTree.notebooks,
      {
        name: 'Work',
        source: 'linked-repo',
        sections: [
          {
            name: 'People',
            path: 'People',
            pages: [{ name: 'Alice', count: 0 }],
            children: []
          }
        ]
      }
    ]
  }
  return createAppIpcMocks({
    navTree,
    navTreeWithLinkedDupe,
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

describe('PageLinkField — source disambiguation + ref normalization (NB-6)', () => {
  it('a target filter narrows by source so a same-named linked page does not collide', async () => {
    // Swap in a tree where a linked notebook shares notebook/section/page
    // names with the vault — without source in the match key, both Alices
    // would leak through the target filter.
    appMocks.ListNavigation.mockResolvedValue(appMocks.navTreeWithLinkedDupe)
    // Only the linked-repo's Alice is of the target type.
    appMocks.QueryPagesByType.mockResolvedValue([
      {
        source: 'linked-repo',
        notebook: 'Work',
        section: 'People',
        page: 'Alice'
      }
    ])
    render(PageLinkField, {
      props: {
        value: '',
        multi: false,
        target: 'person',
        label: 'Author',
        fieldId: 'f-author',
        onCommit: vi.fn()
      }
    })
    await focusInput()
    // Only the linked-repo Alice is eligible — the vault's Alice (same
    // notebook/section/page names) is filtered out by source.
    const options = screen.getAllByRole('option', { name: /Alice/i })
    expect(options).toHaveLength(1)
    // Bob is not of the target type, so he must not appear either.
    expect(screen.queryByRole('option', { name: /Bob/i })).toBeNull()
  })

  it('an already-selected bare-name ref is excluded from the dropdown (no duplicate add)', async () => {
    // The stored value is a bare leaf name "Alice" (as MCP might write),
    // not the full canonical ref "Work/People/Alice".
    render(PageLinkField, {
      props: {
        value: ['Alice'],
        multi: true,
        label: 'Authors',
        fieldId: 'f-authors',
        onCommit: vi.fn()
      }
    })
    await focusInput()
    // Alice is already linked (by bare name) — she must not be offered again.
    expect(screen.queryByRole('option', { name: /Alice/i })).toBeNull()
    // Other pages still appear.
    expect(screen.getByRole('option', { name: /Bob/i })).toBeInTheDocument()
  })
})

describe('PageLinkField — Popover-portaled listbox (escapes overflow)', () => {
  it('portals the listbox to document.body so it is not clipped by ancestor overflow', async () => {
    // Mirrors DependencyPicker's Popover body-ancestry test (#376). jsdom
    // cannot prove no-clip (no real layout), but portal-to-body is the
    // deterministic structural fix: once the listbox lives under body, no
    // PropertiesPanel `.fields{overflow-y:auto}` ancestor can clip it.
    const { container } = render(PageLinkField, {
      props: {
        value: '',
        multi: false,
        label: 'Author',
        fieldId: 'f-author',
        onCommit: vi.fn()
      }
    })
    await focusInput()

    const listbox = screen.getByRole('listbox')
    expect(document.body.contains(listbox)).toBe(true)
    expect(container.contains(listbox)).toBe(false)
    // Capped option set still fully rendered (scroll is the ul's job).
    expect(screen.getAllByRole('option').length).toBeGreaterThan(0)
  })

  it('renders the full capped option set for a long result list', async () => {
    const manyPages = Array.from({ length: 14 }, (_, i) => ({
      name: `Page ${i}`,
      count: 0
    }))
    appMocks.ListNavigation.mockResolvedValue({
      notebooks: [
        {
          name: 'Work',
          source: 'vault',
          sections: [
            {
              name: 'Big',
              path: 'Big',
              pages: manyPages,
              children: []
            }
          ]
        }
      ]
    })
    render(PageLinkField, {
      props: {
        value: '',
        multi: false,
        label: 'Link',
        fieldId: 'f-link',
        onCommit: vi.fn()
      }
    })
    await focusInput()

    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(10)
    expect(options[0]).toHaveTextContent('Page 0')
    expect(options[9]).toHaveTextContent('Page 9')
  })

  it('preserves keyboard nav + selection through the Popover-hosted listbox', async () => {
    // Regression guard: moving the listbox into <Popover> must not break the
    // combobox's arrow/enter semantics (aria-activedescendant on the input).
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
    expect(input).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    // Arrow nav still cycles the active option through the input.
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(input).toHaveAttribute(
      'aria-activedescendant',
      expect.stringContaining('opt-1')
    )
    // Enter still commits the active option.
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(onCommit).toHaveBeenCalledWith('Work/People/Bob')
  })
})
