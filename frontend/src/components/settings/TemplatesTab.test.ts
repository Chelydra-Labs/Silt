import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor
} from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  listTemplates: vi.fn(),
  getTemplate: vi.fn(),
  saveUserTemplate: vi.fn(),
  deleteUserTemplate: vi.fn(),
  fetchPageMarkdown: vi.fn(),
  eventsOn: vi.fn()
}))

vi.mock('../../../bindings/silt/app.js', () => ({
  ListTemplates: mocks.listTemplates,
  GetTemplate: mocks.getTemplate,
  SaveUserTemplate: mocks.saveUserTemplate,
  DeleteUserTemplate: mocks.deleteUserTemplate,
  FetchPageMarkdown: mocks.fetchPageMarkdown,
  RenderTemplate: vi.fn().mockResolvedValue('# Preview'),
  RenderTemplateBlocks: vi.fn().mockResolvedValue([]),
  CreatePageFromTemplate: vi.fn()
}))
vi.mock('@wailsio/runtime', () => ({
  Events: { On: mocks.eventsOn }
}))

import TemplatesTab from './TemplatesTab.svelte'
import { resetTemplateDraftForTests } from './templateDraftSession'
import TemplatePicker from '../../templates/TemplatePicker.svelte'
import {
  _resetForTests,
  initTemplates,
  loadTemplates,
  templatesState
} from '../../templates/store.svelte'

const summaries = [
  {
    id: 'daily',
    title: 'Daily note',
    category: 'Journal',
    source: 'builtin',
    icon: 'today'
  },
  {
    id: 'meeting',
    title: 'Meeting',
    category: 'Work',
    source: 'disk',
    icon: 'groups'
  },
  {
    id: 'plugin-plan',
    title: 'Plugin plan',
    category: 'Planning',
    source: 'plugin',
    plugin_id: 'planner'
  }
]

