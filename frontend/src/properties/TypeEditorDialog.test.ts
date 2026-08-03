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
    SaveType: vi.fn().mockResolvedValue(undefined)
  })
)
vi.mock('$silt-app', () => appMocks)

import TypeEditorDialog from './TypeEditorDialog.svelte'

beforeEach(() => {
  appMocks.SaveType.mockReset().mockResolvedValue(undefined)
})
afterEach(cleanup)

// Helpers ---------------------------------------------------------------------
// The dialog seeds one blank property row on open; most tests want to type
// into it. Identity inputs are queried by ID — the wrapping <label>text</label>
// includes the asterisk glyph in its accessible name, so getByLabelText would
// need a regex; IDs are stable and pin the contract more directly.
async function open() {
  const onClose = vi.fn()
  render(TypeEditorDialog, { props: { open: true, onClose } })
  await tick()
  return { onClose }
}

function nameInput(): HTMLInputElement {
  return document.getElementById('te-name') as HTMLInputElement
}
function descInput(): HTMLInputElement {
  return document.getElementById('te-desc') as HTMLInputElement
}
function iconInput(): HTMLInputElement {
  return document.getElementById('te-icon') as HTMLInputElement
}
function heroSelect(): HTMLSelectElement {
  return document.getElementById('te-hero') as HTMLSelectElement
}

async function setName(value: string): Promise<void> {
  await fireEvent.input(nameInput(), { target: { value } })
}

// Book example type, recreated field-by-field via the editor. Pins the
// acceptance criterion: "a user can recreate the Book example type from
// scratch using only this dialog."
const BOOK_EXPECTED = {
  id: '',
  name: 'Book',
  description: 'Reading notes with author, status, and rating',
  icon: 'menu_book',
  heroField: 'title',
  properties: [
    { name: 'title', type: 'text', required: true },
    { name: 'author', type: 'text' },
    {
      name: 'status',
      type: 'select',
      options: ['todo', 'reading', 'done'],
      default: 'todo'
    },
    { name: 'rating', type: 'number', min: 0, max: 5 },
    { name: 'finished', type: 'date' }
  ]
}

