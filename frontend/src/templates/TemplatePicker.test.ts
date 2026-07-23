// Component-level coverage for the template picker (#55). Mirrors the
// AppearanceTab.test.ts pattern: hoisted mock state + vi.mock for the store
// and Wails IPC, then render + screen assertions.
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

type MockTemplate = {
  id: string
  title: string
  description: string
  category: string
  icon: string
  source: string
  plugin_id?: string
  placeholders: Array<{
    name: string
    description: string
    required: boolean
    default?: string
  }>
}

const mocks = vi.hoisted(() => ({
  templatesState: {
    items: [
      {
        id: 'daily-note',
        title: 'Daily Note',
        description: 'A daily log.',
        category: 'daily',
        icon: 'today',
        source: 'builtin',
        placeholders: []
      },
      {
        id: 'meeting-notes',
        title: 'Meeting Notes',
        description: 'Structured meetings.',
        category: 'meetings',
        icon: 'group',
        source: 'builtin',
        placeholders: [
          { name: 'meeting_title', description: 'Title', required: true }
        ]
      }
    ] as MockTemplate[],
    loadError: null as string | null,
    loading: false
  },
  templateStatus: { kind: 'info' as const, message: '' },
  loadTemplates: vi.fn(),
  clearTemplateStatus: vi.fn(),
  setTemplateStatus: vi.fn(),
  notifications: { items: [] as Array<{ kind: string; message: string }> },
  pushNotification: vi.fn()
}))

vi.mock('../../bindings/silt/app.js', () => ({
  ListTemplates: vi.fn(),
  GetTemplate: vi.fn(),
  RenderTemplate: vi.fn().mockResolvedValue('# Preview content'),
  SaveUserTemplate: vi.fn(),
  DeleteUserTemplate: vi.fn(),
  ReloadTemplates: vi.fn(),
  CreatePageFromTemplate: vi.fn().mockResolvedValue('2026-06-15'),
  RenderTemplateBlocks: vi.fn().mockResolvedValue([])
}))

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
    Nullable: (fn: unknown) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

vi.mock('./store.svelte', () => ({
  templatesState: mocks.templatesState,
  templateStatus: mocks.templateStatus,
  loadTemplates: mocks.loadTemplates,
  initTemplates: vi.fn(() => () => {}),
  setTemplateStatus: mocks.setTemplateStatus,
  clearTemplateStatus: mocks.clearTemplateStatus
}))

vi.mock('../notifications/store.svelte', () => ({
  notificationsState: mocks.notifications,
  pushNotification: mocks.pushNotification,
  dismissNotification: vi.fn(),
  clearAllNotifications: vi.fn()
}))

import TemplatePicker from './TemplatePicker.svelte'

