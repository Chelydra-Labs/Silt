import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// CommentThread reads the local_author pref synchronously via the settings
// store + persists via updatePluginSetting. Mock the store so tests can drive
// both the "pref unset → seed from OS user" and "pref set → skip IPC" paths.
const mocks = vi.hoisted(() => ({
  updatePluginSetting: vi.fn().mockResolvedValue(true),
  settings: {
    config: {
      plugins: {
        active: [],
        disabled: [],
        plugin_settings: {} as Record<string, Record<string, unknown>>
      }
    }
  },
  persistLocalAuthor: vi.fn().mockResolvedValue(true)
}))

vi.mock('../../../../settings/store.svelte', () => ({
  settings: mocks.settings,
  updatePluginSetting: mocks.updatePluginSetting
}))

vi.mock('../../../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: vi.fn(() => () => {})
}))

// jsdom polyfills: transition:fly (used on each comment article) calls
// element.animate(); RichText's EmbedPortal path uses elementFromPoint /
// Range rects. Copied from TasksHub.test.ts so the import graph resolves.
if (!Element.prototype.animate) {
  Element.prototype.animate = function () {
    return {
      cancel() {},
      finish() {},
      play() {},
      pause() {},
      reverse() {},
      addEventListener() {},
      removeEventListener() {},
      onfinish: null,
      oncancel: null
    } as unknown as Animation
  }
}
if (typeof document !== 'undefined' && !document.elementFromPoint) {
  document.elementFromPoint = () => document.body
}
if (
  typeof window !== 'undefined' &&
  window.Range &&
  !Range.prototype.getClientRects
) {
  const zeroRect: DOMRect = {
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 0,
    bottom: 0,
    width: 0,
    height: 0,
    toJSON() {
      return this
    }
  }
  Range.prototype.getClientRects = (() => [
    zeroRect
  ]) as unknown as typeof Range.prototype.getClientRects
  Range.prototype.getBoundingClientRect = () => zeroRect
}

import CommentThread from './CommentThread.svelte'
import type { PluginContext, SubtreeBlock } from '../../../sdk'
import { v2CtxStubs } from '../../../test-helpers'

interface CommentThreadProps {
  taskId: string
  notebook: string
  section: string
  page: string
  fileDate: string
  ctx: PluginContext
  onCommentsChanged?: () => void
}

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    ...v2CtxStubs,
    activeNotebook: 'Work',
    activeSection: 'Journal',
    activePage: 'Daily',
    today: '2026-07-06',
    sqliteQuery: vi.fn(),
    fetchSubtree: vi.fn().mockResolvedValue([]),
    addTaskComment: vi.fn().mockResolvedValue('new-uuid'),
    deleteBlock: vi.fn().mockResolvedValue(true),
    getLocalAuthor: vi.fn().mockResolvedValue('osuser'),
    on: vi.fn(() => () => {}),
    ...overrides
  } as unknown as PluginContext
}

function makeBlock(o: Partial<SubtreeBlock> & { id: string }): SubtreeBlock {
  return {
    type: 'NOTE',
    depth: 1,
    raw_text: '',
    clean_text: '',
    line_number: 0,
    ...o
  } as SubtreeBlock
}

function mount(props: Partial<CommentThreadProps> = {}) {
  return render(CommentThread, {
    props: {
      taskId: 'task-1',
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      fileDate: '2026-07-01',
      ctx: makeCtx(),
      ...props
    } as CommentThreadProps
  })
}

async function flush() {
  await new Promise((r) => setTimeout(r, 0))
}