function template(id: string, source: string, title = id) {
  return {
    schema_version: '1',
    id,
    title,
    description: '',
    category: 'General',
    icon: 'description',
    placeholders: [],
    body: `# ${title}`,
    source,
    plugin_id: ''
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

async function setup() {
  mocks.listTemplates.mockResolvedValue({ templates: summaries })
  await loadTemplates()
  return render(TemplatesTab, {
    props: {
      activeNotebook: 'Work',
      activeSection: 'Notes',
      activePage: 'Current',
      vaultId: 'C:/Vault A'
    }
  })
}

beforeEach(() => {
  _resetForTests()
  resetTemplateDraftForTests()
  vi.clearAllMocks()
  mocks.eventsOn.mockImplementation(() => () => {})
  mocks.saveUserTemplate.mockResolvedValue(undefined)
  mocks.deleteUserTemplate.mockResolvedValue(undefined)
  mocks.fetchPageMarkdown.mockResolvedValue('# Current body')
  mocks.getTemplate.mockImplementation((id: string) =>
    Promise.resolve(
      id === 'daily'
        ? template(id, 'builtin', 'Daily note')
        : id === 'plugin-plan'
          ? template(id, 'plugin', 'Plugin plan')
          : template(id, 'disk', 'Meeting')
    )
  )
})
afterEach(() => {
  cleanup()
  _resetForTests()
  resetTemplateDraftForTests()
})

describe('TemplatesTab', () => {
  it('renders source-aware built-in, user, and plugin rows', async () => {
    await setup()
    expect(screen.getByText('Built-in')).toBeInTheDocument()
    expect(screen.getByText('User')).toBeInTheDocument()
    expect(screen.getByText('planner')).toBeInTheDocument()
  })

  it('loads built-in lazily as read-only and duplicates it into the shared user list', async () => {
    await setup()
    await fireEvent.click(screen.getByRole('button', { name: /Daily note/ }))
    expect(
      await screen.findByText('Read-only source — duplicate to edit.')
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Markdown body')).toBeDisabled()
    mocks.listTemplates.mockResolvedValueOnce({
      templates: [
        ...summaries,
        {
          id: 'daily-copy',
          title: 'Daily note Copy',
          category: 'General',
          source: 'disk'
        }
      ]
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Duplicate' }))
    await waitFor(() => expect(mocks.saveUserTemplate).toHaveBeenCalled())
    expect(mocks.saveUserTemplate.mock.calls[0][0]).toMatchObject({
      id: 'daily-copy',
      title: 'Daily note Copy',
      source: 'disk'
    })
    await waitFor(() =>
      expect(
        templatesState.items.some((item) => item.id === 'daily-copy')
      ).toBe(true)
    )
  })

  it('duplicates a plugin source into a user-owned template', async () => {
    await setup()
    await fireEvent.click(screen.getByRole('button', { name: /Plugin plan/ }))
    expect(
      await screen.findByText('Read-only source — duplicate to edit.')
    ).toBeInTheDocument()
    await fireEvent.click(screen.getByRole('button', { name: 'Duplicate' }))
    await waitFor(() => expect(mocks.saveUserTemplate).toHaveBeenCalled())
    expect(mocks.saveUserTemplate.mock.calls[0][0]).toMatchObject({
      id: 'plugin-plan-copy',
      title: 'Plugin plan Copy',
      source: 'disk',
      plugin_id: undefined
    })
  })

  it('creates and saves a validated blank Markdown template', async () => {
    await setup()
    await fireEvent.click(screen.getByRole('button', { name: 'New blank' }))
    expect(screen.getByText('Template ID is required.')).toBeInTheDocument()
    expect(screen.getByLabelText('Template ID')).toHaveAttribute(
      'aria-describedby',
      'template-id-error'
    )
    await fireEvent.input(screen.getByLabelText('Template ID'), {
      target: { value: 'release-plan' }
    })
    await fireEvent.input(screen.getByLabelText('Title'), {
      target: { value: 'Release plan' }
    })
    await fireEvent.input(screen.getByLabelText('Markdown body'), {
      target: { value: '# Plan' }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(mocks.saveUserTemplate).toHaveBeenCalled())
    expect(mocks.saveUserTemplate.mock.calls[0][0]).toMatchObject({
      id: 'release-plan',
      title: 'Release plan',
      body: '# Plan'
    })
  })

  it('retains an unsaved draft across unmount and remount', async () => {
    const view = await setup()
    await fireEvent.click(screen.getByRole('button', { name: 'New blank' }))
    await fireEvent.input(screen.getByLabelText('Title'), {
      target: { value: 'Retained draft' }
    })
    view.unmount()

    render(TemplatesTab, {
      props: {
        activeNotebook: 'Work',
        activeSection: 'Notes',
        activePage: 'Current',
        vaultId: 'C:/Vault A'
      }
    })
    expect(screen.getByLabelText('Title')).toHaveValue('Retained draft')
  })

  it('uses the next available duplicate ID when a local fork already exists', async () => {
    mocks.listTemplates.mockResolvedValue({
      templates: [
        ...summaries,
        {
          id: 'daily-copy',
          title: 'Existing fork',
          category: 'General',
          source: 'disk'
        }
      ]
    })
    await loadTemplates()
    render(TemplatesTab, {
      props: {
        activeNotebook: 'Work',
        activeSection: 'Notes',
        activePage: 'Current',
        vaultId: 'C:/Vault A'
      }
    })
    await fireEvent.click(screen.getByRole('button', { name: /Daily note/ }))
    await screen.findByText('Read-only source — duplicate to edit.')
    await fireEvent.click(screen.getByRole('button', { name: 'Duplicate' }))
    await waitFor(() => expect(mocks.saveUserTemplate).toHaveBeenCalled())
    expect(mocks.saveUserTemplate.mock.calls[0][0].id).toBe('daily-copy-2')
  })

  it('edits an existing user template and adopts the canonical saved payload as its baseline', async () => {
    await setup()
    await fireEvent.click(screen.getByRole('button', { name: /Meeting/ }))
    await screen.findByDisplayValue('# Meeting')
    await fireEvent.input(screen.getByLabelText('Title'), {
      target: { value: '  Weekly meeting  ' }
    })
    await fireEvent.input(screen.getByLabelText('Category'), {
      target: { value: '  Work notes  ' }
    })
    await fireEvent.input(screen.getByLabelText('Description'), {
      target: { value: '  Team sync  ' }
    })
    await fireEvent.input(screen.getByLabelText('Icon'), {
      target: { value: '  groups  ' }
    })
    await fireEvent.input(screen.getByLabelText('Markdown body'), {
      target: { value: '# Updated meeting' }
    })

    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(mocks.saveUserTemplate).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 'meeting',
          title: 'Weekly meeting',
          category: 'Work notes',
          description: 'Team sync',
          icon: 'groups',
          body: '# Updated meeting',
          source: 'disk',
          plugin_id: undefined
        })
      )
    )
    expect(screen.getByLabelText('Title')).toHaveValue('Weekly meeting')
    expect(screen.getByLabelText('Category')).toHaveValue('Work notes')
    expect(screen.getByLabelText('Description')).toHaveValue('Team sync')
    expect(screen.getByLabelText('Icon')).toHaveValue('groups')
    expect(screen.getByText('Saved')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()

    await fireEvent.click(screen.getByRole('button', { name: /Daily note/ }))
    await screen.findByText('Read-only source — duplicate to edit.')
    await fireEvent.click(screen.getByRole('button', { name: /Meeting/ }))
    await screen.findByDisplayValue('# Meeting')
    expect(screen.getByLabelText('Markdown body')).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Delete' })).toBeEnabled()
  })

  it('treats an untouched loaded user template as saved', async () => {
    await setup()
    await fireEvent.click(screen.getByRole('button', { name: /Meeting/ }))
    await screen.findByDisplayValue('# Meeting')

    expect(screen.getByText('Saved')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('seeds creation from the current page', async () => {
    await setup()
    await fireEvent.click(
      screen.getByRole('button', { name: 'From current page' })
    )
    await waitFor(() =>
      expect(mocks.fetchPageMarkdown).toHaveBeenCalledWith(
        'Work',
        'Notes',
        'Current'
      )
    )
    expect(screen.getByLabelText('Markdown body')).toHaveValue('# Current body')
    expect(screen.getByLabelText('Template ID')).toHaveValue('current')
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    await fireEvent.input(screen.getByLabelText('Markdown body'), {
      target: { value: '# Edited current body' }
    })
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
  })

  it('does not restore a retained draft in another vault', async () => {
    const view = await setup()
    await fireEvent.click(screen.getByRole('button', { name: 'New blank' }))
    await fireEvent.input(screen.getByLabelText('Title'), {
      target: { value: 'Vault A draft' }
    })
    view.unmount()

    render(TemplatesTab, {
      props: {
        activeNotebook: 'Personal',
        activeSection: 'Notes',
        activePage: 'Home',
        vaultId: 'C:/Vault B'
      }
    })

    expect(screen.queryByDisplayValue('Vault A draft')).not.toBeInTheDocument()
    expect(
      screen.getByText('Select a template, or create a new one.')
    ).toBeInTheDocument()
  })

  it('discards the retained draft when its vault closes', async () => {
    const view = await setup()
    await fireEvent.click(screen.getByRole('button', { name: 'New blank' }))
    await fireEvent.input(screen.getByLabelText('Title'), {
      target: { value: 'Closing vault draft' }
    })

    const vaultClosing = mocks.eventsOn.mock.calls.find(
      ([event]) => event === 'vault:closing'
    )?.[1] as (() => void) | undefined
    expect(vaultClosing).toBeTypeOf('function')
    vaultClosing?.()
    view.unmount()

    render(TemplatesTab, {
      props: {
        activeNotebook: 'Work',
        activeSection: 'Notes',
        activePage: 'Current',
        vaultId: 'C:/Vault A'
      }
    })

    expect(
      screen.queryByDisplayValue('Closing vault draft')
    ).not.toBeInTheDocument()
  })

  it('surfaces lazy-load and current-page read failures without losing the list', async () => {
    mocks.getTemplate.mockRejectedValueOnce(new Error('template unreadable'))
    await setup()
    await fireEvent.click(screen.getByRole('button', { name: /Daily note/ }))
    expect(await screen.findByText('template unreadable')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Meeting/ })).toBeInTheDocument()

    mocks.fetchPageMarkdown.mockRejectedValueOnce(new Error('page unavailable'))
    await fireEvent.click(
      screen.getByRole('button', { name: 'From current page' })
    )
    expect(await screen.findByText('page unavailable')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Meeting/ })).toBeInTheDocument()
  })

  it('ignores a stale template response after a newer selection completes', async () => {
    const daily = deferred<ReturnType<typeof template>>()
    const meeting = deferred<ReturnType<typeof template>>()
    mocks.getTemplate.mockImplementation((id: string) =>
      id === 'daily' ? daily.promise : meeting.promise
    )
    await setup()

    await fireEvent.click(screen.getByRole('button', { name: /Daily note/ }))
    await fireEvent.click(screen.getByRole('button', { name: /Meeting/ }))
    meeting.resolve(template('meeting', 'disk', 'Meeting B'))
    expect(await screen.findByDisplayValue('Meeting B')).toBeInTheDocument()

    daily.resolve(template('daily', 'builtin', 'Daily A'))
    await waitFor(() =>
      expect(screen.getByLabelText('Title')).toHaveValue('Meeting B')
    )
    expect(screen.queryByDisplayValue('Daily A')).not.toBeInTheDocument()
    expect(screen.queryByText('Loading template…')).not.toBeInTheDocument()
  })

  it('warns before discarding an unsaved edit', async () => {
    await setup()
    await fireEvent.click(screen.getByRole('button', { name: /Meeting/ }))
    await screen.findByDisplayValue('# Meeting')
    await fireEvent.input(screen.getByLabelText('Markdown body'), {
      target: { value: 'changed' }
    })
    await fireEvent.click(screen.getByRole('button', { name: /Daily note/ }))
    expect(
      screen.getByRole('dialog', { name: 'Discard unsaved changes?' })
    ).toBeInTheDocument()
    await fireEvent.click(screen.getByTestId('template-confirm-confirm'))
    await waitFor(() => expect(mocks.getTemplate).toHaveBeenCalledWith('daily'))
  })

  it('preserves the active draft when the template watcher refreshes the shared list', async () => {
    await setup()
    const dispose = initTemplates()
    await waitFor(() => expect(mocks.eventsOn).toHaveBeenCalled())
    await fireEvent.click(screen.getByRole('button', { name: /Meeting/ }))
    await screen.findByDisplayValue('# Meeting')
    await fireEvent.input(screen.getByLabelText('Markdown body'), {
      target: { value: '# Unsaved local edit' }
    })

    mocks.listTemplates.mockResolvedValue({
      templates: [
        ...summaries,
        {
          id: 'external',
          title: 'Added externally',
          category: 'Imported',
          source: 'disk'
        }
      ]
    })
    const watcher = mocks.eventsOn.mock.calls.find(
      ([event]) => event === 'templates:changed'
    )?.[1] as (() => void) | undefined
    expect(watcher).toBeTypeOf('function')
    watcher?.()

    await screen.findByRole('button', { name: /Added externally/ })
    expect(screen.getByLabelText('Markdown body')).toHaveValue(
      '# Unsaved local edit'
    )
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
    dispose()
  })

  it('allows deletion only for user templates and confirms before deleting', async () => {
    await setup()
    await fireEvent.click(screen.getByRole('button', { name: /Daily note/ }))
    await screen.findByText('Read-only source — duplicate to edit.')
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: /Meeting/ }))
    await screen.findByDisplayValue('# Meeting')
    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(
      screen.getByRole('dialog', { name: 'Delete template?' })
    ).toBeInTheDocument()
    await fireEvent.click(screen.getByTestId('template-confirm-confirm'))
    await waitFor(() =>
      expect(mocks.deleteUserTemplate).toHaveBeenCalledWith('meeting')
    )
  })

  it('cancels deletion without changing the active user template', async () => {
    await setup()
    await fireEvent.click(screen.getByRole('button', { name: /Meeting/ }))
    await screen.findByDisplayValue('# Meeting')
    const deleteButton = screen.getByRole('button', { name: 'Delete' })
    deleteButton.focus()
    await fireEvent.click(deleteButton)
    await fireEvent.click(screen.getByTestId('template-confirm-cancel'))

    expect(mocks.deleteUserTemplate).not.toHaveBeenCalled()
    expect(screen.getByLabelText('Title')).toHaveValue('Meeting')
    await waitFor(() => expect(deleteButton).toHaveFocus())
  })

  it('moves focus into confirmations instead of focusing the destructive action', async () => {
    await setup()
    await fireEvent.click(screen.getByRole('button', { name: /Meeting/ }))
    await screen.findByDisplayValue('# Meeting')
    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    const dialog = screen.getByRole('dialog', { name: 'Delete template?' })
    await waitFor(() => expect(dialog).toHaveFocus())
    expect(screen.getByTestId('template-confirm-confirm')).not.toHaveFocus()
  })

  it('makes saved and deleted templates visible to picker consumers through the shared store', async () => {
    await setup()
    render(TemplatePicker, {
      props: { mode: 'insert', onClose: vi.fn(), onInsertBlocks: vi.fn() }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'New blank' }))
    await fireEvent.input(screen.getByLabelText('Template ID'), {
      target: { value: 'shared-template' }
    })
    await fireEvent.input(screen.getByLabelText('Title'), {
      target: { value: 'Shared template' }
    })
    mocks.listTemplates.mockResolvedValueOnce({
      templates: [
        ...summaries,
        {
          id: 'shared-template',
          title: 'Shared template',
          category: 'General',
          source: 'disk'
        }
      ]
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(
      await screen.findByRole('option', { name: /Shared template/ })
    ).toBeInTheDocument()
    expect(
      templatesState.items.some(({ id }) => id === 'shared-template')
    ).toBe(true)

    mocks.listTemplates.mockResolvedValueOnce({ templates: summaries })
    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await fireEvent.click(screen.getByTestId('template-confirm-confirm'))
    await waitFor(() =>
      expect(
        screen.queryByRole('option', { name: /Shared template/ })
      ).not.toBeInTheDocument()
    )
    expect(
      templatesState.items.some(({ id }) => id === 'shared-template')
    ).toBe(false)
  })

  it('keeps drafts visible when save or delete fails', async () => {
    mocks.saveUserTemplate.mockRejectedValueOnce(new Error('ID already exists'))
    await setup()
    await fireEvent.click(screen.getByRole('button', { name: 'New blank' }))
    await fireEvent.input(screen.getByLabelText('Template ID'), {
      target: { value: 'duplicate' }
    })
    await fireEvent.input(screen.getByLabelText('Title'), {
      target: { value: 'Duplicate' }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('ID already exists')).toBeInTheDocument()
    expect(screen.getByLabelText('Title')).toHaveValue('Duplicate')

    cleanup()
    _resetForTests()
    resetTemplateDraftForTests()
    mocks.deleteUserTemplate.mockRejectedValueOnce(
      new Error('permission denied')
    )
    await setup()
    await fireEvent.click(screen.getByRole('button', { name: /Meeting/ }))
    await screen.findByDisplayValue('# Meeting')
    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await fireEvent.click(screen.getByTestId('template-confirm-confirm'))
    expect(await screen.findByText('permission denied')).toBeInTheDocument()
    expect(screen.getByLabelText('Title')).toHaveValue('Meeting')
  })
})