describe('TemplatePicker (#55)', () => {
  beforeEach(() => {
    mocks.loadTemplates.mockReset()
    mocks.clearTemplateStatus.mockReset()
    mocks.setTemplateStatus.mockReset()
    mocks.pushNotification.mockReset()
  })

  afterEach(() => {
    cleanup()
  })

  it('renders a dialog with template options grouped by category', () => {
    render(TemplatePicker, {
      props: { mode: 'insert', onClose: vi.fn(), onInsertBlocks: vi.fn() }
    })

    expect(
      screen.getByRole('dialog', { name: 'Template picker' })
    ).toBeInTheDocument()

    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(2)
    expect(screen.getByText('Daily Note')).toBeInTheDocument()
    expect(screen.getByText('Meeting Notes')).toBeInTheDocument()
  })

  it('shows the Insert button in insert mode', () => {
    render(TemplatePicker, {
      props: { mode: 'insert', onClose: vi.fn(), onInsertBlocks: vi.fn() }
    })

    expect(screen.getByText('Insert')).toBeInTheDocument()
  })

  it('shows the Create Page button + page-name field in new-page mode', () => {
    render(TemplatePicker, {
      props: {
        mode: 'new-page',
        notebook: 'Work',
        section: '',
        onClose: vi.fn(),
        onCreatedPage: vi.fn()
      }
    })

    expect(screen.getByText('Create Page')).toBeInTheDocument()
    expect(screen.getByLabelText('Page name')).toBeInTheDocument()
  })

  it('filters the list when searching', async () => {
    render(TemplatePicker, {
      props: { mode: 'insert', onClose: vi.fn(), onInsertBlocks: vi.fn() }
    })

    const search = screen.getByLabelText('Search templates')
    await fireEvent.input(search, { target: { value: 'meeting' } })

    expect(screen.getByText('Meeting Notes')).toBeInTheDocument()
    expect(screen.queryByText('Daily Note')).not.toBeInTheDocument()
  })

  it('renders the placeholder form when a template with placeholders is focused', () => {
    render(TemplatePicker, {
      props: { mode: 'insert', onClose: vi.fn(), onInsertBlocks: vi.fn() }
    })

    // Click the Meeting Notes option (it has a meeting_title placeholder).
    const meetingOption = screen.getByText('Meeting Notes')
    void fireEvent.click(meetingOption)

    expect(screen.getByText('Placeholders')).toBeInTheDocument()
    expect(screen.getByText(/meeting_title/)).toBeInTheDocument()
  })

  it('shows the empty state when no templates match the search', async () => {
    render(TemplatePicker, {
      props: { mode: 'insert', onClose: vi.fn(), onInsertBlocks: vi.fn() }
    })

    const search = screen.getByLabelText('Search templates')
    await fireEvent.input(search, { target: { value: 'zzz-no-match' } })

    expect(
      screen.getByText('No templates match your search.')
    ).toBeInTheDocument()
  })

  it('pre-fills the page-name field in new-page mode (#95)', () => {
    render(TemplatePicker, {
      props: {
        mode: 'new-page',
        notebook: 'Work',
        section: '',
        onClose: vi.fn(),
        onCreatedPage: vi.fn()
      }
    })

    const input = screen.getByLabelText('Page name')
    expect(input.value).toMatch(/^Page \d{4}-\d{2}-\d{2}$/)
  })

  it('dispatches focus-page-title on successful CreatePageFromTemplate (#95)', async () => {
    const { CreatePageFromTemplate } =
      await import('../../bindings/silt/app.js')
    ;(CreatePageFromTemplate as ReturnType<typeof vi.fn>).mockResolvedValue(
      '2026-06-15'
    )

    const onCreatedPage = vi.fn()
    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

    render(TemplatePicker, {
      props: {
        mode: 'new-page',
        notebook: 'Work',
        section: '',
        onClose: vi.fn(),
        onCreatedPage
      }
    })

    const input = screen.getByLabelText('Page name')
    await fireEvent.input(input, { target: { value: 'Sprint Day' } })
    const createBtn = screen.getByText('Create Page')
    await fireEvent.click(createBtn)

    await vi.waitFor(() => {
      expect(CreatePageFromTemplate).toHaveBeenCalledWith(
        'Work',
        '',
        'Sprint Day',
        '',
        'daily-note',
        expect.any(Object)
      )
    })
    expect(dispatchSpy).toHaveBeenCalled()
    const event = dispatchSpy.mock.calls
      .map((c) => c[0])
      .find((e) => e.type === 'focus-page-title')
    expect(event).toBeDefined()
    expect(onCreatedPage).toHaveBeenCalledWith('Sprint Day')
    dispatchSpy.mockRestore()
  })

  it('pushes a toast when CreatePageFromTemplate fails (#94)', async () => {
    const { CreatePageFromTemplate } =
      await import('../../bindings/silt/app.js')
    ;(CreatePageFromTemplate as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      new Error('disk full')
    )
    const onCreatedPage = vi.fn()
    render(TemplatePicker, {
      props: {
        mode: 'new-page',
        notebook: 'Work',
        section: '',
        onClose: vi.fn(),
        onCreatedPage
      }
    })
    const input = screen.getByLabelText('Page name')
    await fireEvent.input(input, { target: { value: 'Will Fail' } })
    await fireEvent.click(screen.getByText('Create Page'))
    await vi.waitFor(() => {
      expect(mocks.pushNotification).toHaveBeenCalledWith(
        expect.objectContaining({ kind: 'error' })
      )
    })
    expect(mocks.setTemplateStatus).toHaveBeenCalledWith(
      expect.objectContaining({ kind: 'error' })
    )
    expect(onCreatedPage).not.toHaveBeenCalled()
  })

  it('pushes a toast when RenderTemplateBlocks fails (#94)', async () => {
    const { RenderTemplateBlocks } = await import('../../bindings/silt/app.js')
    ;(RenderTemplateBlocks as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      new Error('IPC lost')
    )
    const onInsertBlocks = vi.fn()
    render(TemplatePicker, {
      props: { mode: 'insert', onClose: vi.fn(), onInsertBlocks }
    })
    await fireEvent.click(screen.getByText('Insert'))
    await vi.waitFor(() => {
      expect(mocks.pushNotification).toHaveBeenCalledWith(
        expect.objectContaining({ kind: 'error' })
      )
    })
    expect(mocks.setTemplateStatus).toHaveBeenCalledWith(
      expect.objectContaining({ kind: 'error' })
    )
    expect(onInsertBlocks).not.toHaveBeenCalled()
  })

  it('allows create when required placeholder has a declared default', async () => {
    const prevItems = mocks.templatesState.items
    mocks.templatesState.items = [
      {
        id: 'with-default',
        title: 'With Default',
        description: '',
        category: 'notes',
        icon: 'note',
        source: 'user',
        placeholders: [
          {
            name: 'topic',
            description: 'Topic',
            required: true,
            default: 'General'
          }
        ]
      }
    ]
    try {
      const { CreatePageFromTemplate } =
        await import('../../bindings/silt/app.js')
      const createFn = CreatePageFromTemplate as ReturnType<typeof vi.fn>
      createFn.mockReset()
      createFn.mockResolvedValue('2026-06-15')
      const onCreatedPage = vi.fn()
      render(TemplatePicker, {
        props: {
          mode: 'new-page',
          notebook: 'Work',
          section: '',
          onClose: vi.fn(),
          onCreatedPage
        }
      })
      await fireEvent.click(screen.getByText('With Default'))
      const input = screen.getByLabelText('Page name')
      await fireEvent.input(input, { target: { value: 'Note' } })
      await fireEvent.click(screen.getByText('Create Page'))
      await vi.waitFor(() => {
        expect(createFn).toHaveBeenCalled()
      })
      expect(onCreatedPage).toHaveBeenCalledWith('Note')
    } finally {
      mocks.templatesState.items = prevItems
    }
  })

  it('blocks create when a required placeholder is empty (#650)', async () => {
    const { CreatePageFromTemplate } =
      await import('../../bindings/silt/app.js')
    const createFn = CreatePageFromTemplate as ReturnType<typeof vi.fn>
    createFn.mockClear()
    mocks.setTemplateStatus.mockClear()
    const onCreatedPage = vi.fn()
    render(TemplatePicker, {
      props: {
        mode: 'new-page',
        notebook: 'Work',
        section: '',
        onClose: vi.fn(),
        onCreatedPage
      }
    })
    await fireEvent.click(screen.getByText('Meeting Notes'))
    expect(screen.getByText('Placeholders')).toBeInTheDocument()
    const input = screen.getByLabelText('Page name')
    await fireEvent.input(input, { target: { value: 'Standup' } })
    await fireEvent.click(screen.getByText('Create Page'))
    await vi.waitFor(() => {
      expect(mocks.setTemplateStatus).toHaveBeenCalledWith(
        expect.objectContaining({
          kind: 'error',
          message: expect.stringContaining('meeting_title')
        })
      )
    })
    expect(createFn).not.toHaveBeenCalled()
    expect(onCreatedPage).not.toHaveBeenCalled()
    expect(screen.getByText('Required')).toBeInTheDocument()
  })

  it('surfaces page_exists with open-existing choice (#652)', async () => {
    const { CreatePageFromTemplate } =
      await import('../../bindings/silt/app.js')
    const createFn = CreatePageFromTemplate as ReturnType<typeof vi.fn>
    createFn.mockReset()
    createFn.mockRejectedValueOnce(
      new Error(
        JSON.stringify({
          code: 'page_exists',
          message: 'a page named "Standup" already exists'
        })
      )
    )
    mocks.setTemplateStatus.mockClear()
    const onCreatedPage = vi.fn()
    const onClose = vi.fn()
    render(TemplatePicker, {
      props: {
        mode: 'new-page',
        notebook: 'Work',
        section: '',
        onClose,
        onCreatedPage
      }
    })
    const input = screen.getByLabelText('Page name')
    await fireEvent.input(input, { target: { value: 'Standup' } })
    await fireEvent.click(screen.getByText('Create Page'))
    await vi.waitFor(() => {
      expect(screen.getByText('Open existing')).toBeInTheDocument()
    })
    expect(onCreatedPage).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
    expect(mocks.setTemplateStatus).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'error',
        message: expect.stringContaining('already exists')
      })
    )

    // Rename the field after collision — Open existing must still use Standup.
    await fireEvent.input(input, { target: { value: 'Standup 2' } })
    await fireEvent.click(screen.getByText('Open existing'))
    expect(onCreatedPage).toHaveBeenCalledWith('Standup')
    expect(onCreatedPage).not.toHaveBeenCalledWith('Standup 2')
    expect(onClose).toHaveBeenCalled()
  })

  it('groups plugin templates under Plugins / <plugin_id> (#96)', () => {
    mocks.templatesState.items.push({
      id: 'kanban-sprint',
      title: 'Sprint',
      description: 'A plugin template',
      category: 'projects',
      icon: 'sprint',
      source: 'plugin',
      plugin_id: 'silt-tasks',
      placeholders: []
    })
    render(TemplatePicker, {
      props: { mode: 'insert', onClose: vi.fn(), onInsertBlocks: vi.fn() }
    })
    // The plugin group header appears; the plugin template is rendered.
    expect(screen.getByText('Plugins / silt-tasks')).toBeInTheDocument()
    expect(screen.getByText('Sprint')).toBeInTheDocument()
  })
})