describe('CommentThread', () => {
  beforeEach(() => {
    // Reset the persisted local_author between tests so the seed-vs-skip
    // branches are deterministic.
    mocks.settings.config.plugins.plugin_settings = {}
    mocks.updatePluginSetting.mockClear()
    mocks.updatePluginSetting.mockResolvedValue(true)
  })
  afterEach(() => cleanup())

  it('renders the empty state when fetchSubtree returns []', async () => {
    mount()
    await flush()
    expect(await screen.findByTestId('comment-empty-state')).toBeInTheDocument()
  })

  it('renders comments from fetchSubtree (NOTE blocks with author/timestamp)', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'First comment',
          author: 'alice',
          timestamp: '2026-07-01T09:00:00'
        }),
        makeBlock({
          id: 'c2',
          clean_text: 'Second comment',
          author: 'bob',
          timestamp: '2026-07-02T10:30:00'
        })
      ])
    })
    mount({ ctx })
    await flush()
    const articles = document.querySelectorAll('[role="comment"]')
    expect(articles.length).toBe(2)
    expect(screen.getByText('First comment')).toBeInTheDocument()
    expect(screen.getByText('Second comment')).toBeInTheDocument()
    expect(screen.getByText('alice')).toBeInTheDocument()
    expect(screen.getByText('bob')).toBeInTheDocument()
  })

  it('renders "Unknown" + "Undated" for legacy NOTEs without author/timestamp', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi
        .fn()
        .mockResolvedValue([
          makeBlock({ id: 'legacy', clean_text: 'old note, no tokens' })
        ])
    })
    mount({ ctx })
    await flush()
    expect(screen.getByText('Unknown')).toBeInTheDocument()
    expect(screen.getByText('Undated')).toBeInTheDocument()
  })

  it('clicking the delete button calls ctx.deleteBlock with the comment id', async () => {
    const deleteBlock = vi.fn().mockResolvedValue(true)
    // jsdom leaves window.confirm unimplemented (returns false) — stub it so
    // the confirm guard lets the delete through.
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'to delete',
          author: 'a',
          timestamp: '2026-07-01T09:00:00'
        })
      ]),
      deleteBlock
    })
    mount({ ctx })
    await flush()
    const delBtn = screen.getByLabelText('Delete comment')
    await fireEvent.click(delBtn)
    await flush()
    expect(deleteBlock).toHaveBeenCalledWith('c1')
    confirmSpy.mockRestore()
  })

  it('on delete failure, restores the comment at its original index (not the end)', async () => {
    // Pins the round-5 fix: onDelete captures the index before filtering and
    // splices the comment back at that slot on failure. Appending at the end
    // would reshuffle thread order on every transient failure.
    const deleteBlock = vi.fn().mockRejectedValue(new Error('disk locked'))
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'a',
          clean_text: 'A',
          author: 'x',
          timestamp: '2026-07-01T09:00:00'
        }),
        makeBlock({
          id: 'b',
          clean_text: 'B',
          author: 'x',
          timestamp: '2026-07-02T09:00:00'
        }),
        makeBlock({
          id: 'c',
          clean_text: 'C',
          author: 'x',
          timestamp: '2026-07-03T09:00:00'
        })
      ]),
      deleteBlock
    })
    mount({ ctx })
    await flush()

    // Delete the middle comment (B).
    const delBtns = screen.getAllByLabelText('Delete comment')
    await fireEvent.click(delBtns[1]!)
    await flush()

    // B is restored at its original index (1), not appended at the end (2).
    const articles = document.querySelectorAll('[role="comment"]')
    expect(articles).toHaveLength(3)
    expect(articles[1]!.textContent).toContain('B')
    expect(articles[2]!.textContent).toContain('C')
    expect(screen.getByRole('alert')).toBeInTheDocument()
    confirmSpy.mockRestore()
  })

  it('typing in the composer + Enter calls ctx.addTaskComment with taskId + text + author', async () => {
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const ctx = makeCtx({ addTaskComment })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'hello world' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()
    expect(addTaskComment).toHaveBeenCalledWith(
      'task-1',
      'hello world',
      'osuser'
    )
  })

  it('failed post restores the draft + "Try again" re-submits (#459)', async () => {
    const addTaskComment = vi
      .fn()
      .mockRejectedValueOnce(new Error('server down'))
      .mockResolvedValue('real-id')
    const ctx = makeCtx({ addTaskComment })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'please retry me' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()

    // The post failed: the error banner surfaces with a "Try again" CTA...
    expect(addTaskComment).toHaveBeenCalledTimes(1)
    const retry = screen.getByRole('button', { name: 'Try again' })
    // ...and the failed draft is restored to the composer for editing/retry.
    expect(ta.value).toBe('please retry me')

    await fireEvent.click(retry)
    await flush()

    // The retry re-invoked addTaskComment with the same draft, and it landed.
    expect(addTaskComment).toHaveBeenCalledTimes(2)
    expect(addTaskComment).toHaveBeenLastCalledWith(
      'task-1',
      'please retry me',
      'osuser'
    )
    // Banner cleared after the successful retry.
    expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull()
  })

  it('Enter submits; Shift+Enter does NOT submit (inserts a newline)', async () => {
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const ctx = makeCtx({ addTaskComment })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'line one' } })
    await fireEvent.keyDown(ta, { key: 'Enter', shiftKey: true })
    await flush()
    expect(addTaskComment).not.toHaveBeenCalled()
  })

  it('submit button is disabled when the composer textarea is empty', async () => {
    mount()
    await flush()
    const btn = screen.getByText('Post') as HTMLButtonElement
    expect(btn.disabled).toBe(true)
  })

  it('optimistically renders the new comment before addTaskComment resolves', async () => {
    let resolveAdd: (id: string) => void = () => {}
    const addTaskComment = vi.fn(
      () => new Promise<string>((r) => (resolveAdd = r))
    )
    const ctx = makeCtx({ addTaskComment })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'optimistic body' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    // The optimistic comment appears immediately, before the promise resolves.
    expect(screen.getByText('optimistic body')).toBeInTheDocument()
    expect(addTaskComment).toHaveBeenCalled()
    // Resolve to let pending state clear + avoid hanging the test.
    resolveAdd('server-uuid')
    await flush()
  })

  it('on success, replaces the optimistic id with the UUID returned by addTaskComment', async () => {
    const addTaskComment = vi.fn().mockResolvedValue('server-uuid-123')
    const ctx = makeCtx({ addTaskComment })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'real body' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()
    // The comment is still present (now server-confirmed) and no longer
    // carries the "saving…" pending marker.
    expect(screen.getByText('real body')).toBeInTheDocument()
    expect(document.body.textContent).not.toContain('saving…')
  })

  it('on error, reverts the optimistic entry and shows role="alert"', async () => {
    const addTaskComment = vi.fn().mockRejectedValue(new Error('disk locked'))
    const ctx = makeCtx({ addTaskComment })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'doomed' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()
    expect(screen.queryByText('doomed')).toBeNull()
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })

  it('seeds composerAuthor from ctx.getLocalAuthor when the pref is empty, and persists it', async () => {
    mocks.settings.config.plugins.plugin_settings = {} // pref unset
    const getLocalAuthor = vi.fn().mockResolvedValue('osuser')
    const ctx = makeCtx({ getLocalAuthor })
    mount({ ctx })
    await flush()
    expect(getLocalAuthor).toHaveBeenCalled()
    const authorInput = screen.getByLabelText(
      'Default comment author'
    ) as HTMLInputElement
    expect(authorInput.value).toBe('osuser')
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'silt-tasks',
      'local_author',
      'osuser'
    )
  })

  it('does NOT call ctx.getLocalAuthor when the local_author pref is already set', async () => {
    mocks.settings.config.plugins.plugin_settings = {
      'silt-tasks': { local_author: 'saved-name' }
    }
    const getLocalAuthor = vi.fn()
    const ctx = makeCtx({ getLocalAuthor })
    mount({ ctx })
    await flush()
    expect(getLocalAuthor).not.toHaveBeenCalled()
    const authorInput = screen.getByLabelText(
      'Default comment author'
    ) as HTMLInputElement
    expect(authorInput.value).toBe('saved-name')
  })

  it('changing the author input + blur calls persistLocalAuthor (updatePluginSetting)', async () => {
    mount()
    await flush()
    const authorInput = screen.getByLabelText(
      'Default comment author'
    ) as HTMLInputElement
    mocks.updatePluginSetting.mockClear()
    await fireEvent.input(authorInput, { target: { value: 'renamed' } })
    await fireEvent.blur(authorInput)
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'silt-tasks',
      'local_author',
      'renamed'
    )
  })

  it('block:changed event triggers a reload of the thread', async () => {
    let off = vi.fn()
    let registered: (() => void) | null = null
    const on = vi.fn((evt: string, cb: () => void) => {
      if (evt === 'block:changed') registered = cb
      return () => off()
    })
    const fetchSubtree = vi
      .fn()
      .mockResolvedValueOnce([]) // initial load
      .mockResolvedValueOnce([
        makeBlock({
          id: 'ext',
          clean_text: 'arrived via external edit',
          author: 'x',
          timestamp: '2026-07-03T08:00:00'
        })
      ]) // reload after event
    const ctx = makeCtx({
      on: on as unknown as PluginContext['on'],
      fetchSubtree
    })
    mount({ ctx })
    await flush()
    expect(registered).toBeTruthy()
    registered!()
    await flush()
    expect(screen.getByText('arrived via external edit')).toBeInTheDocument()
  })

  it('switching the taskId prop triggers a reload', async () => {
    const fetchSubtree = vi.fn().mockResolvedValue([]) // task-1, then task-2
    const ctx = makeCtx({ fetchSubtree })
    const { rerender } = mount({ ctx, taskId: 'task-1' })
    await flush()
    const initialCalls = fetchSubtree.mock.calls.length
    await rerender({
      taskId: 'task-2',
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      fileDate: '2026-07-01',
      ctx
    })
    await flush()
    expect(fetchSubtree.mock.calls.length).toBeGreaterThan(initialCalls)
    expect(fetchSubtree.mock.calls.at(-1)?.[0]).toBe('task-2')
  })

  it('renders **bold** segments inside <strong>', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'b',
          clean_text: 'this is **very important** text',
          author: 'a',
          timestamp: '2026-07-01T09:00:00'
        })
      ])
    })
    mount({ ctx })
    await flush()
    const strong = document.querySelector('[role="comment"] strong')
    expect(strong).toBeTruthy()
    expect(strong?.textContent).toContain('very important')
  })
})