describe('TypeEditorDialog — rendering & a11y', () => {
  it('renders as a blocking modal (role=dialog, aria-modal=true) when open', async () => {
    await open()
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(dialog).toHaveAttribute('aria-labelledby', 'type-editor-title')
    expect(dialog.getAttribute('tabindex')).toBe('-1')
  })

  it('does not render when closed', () => {
    render(TypeEditorDialog, {
      props: { open: false, onClose: () => {} }
    })
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('seeds one blank property row so the list is not a dead-end', async () => {
    await open()
    // The seeded row's name input is reachable by its aria-label.
    expect(screen.getByLabelText('Property 1 name')).toBeInTheDocument()
  })

  it('exposes labelled inputs for every identity field', async () => {
    await open()
    // Each identity input has a stable id and an associated <label for=…>.
    for (const id of ['te-name', 'te-desc', 'te-icon', 'te-hero']) {
      const el = document.getElementById(id)
      expect(el).not.toBeNull()
      // The label association is the a11y contract — input.id must match a
      // label[for].
      const lbl = document.querySelector(`label[for="${id}"]`)
      expect(lbl).not.toBeNull()
    }
  })
})

describe('TypeEditorDialog — property row editor', () => {
  it('lists all 9 property types in each row type select', async () => {
    await open()
    const typeSelect = screen.getByLabelText('Property 1 type')
    // The closed <select> exposes its options via the listbox role once
    // expanded, but the option elements are queryable from the select too.
    const options = typeSelect.querySelectorAll('option')
    const values = Array.from(options).map((o) => o.getAttribute('value'))
    expect(values).toEqual([
      'text',
      'number',
      'date',
      'datetime',
      'checkbox',
      'select',
      'multiselect',
      'page',
      'pages'
    ])
  })

  it('shows the options input when type is select', async () => {
    await open()
    await fireEvent.change(screen.getByLabelText('Property 1 type'), {
      target: { value: 'select' }
    })
    // The Options label wraps a hint span, so query the input by ID.
    const opts = document.getElementById('te-opts-1') as HTMLInputElement
    expect(opts).toBeInTheDocument()
    // select requires options → aria-required is set on the input.
    expect(opts).toHaveAttribute('aria-required', 'true')
  })

  it('shows the options input for multiselect but NOT marked required', async () => {
    await open()
    await fireEvent.change(screen.getByLabelText('Property 1 type'), {
      target: { value: 'multiselect' }
    })
    const opts = document.getElementById('te-opts-1') as HTMLInputElement
    expect(opts).toBeInTheDocument()
    expect(opts).not.toHaveAttribute('aria-required')
  })

  it('shows min/max inputs when type is number', async () => {
    await open()
    await fireEvent.change(screen.getByLabelText('Property 1 type'), {
      target: { value: 'number' }
    })
    expect(screen.getByLabelText('Min')).toBeInTheDocument()
    expect(screen.getByLabelText('Max')).toBeInTheDocument()
  })

  it('hides options when switching type away from select/multiselect', async () => {
    await open()
    await fireEvent.change(screen.getByLabelText('Property 1 type'), {
      target: { value: 'select' }
    })
    await fireEvent.input(
      document.getElementById('te-opts-1') as HTMLInputElement,
      { target: { value: 'a, b, c' } }
    )
    // Switch to text — the options input disappears so the row state stays
    // clean (no orphaned optionsText leaks into the assembled TypeDef).
    await fireEvent.change(screen.getByLabelText('Property 1 type'), {
      target: { value: 'text' }
    })
    expect(document.getElementById('te-opts-1')).toBeNull()
  })

  it('"Add property" appends a new blank row with a fresh rowId', async () => {
    await open()
    await fireEvent.click(screen.getByRole('button', { name: /Add property/ }))
    // Two property-name inputs now exist (aria-labels carry the row index).
    expect(screen.getByLabelText('Property 1 name')).toBeInTheDocument()
    expect(screen.getByLabelText('Property 2 name')).toBeInTheDocument()
  })

  it('the row remove button drops the row', async () => {
    await open()
    await fireEvent.click(screen.getByRole('button', { name: /Add property/ }))
    expect(screen.getByLabelText('Property 2 name')).toBeInTheDocument()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Remove property 2' })
    )
    expect(screen.queryByLabelText('Property 2 name')).toBeNull()
  })

  it('heroField select lists only properties with a name', async () => {
    await open()
    // Seed one blank row → no named property yet → heroField only has None.
    await fireEvent.click(screen.getByRole('button', { name: /Add property/ }))
    await fireEvent.input(screen.getByLabelText('Property 1 name'), {
      target: { value: 'title' }
    })
    const hero = heroSelect()
    const heroOptions = Array.from(hero.options).map((o) => o.value)
    expect(heroOptions).toContain('title')
  })
})

describe('TypeEditorDialog — validation', () => {
  it('blocks save and surfaces an error when the type name is empty', async () => {
    const { onClose } = await open()
    await fireEvent.click(screen.getByRole('button', { name: 'Create type' }))
    await tick()
    expect(appMocks.SaveType).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
    // The error is reachable as an aria-live alert.
    const alerts = screen.getAllByRole('alert')
    expect(
      alerts.some((el) => /name is required/i.test(el.textContent ?? ''))
    ).toBe(true)
  })

  it('blocks save when a select property has no options', async () => {
    await open()
    await setName('Widget')
    await fireEvent.input(screen.getByLabelText('Property 1 name'), {
      target: { value: 'status' }
    })
    await fireEvent.change(screen.getByLabelText('Property 1 type'), {
      target: { value: 'select' }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Create type' }))
    await tick()
    expect(appMocks.SaveType).not.toHaveBeenCalled()
    expect(
      screen
        .getAllByRole('alert')
        .some((el) => /at least one option/i.test(el.textContent ?? ''))
    ).toBe(true)
  })

  it('blocks save when a property name is invalid (uppercase)', async () => {
    await open()
    await setName('Widget')
    await fireEvent.input(screen.getByLabelText('Property 1 name'), {
      target: { value: 'BadName' }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Create type' }))
    await tick()
    expect(appMocks.SaveType).not.toHaveBeenCalled()
    expect(
      screen
        .getAllByRole('alert')
        .some((el) => /must be lowercase/i.test(el.textContent ?? ''))
    ).toBe(true)
  })

  it('blocks save for a reserved property name (e.g. "page")', async () => {
    await open()
    await setName('Widget')
    await fireEvent.input(screen.getByLabelText('Property 1 name'), {
      target: { value: 'page' }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Create type' }))
    await tick()
    expect(appMocks.SaveType).not.toHaveBeenCalled()
    expect(
      screen
        .getAllByRole('alert')
        .some((el) => /reserved/i.test(el.textContent ?? ''))
    ).toBe(true)
  })

  it('does not show validation errors before the first Save attempt', async () => {
    await open()
    // No alerts before any save attempt.
    expect(screen.queryAllByRole('alert')).toHaveLength(0)
  })
})

describe('TypeEditorDialog — SaveType contract', () => {
  it('recreates the Book example type end-to-end', async () => {
    const { onClose } = await open()

    // Identity
    await fireEvent.input(nameInput(), {
      target: { value: 'Book' }
    })
    await fireEvent.input(descInput(), {
      target: { value: 'Reading notes with author, status, and rating' }
    })
    await fireEvent.input(iconInput(), {
      target: { value: 'menu_book' }
    })

    // Row 1: title (text, required) — defaults to text, no type change needed.
    await fireEvent.input(screen.getByLabelText('Property 1 name'), {
      target: { value: 'title' }
    })
    await fireEvent.click(screen.getByLabelText('Property 1 required'))

    // Row 2: author (text)
    await fireEvent.click(screen.getByRole('button', { name: /Add property/ }))
    await fireEvent.input(screen.getByLabelText('Property 2 name'), {
      target: { value: 'author' }
    })

    // Row 3: status (select with options + default)
    await fireEvent.click(screen.getByRole('button', { name: /Add property/ }))
    await fireEvent.input(screen.getByLabelText('Property 3 name'), {
      target: { value: 'status' }
    })
    await fireEvent.change(screen.getByLabelText('Property 3 type'), {
      target: { value: 'select' }
    })
    // Options / Default labels carry inline hint spans (so getByLabelText
    // over their full accessible name is brittle) — query by ID instead.
    await fireEvent.input(
      document.getElementById('te-opts-3') as HTMLInputElement,
      { target: { value: 'todo, reading, done' } }
    )
    // Default for select is itself a select dropdown of the parsed options.
    await fireEvent.change(
      document.getElementById('te-def-3') as HTMLSelectElement,
      { target: { value: 'todo' } }
    )

    // Row 4: rating (number with min/max)
    await fireEvent.click(screen.getByRole('button', { name: /Add property/ }))
    await fireEvent.input(screen.getByLabelText('Property 4 name'), {
      target: { value: 'rating' }
    })
    await fireEvent.change(screen.getByLabelText('Property 4 type'), {
      target: { value: 'number' }
    })
    await fireEvent.input(screen.getByLabelText('Min'), {
      target: { value: '0' }
    })
    await fireEvent.input(screen.getByLabelText('Max'), {
      target: { value: '5' }
    })

    // Row 5: finished (date)
    await fireEvent.click(screen.getByRole('button', { name: /Add property/ }))
    await fireEvent.input(screen.getByLabelText('Property 5 name'), {
      target: { value: 'finished' }
    })
    await fireEvent.change(screen.getByLabelText('Property 5 type'), {
      target: { value: 'date' }
    })

    // Pick title as the heroField.
    await fireEvent.change(heroSelect(), {
      target: { value: 'title' }
    })

    // Save
    await fireEvent.click(screen.getByRole('button', { name: 'Create type' }))
    await waitFor(() => expect(appMocks.SaveType).toHaveBeenCalledTimes(1))
    const td = appMocks.SaveType.mock.calls[0][0]

    // Contract assertions: id is omitted (server derives), top-level fields
    // round-trip, and each property's wire shape matches the Book seed.
    expect(td.id).toBe('')
    expect(td.name).toBe(BOOK_EXPECTED.name)
    expect(td.description).toBe(BOOK_EXPECTED.description)
    expect(td.icon).toBe(BOOK_EXPECTED.icon)
    expect(td.heroField).toBe(BOOK_EXPECTED.heroField)
    expect(td.properties.map((p: { name: string }) => p.name)).toEqual(
      BOOK_EXPECTED.properties.map((p) => p.name)
    )
    // required flag set only on title.
    const titleProp = td.properties.find(
      (p: { name: string }) => p.name === 'title'
    )
    expect(titleProp.required).toBe(true)
    const authorProp = td.properties.find(
      (p: { name: string }) => p.name === 'author'
    )
    expect(authorProp.required).toBeFalsy()
    // status carries options + default.
    const statusProp = td.properties.find(
      (p: { name: string }) => p.name === 'status'
    )
    expect(statusProp.options).toEqual(['todo', 'reading', 'done'])
    expect(statusProp.default).toBe('todo')
    // rating carries numeric min/max.
    const ratingProp = td.properties.find(
      (p: { name: string }) => p.name === 'rating'
    )
    expect(ratingProp.min).toBe(0)
    expect(ratingProp.max).toBe(5)
    // finished is a plain date property with no extras.
    const finishedProp = td.properties.find(
      (p: { name: string }) => p.name === 'finished'
    )
    expect(finishedProp.type).toBe('date')

    // Success closes the dialog.
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  })

  it('omits description/icon/heroField when left blank', async () => {
    await open()
    await setName('Plain')
    await fireEvent.input(screen.getByLabelText('Property 1 name'), {
      target: { value: 'a' }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Create type' }))
    await waitFor(() => expect(appMocks.SaveType).toHaveBeenCalledTimes(1))
    const td = appMocks.SaveType.mock.calls[0][0]
    expect(td.description).toBeFalsy()
    expect(td.icon).toBeFalsy()
    expect(td.heroField).toBeFalsy()
  })

  it('omits a number propertys min/max when left blank', async () => {
    await open()
    await setName('Plain')
    await fireEvent.input(screen.getByLabelText('Property 1 name'), {
      target: { value: 'count' }
    })
    await fireEvent.change(screen.getByLabelText('Property 1 type'), {
      target: { value: 'number' }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Create type' }))
    await waitFor(() => expect(appMocks.SaveType).toHaveBeenCalledTimes(1))
    const td = appMocks.SaveType.mock.calls[0][0]
    const countProp = td.properties.find(
      (p: { name: string }) => p.name === 'count'
    )
    expect(countProp.min).toBeFalsy()
    expect(countProp.max).toBeFalsy()
  })

  it('emits a checkbox default as a boolean', async () => {
    await open()
    await setName('Plain')
    await fireEvent.input(screen.getByLabelText('Property 1 name'), {
      target: { value: 'flag' }
    })
    await fireEvent.change(screen.getByLabelText('Property 1 type'), {
      target: { value: 'checkbox' }
    })
    await fireEvent.click(screen.getByLabelText('default on'))
    await fireEvent.click(screen.getByRole('button', { name: 'Create type' }))
    await waitFor(() => expect(appMocks.SaveType).toHaveBeenCalledTimes(1))
    const td = appMocks.SaveType.mock.calls[0][0]
    const flagProp = td.properties.find(
      (p: { name: string }) => p.name === 'flag'
    )
    expect(flagProp.default).toBe(true)
  })

  it('surfaces a SaveType rejection inline (aria-live alert)', async () => {
    appMocks.SaveType.mockRejectedValueOnce(
      new Error('id "book" already exists')
    )
    await open()
    await setName('Book')
    await fireEvent.input(screen.getByLabelText('Property 1 name'), {
      target: { value: 'title' }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Create type' }))
    await waitFor(() => {
      expect(
        screen
          .getAllByRole('alert')
          .some((el) => /already exists/i.test(el.textContent ?? ''))
      ).toBe(true)
    })
  })

  it('disables the Save button while a save is in flight', async () => {
    let releaseSave: () => void = () => {}
    appMocks.SaveType.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          releaseSave = resolve
        })
    )
    await open()
    await setName('Book')
    await fireEvent.input(screen.getByLabelText('Property 1 name'), {
      target: { value: 'title' }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Create type' }))
    await tick()
    // While in flight the button shows the saving state and is disabled.
    const saveBtn = screen.getByRole('button', { name: /Saving/ })
    expect(saveBtn).toBeDisabled()
    releaseSave()
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: /Saving/ })).toBeNull()
    )
  })
})

describe('TypeEditorDialog — dismiss behaviour', () => {
  it('Escape closes without saving', async () => {
    const { onClose } = await open()
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledOnce()
    expect(appMocks.SaveType).not.toHaveBeenCalled()
  })

  it('overlay click closes without saving', async () => {
    const { onClose } = await open()
    // The backdrop button (te-backdrop) is the overlay click surface. It's
    // labelled "Cancel" for AT, same as the footer button, so disambiguate
    // by class.
    const backdrop = document.querySelector('.te-backdrop') as HTMLButtonElement
    expect(backdrop).not.toBeNull()
    await fireEvent.click(backdrop)
    expect(onClose).toHaveBeenCalledOnce()
    expect(appMocks.SaveType).not.toHaveBeenCalled()
  })

  it('the footer Cancel button closes without saving', async () => {
    const { onClose } = await open()
    // Footer Cancel is the second button labelled "Cancel" — the backdrop
    // button is aria-hidden to sighted users but labelled "Cancel" too, so
    // disambiguate by getting the visible button in the footer region.
    const cancelButtons = screen.getAllByRole('button', { name: 'Cancel' })
    // Pick the visible one (the backdrop is tabindex=-1 but still a button).
    const visible =
      cancelButtons.find((b) => b.tabIndex !== -1) ?? cancelButtons[0]
    await fireEvent.click(visible)
    expect(onClose).toHaveBeenCalledOnce()
    expect(appMocks.SaveType).not.toHaveBeenCalled()
  })
})

describe('TypeEditorDialog — resets between opens', () => {
  it('a second open starts with a fresh form (no leaked draft)', async () => {
    const { unmount } = render(TypeEditorDialog, {
      props: { open: true, onClose: () => {} }
    })
    await tick()
    await setName('First')
    await fireEvent.input(screen.getByLabelText('Property 1 name'), {
      target: { value: 'first_prop' }
    })
    unmount()

    render(TypeEditorDialog, {
      props: { open: true, onClose: () => {} }
    })
    await tick()
    // The name input is empty again.
    expect(nameInput().value).toBe('')
    // One blank property row.
    expect(screen.getByLabelText('Property 1 name')).toBeInTheDocument()
    expect(screen.queryByLabelText('Property 2 name')).toBeNull()
    // The seeded row's name is empty.
    expect(
      (screen.getByLabelText('Property 1 name') as HTMLInputElement).value
    ).toBe('')
  })
})
