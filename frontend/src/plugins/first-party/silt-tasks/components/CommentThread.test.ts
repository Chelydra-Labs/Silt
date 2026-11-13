import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// CommentThread attributes comments from the configured local_author pref
// (#1125): a fresh ctx.getPluginSettings() read at mount (Settings writes
// emit no config:changed, so the hub's cached slice can be stale) with no
// per-comment Author input. An ABSENT pref falls back to the OS display
// name via ctx.getLocalAuthor (display-only); a present "" is an explicit
// clear and posts without an author. The test ctx's getPluginSettings
// resolves `mocks.tasksSettings`, so each test seeds the slice before
// mount.
const mocks = vi.hoisted(() => ({
  updatePluginSetting: vi.fn().mockResolvedValue(true),
  tasksSettings: {} as Record<string, unknown>
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
import {
  isCommentBlock,
  isNoteBlock,
  partitionComments
} from '../commentBlocks'

interface CommentThreadProps {
  taskId: string
  notebook: string
  section: string
  page: string
  fileDate: string
  ctx: PluginContext
  onCommentsChanged?: () => void
  layout?: 'inline' | 'drawer'
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
    getPluginSettings: vi.fn(() => Promise.resolve(mocks.tasksSettings)),
    updatePluginSetting: mocks.updatePluginSetting,
    on: vi.fn(() => () => {}),
    ...overrides
  } as unknown as PluginContext
}

// Control what the component's fresh ctx.getPluginSettings() read resolves.
function seedSettings(slice: Record<string, unknown> = {}) {
  mocks.tasksSettings = slice
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
  beforeEach(async () => {
    // Reset the resolved local_author between tests so the configured-vs-
    // empty attribution branches are deterministic.
    mocks.updatePluginSetting.mockClear()
    mocks.updatePluginSetting.mockResolvedValue(true)
    seedSettings({})
  })
  afterEach(() => cleanup())

  it('renders the empty state when fetchSubtree returns []', async () => {
    mount()
    await flush()
    expect(await screen.findByTestId('comment-empty-state')).toBeInTheDocument()
  })

  it('drawer layout bounds the comment list and marks the composer sticky', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'One',
          author: 'a',
          timestamp: '2026-07-01T09:00:00'
        })
      ])
    })
    mount({ ctx, layout: 'drawer' })
    await flush()
    const list = await screen.findByTestId('comment-list-scroll')
    expect(list.className).toMatch(/max-h-/)
    expect(screen.getByTestId('comment-composer-sticky')).toBeInTheDocument()
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
      undefined
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
      undefined
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
    // The optimistic comment appears before addTaskComment resolves (its
    // promise is still pending here; only the author read's microtasks
    // ran — the absent pref takes the OS-name fallback hop).
    await flush()
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

  it('renders no per-comment Author input (configured in Settings, #1125)', async () => {
    mount()
    await flush()
    expect(screen.queryByLabelText('Default comment author')).toBeNull()
  })

  it('attributes top-level comments AND replies with the configured author', async () => {
    seedSettings({ local_author: 'Dana' })
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const ctx = makeCtx({
      addTaskComment,
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'parent comment',
          author: 'alice',
          timestamp: '2026-07-01T09:00:00',
          parent_id: 'task-1'
        })
      ])
    })
    mount({ ctx })
    await flush()

    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'top-level' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()
    expect(addTaskComment).toHaveBeenCalledWith('task-1', 'top-level', 'Dana')

    // Two comments now (seeded parent + the just-posted top-level); reply
    // to the seeded one.
    const replyBtn = screen.getAllByLabelText('Reply to comment')[0]!
    await fireEvent.click(replyBtn)
    await flush()
    const replyTa = screen.getByLabelText('Reply text') as HTMLTextAreaElement
    await fireEvent.input(replyTa, { target: { value: 'a reply' } })
    await fireEvent.keyDown(replyTa, { key: 'Enter' })
    await flush()
    expect(addTaskComment).toHaveBeenLastCalledWith(
      'task-1',
      'a reply',
      'Dana',
      'c1'
    )
  })

  it('absent pref → posts with the OS display name (display-only, never persisted)', async () => {
    seedSettings({})
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const getLocalAuthor = vi.fn().mockResolvedValue('OS Name')
    const ctx = makeCtx({ addTaskComment, getLocalAuthor })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'auto-attributed' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()
    expect(addTaskComment).toHaveBeenCalledWith(
      'task-1',
      'auto-attributed',
      'OS Name'
    )
    expect(getLocalAuthor).toHaveBeenCalled()
    // The fallback is display-only: posting must not persist anything.
    expect(mocks.updatePluginSetting).not.toHaveBeenCalled()
  })

  it('present "" pref → author is undefined (explicit clear, no OS fallback)', async () => {
    seedSettings({ local_author: '' })
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const getLocalAuthor = vi.fn().mockResolvedValue('OS Name')
    const ctx = makeCtx({ addTaskComment, getLocalAuthor })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'anon' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()
    expect(addTaskComment).toHaveBeenCalledWith('task-1', 'anon', undefined)
    expect(getLocalAuthor).not.toHaveBeenCalled()
  })

  it('getPluginSettings rejection still posts (author undefined, non-fatal)', async () => {
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const ctx = makeCtx({
      addTaskComment,
      getPluginSettings: vi.fn().mockRejectedValue(new Error('ipc died'))
    })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'resilient' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()
    // loadAuthor's catch path swallows the rejection at mount AND at the
    // submit-time re-read — the post carries no author token.
    expect(addTaskComment).toHaveBeenCalledWith(
      'task-1',
      'resilient',
      undefined
    )
  })

  it('keeps the loaded author when only the submit-time re-read fails', async () => {
    // Mount read succeeds, submit re-read rejects: discarding the
    // mount-time author would silently downgrade attribution to
    // "Unknown" over a transient IPC failure.
    const getPluginSettings = vi
      .fn()
      .mockResolvedValueOnce({ local_author: 'Dana' })
      .mockRejectedValue(new Error('ipc died'))
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const ctx = makeCtx({ addTaskComment, getPluginSettings })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'keep author' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()
    expect(addTaskComment).toHaveBeenCalledWith('task-1', 'keep author', 'Dana')
  })

  it('whitespace-only pref ("  ") posts with author undefined', async () => {
    seedSettings({ local_author: '   ' })
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const ctx = makeCtx({ addTaskComment })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'anon' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()
    expect(addTaskComment).toHaveBeenCalledWith('task-1', 'anon', undefined)
  })

  it('re-reads the author at submit (Settings change after mount wins)', async () => {
    seedSettings({})
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const ctx = makeCtx({ addTaskComment })
    mount({ ctx })
    await flush()
    // The user sets the author in Settings while this thread stays open;
    // the submit-time re-read must pick it up.
    seedSettings({ local_author: 'Dana' })
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'fresh' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()
    expect(addTaskComment).toHaveBeenCalledWith('task-1', 'fresh', 'Dana')
  })

  // Opens the claim-before-await window the pending guard protects: the
  // settings read pends until the test resolves it, so a second Enter
  // lands while submit()/submitReply() is still suspended.
  function deferredSettings() {
    let resolveSettings!: (v: Record<string, unknown>) => void
    const pending = new Promise<Record<string, unknown>>((res) => {
      resolveSettings = res
    })
    return {
      getPluginSettings: vi.fn(() => pending),
      resolveSettings
    }
  }

  it('drops a second Enter during the author re-read (no double post)', async () => {
    const { getPluginSettings, resolveSettings } = deferredSettings()
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const ctx = makeCtx({ addTaskComment, getPluginSettings })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'once' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    // Refill the draft so the second Enter falls to the pending guard,
    // not the empty-text one — pinning the claim-before-await ordering.
    await fireEvent.input(ta, { target: { value: 'once' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    resolveSettings({ local_author: 'Dana' })
    await flush()
    expect(addTaskComment).toHaveBeenCalledTimes(1)
    expect(addTaskComment).toHaveBeenCalledWith('task-1', 'once', 'Dana')
  })

  it('drops a second Enter during the reply author re-read (no double post)', async () => {
    const { getPluginSettings, resolveSettings } = deferredSettings()
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const ctx = makeCtx({
      addTaskComment,
      getPluginSettings,
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'parent comment',
          author: 'alice',
          timestamp: '2026-07-01T09:00:00',
          parent_id: 'task-1'
        })
      ])
    })
    mount({ ctx })
    await flush()
    const replyBtn = screen.getAllByLabelText('Reply to comment')[0]!
    await fireEvent.click(replyBtn)
    await flush()
    const replyTa = screen.getByLabelText('Reply text') as HTMLTextAreaElement
    await fireEvent.input(replyTa, { target: { value: 'reply once' } })
    await fireEvent.keyDown(replyTa, { key: 'Enter' })
    await fireEvent.input(replyTa, { target: { value: 'reply once' } })
    await fireEvent.keyDown(replyTa, { key: 'Enter' })
    resolveSettings({})
    await flush()
    expect(addTaskComment).toHaveBeenCalledTimes(1)
    expect(addTaskComment).toHaveBeenCalledWith(
      'task-1',
      'reply once',
      undefined,
      'c1'
    )
  })

  it('disables reply Cancel while the reply is in flight', async () => {
    // Canceling mid-flight would close the composer the failure path
    // restores its draft into; the button (and the Escape route through
    // cancelReply) must hold until the post settles.
    const { getPluginSettings, resolveSettings } = deferredSettings()
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const ctx = makeCtx({
      addTaskComment,
      getPluginSettings,
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'parent comment',
          author: 'alice',
          timestamp: '2026-07-01T09:00:00',
          parent_id: 'task-1'
        })
      ])
    })
    mount({ ctx })
    await flush()
    await fireEvent.click(screen.getAllByLabelText('Reply to comment')[0]!)
    await flush()
    const replyTa = screen.getByLabelText('Reply text') as HTMLTextAreaElement
    await fireEvent.input(replyTa, { target: { value: 'reply' } })
    await fireEvent.keyDown(replyTa, { key: 'Enter' })
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
    // Per-comment Reply buttons are gated too: starting another comment's
    // reply mid-flight would move the composer the settle path reuses.
    expect(screen.getAllByLabelText('Reply to comment')[0]!).toBeDisabled()
    resolveSettings({})
    await flush()
    // Success closes the composer (replyToId resets) — the Cancel button
    // is gone, not merely re-enabled.
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull()
    expect(addTaskComment).toHaveBeenCalledTimes(1)
  })

  it('disables Reply on a comment whose own post is still in flight', async () => {
    // The optimistic entry renders with pending: true; replying to it
    // would carry parentId = "optimistic-…", which the id swap strands:
    // the composer vanishes and a failed reply restores the draft into
    // an id that no longer renders.
    let resolveComment!: (id: string) => void
    const pendingComment = new Promise<string>((res) => {
      resolveComment = res
    })
    const addTaskComment = vi.fn(() => pendingComment)
    const ctx = makeCtx({ addTaskComment })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'top-level' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()
    // The optimistic entry is the only comment; its Reply button holds
    // until the real id replaces the optimistic one.
    expect(screen.getAllByLabelText('Reply to comment')[0]!).toBeDisabled()
    resolveComment('real-uuid')
    await flush()
    expect(screen.getAllByLabelText('Reply to comment')[0]!).toBeEnabled()
  })

  it('a failed reply re-opens the composer on the original parent with the draft restored (Escape mid-flight is a no-op)', async () => {
    // Deferred-then-rejecting post: the failure path must restore the
    // draft INTO the composer even though Escape was pressed mid-flight —
    // cancelReply's pending guard kept it open — and re-anchor it to the
    // original parent.
    let rejectComment!: (e: Error) => void
    const pendingComment = new Promise<string>((_, rej) => {
      rejectComment = rej
    })
    const addTaskComment = vi.fn(() => pendingComment)
    const ctx = makeCtx({
      addTaskComment,
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'parent comment',
          author: 'alice',
          timestamp: '2026-07-01T09:00:00',
          parent_id: 'task-1'
        }),
        makeBlock({
          id: 'c2',
          clean_text: 'second comment',
          author: 'bob',
          timestamp: '2026-07-01T10:00:00',
          parent_id: 'task-1'
        })
      ])
    })
    mount({ ctx })
    await flush()
    await fireEvent.click(screen.getAllByLabelText('Reply to comment')[0]!)
    await flush()
    const replyTa = screen.getByLabelText('Reply text') as HTMLTextAreaElement
    await fireEvent.input(replyTa, { target: { value: 'flaky reply' } })
    await fireEvent.keyDown(replyTa, { key: 'Enter' })
    // Escape mid-flight: cancelReply must hold (guard), composer stays.
    await fireEvent.keyDown(replyTa, { key: 'Escape' })
    expect(screen.getByTestId('reply-composer')).toBeInTheDocument()
    rejectComment(new Error('post failed'))
    await flush()
    // Failure restores the draft into the composer re-anchored to the
    // original parent (c1's article), not the second comment's.
    const composer = screen.getByTestId('reply-composer')
    const hostArticle = composer.closest('article')
    expect(hostArticle).not.toBeNull()
    expect(hostArticle!.textContent).toContain('parent comment')
    expect(
      (screen.getByLabelText('Reply text') as HTMLTextAreaElement).value
    ).toBe('flaky reply')
    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(addTaskComment).toHaveBeenCalledWith(
      'task-1',
      'flaky reply',
      undefined,
      'c1'
    )
  })

  it('keystrokes typed during the author re-read survive the post', async () => {
    const { getPluginSettings, resolveSettings } = deferredSettings()
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const ctx = makeCtx({ addTaskComment, getPluginSettings })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    await fireEvent.input(ta, { target: { value: 'first' } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    // The composer stays editable while the re-read pends; what the user
    // types now is the start of the next draft, not collateral to wipe.
    await fireEvent.input(ta, { target: { value: 'second' } })
    resolveSettings({})
    await flush()
    expect(addTaskComment).toHaveBeenCalledWith('task-1', 'first', undefined)
    expect(ta.value).toBe('second')
  })

  it('block:changed event triggers a reload of the thread', async () => {
    const off = vi.fn()
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

  // #1153: HTML markup in a comment body must never display verbatim. All
  // stripping below happens at render time only — the composer keeps sending
  // exactly what the user typed and the stored NOTE body is untouched.
  it('renders <b>ship it</b> as plain text with no tag text in the DOM', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: '<b>ship it</b>',
          author: 'a',
          timestamp: '2026-07-01T09:00:00'
        })
      ])
    })
    const { container } = mount({ ctx })
    await flush()
    expect(screen.getByText('ship it')).toBeInTheDocument()
    expect(container.textContent).not.toContain('<b>')
    // Belt-and-braces: no tag was parsed into an element either (no
    // {@html} path anywhere in the thread).
    expect(container.querySelector('[role="comment"] b')).toBeNull()
  })

  it('renders block/nested markup as readable text', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: '<div>Please <b>review</b> the PR</div>',
          author: 'a',
          timestamp: '2026-07-01T09:00:00'
        })
      ])
    })
    const { container } = mount({ ctx })
    await flush()
    expect(screen.getByText('Please review the PR')).toBeInTheDocument()
    expect(container.textContent).not.toContain('<div>')
    expect(container.textContent).not.toContain('<b>')
  })

  it('strips markup from replies too (shared commentBody snippet)', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'top',
          clean_text: 'top-level',
          author: 'alice',
          timestamp: '2026-07-01T09:00:00',
          parent_id: 'task-1',
          depth: 1
        }),
        makeBlock({
          id: 'r1',
          clean_text: '<div>Please <b>review</b> the PR</div>',
          author: 'bob',
          timestamp: '2026-07-01T10:00:00',
          parent_id: 'top',
          depth: 2
        })
      ])
    })
    const { container } = mount({ ctx })
    await flush()
    const reply = container.querySelector('[role="comment"].ml-4')
    expect(reply?.textContent).toContain('Please review the PR')
    expect(reply?.textContent).not.toContain('<b>')
  })

  it('strips markup in the drawer layout as well (same snippet)', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: '<b>ship it</b>',
          author: 'a',
          timestamp: '2026-07-01T09:00:00'
        })
      ])
    })
    const { container } = mount({ ctx, layout: 'drawer' })
    await flush()
    expect(await screen.findByTestId('comment-list-scroll')).toBeInTheDocument()
    expect(screen.getByText('ship it')).toBeInTheDocument()
    expect(container.textContent).not.toContain('<b>')
  })

  it('preserves a lone < in prose (count < limit stays verbatim)', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'count < limit',
          author: 'a',
          timestamp: '2026-07-01T09:00:00'
        })
      ])
    })
    const { container } = mount({ ctx })
    await flush()
    expect(screen.getByText('count < limit')).toBeInTheDocument()
    expect(container.textContent).toContain('count < limit')
  })

  it('injects no element and shows no tag text for hostile payloads', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'nice <img src=x onerror=alert(1)> job',
          author: 'a',
          timestamp: '2026-07-01T09:00:00'
        }),
        makeBlock({
          id: 'c2',
          clean_text: '<script>alert(1)</script>',
          author: 'b',
          timestamp: '2026-07-01T10:00:00'
        })
      ])
    })
    const { container } = mount({ ctx })
    await flush()
    expect(container.querySelectorAll('img, script')).toHaveLength(0)
    expect(container.textContent).not.toContain('<img')
    expect(container.textContent).not.toContain('<script')
  })

  it('composing still sends the raw typed text (strip is display-only)', async () => {
    const addTaskComment = vi.fn().mockResolvedValue('new-uuid')
    const ctx = makeCtx({ addTaskComment })
    mount({ ctx })
    await flush()
    const ta = screen.getByLabelText('Comment text') as HTMLTextAreaElement
    const raw = 'count < limit and <b>bold</b>'
    await fireEvent.input(ta, { target: { value: raw } })
    await fireEvent.keyDown(ta, { key: 'Enter' })
    await flush()
    // The stored content must be exactly what the user typed — stripping
    // lives in the render path, not the write path.
    expect(addTaskComment).toHaveBeenCalledWith('task-1', raw, undefined)
  })

  it('filters non-NOTE blocks out of the thread', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'a real comment',
          author: 'alice',
          timestamp: '2026-07-01T09:00:00',
          parent_id: 'task-1'
        }),
        makeBlock({
          id: 'child-task',
          type: 'TASK',
          clean_text: 'nested task should not render',
          parent_id: 'task-1'
        })
      ])
    })
    mount({ ctx })
    await flush()
    expect(screen.getByText('a real comment')).toBeInTheDocument()
    expect(
      screen.queryByText('nested task should not render')
    ).not.toBeInTheDocument()
    expect(document.querySelectorAll('[role="comment"]')).toHaveLength(1)
  })

  it('nests replies under their parent and flattens deeper nesting', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'top',
          clean_text: 'top-level',
          author: 'alice',
          timestamp: '2026-07-01T09:00:00',
          parent_id: 'task-1',
          depth: 1
        }),
        makeBlock({
          id: 'r1',
          clean_text: 'first reply',
          author: 'bob',
          timestamp: '2026-07-01T10:00:00',
          parent_id: 'top',
          depth: 2
        }),
        makeBlock({
          id: 'r2',
          clean_text: 'nested reply flattened',
          author: 'carol',
          timestamp: '2026-07-01T11:00:00',
          parent_id: 'r1',
          depth: 3
        }),
        makeBlock({
          id: 'other',
          clean_text: 'sibling top',
          author: 'dana',
          timestamp: '2026-07-01T12:00:00',
          parent_id: 'task-1',
          depth: 1
        })
      ])
    })
    mount({ ctx })
    await flush()
    const articles = document.querySelectorAll('[role="comment"]')
    expect(articles).toHaveLength(4)
    expect(screen.getByText('top-level')).toBeInTheDocument()
    expect(screen.getByText('first reply')).toBeInTheDocument()
    expect(screen.getByText('nested reply flattened')).toBeInTheDocument()
    expect(screen.getByText('sibling top')).toBeInTheDocument()
    // Nested replies get the indent class; top-level do not.
    const nested = document.querySelectorAll('[role="comment"].ml-4')
    expect(nested.length).toBe(2)
  })

  it('Reply button opens inline composer and posts with parentCommentId', async () => {
    const addTaskComment = vi.fn().mockResolvedValue('reply-uuid')
    const ctx = makeCtx({
      addTaskComment,
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'parent comment',
          author: 'alice',
          timestamp: '2026-07-01T09:00:00',
          parent_id: 'task-1'
        })
      ])
    })
    mount({ ctx })
    await flush()

    const replyBtn = screen.getByLabelText('Reply to comment')
    await fireEvent.click(replyBtn)
    await flush()

    const replyTa = screen.getByLabelText('Reply text') as HTMLTextAreaElement
    expect(replyTa).toBeInTheDocument()
    await fireEvent.input(replyTa, { target: { value: 'my reply' } })
    await fireEvent.keyDown(replyTa, { key: 'Enter' })
    await flush()

    expect(addTaskComment).toHaveBeenCalledWith(
      'task-1',
      'my reply',
      undefined,
      'c1'
    )
    expect(screen.getByText('my reply')).toBeInTheDocument()
  })

  it('Escape dismisses only the reply composer and restores trigger focus', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'parent comment',
          author: 'alice',
          timestamp: '2026-07-01T09:00:00',
          parent_id: 'task-1'
        })
      ])
    })
    mount({ ctx })
    await flush()

    const replyButton = screen.getByLabelText('Reply to comment')
    await fireEvent.click(replyButton)
    const composer = screen.getByLabelText('Reply text')
    expect(document.activeElement).toBe(composer)

    await fireEvent.keyDown(composer, { key: 'Escape' })
    await flush()
    expect(screen.queryByLabelText('Reply text')).toBeNull()
    expect(document.activeElement).toBe(replyButton)
  })
})

describe('comment predicates (display/edit asymmetry)', () => {
  it('isNoteBlock is a type test only', () => {
    expect(isNoteBlock({ type: 'NOTE' })).toBe(true)
    expect(isNoteBlock({ type: 'note' })).toBe(true)
    expect(isNoteBlock({ type: 'TASK' })).toBe(false)
    expect(isNoteBlock({})).toBe(false)
  })

  it.each([
    ['author only', { type: 'NOTE', author: 'Dana' }, true],
    [
      'timestamp only',
      { type: 'NOTE', timestamp: '2026-07-07T14:22:00' },
      true
    ],
    [
      'author and timestamp',
      { type: 'NOTE', author: 'Dana', timestamp: '2026-07-07T14:22:00' },
      true
    ],
    ['neither token (prose note)', { type: 'NOTE' }, false],
    [
      'whitespace attribution',
      { type: 'NOTE', author: '  ', timestamp: '\t' },
      false
    ],
    [
      'attributed TASK is not a comment',
      { type: 'TASK', author: 'Dana', timestamp: '2026-07-07T14:22:00' },
      false
    ]
  ])('isCommentBlock: %s', (_name, block, want) => {
    expect(isCommentBlock(block)).toBe(want)
  })

  it('partitionComments keeps comments and content in original order', () => {
    const part = partitionComments([
      { type: 'NOTE', clean_text: 'prose' },
      { type: 'NOTE', author: 'Dana', clean_text: 'comment one' },
      { type: 'TASK', clean_text: 'sub-task' },
      {
        type: 'NOTE',
        timestamp: '2026-07-07T14:22:00',
        clean_text: 'comment two'
      }
    ])
    expect(part.content.map((b) => b.clean_text)).toEqual(['prose', 'sub-task'])
    expect(part.comments.map((b) => b.clean_text)).toEqual([
      'comment one',
      'comment two'
    ])
  })

  it('the thread still renders legacy token-less NOTEs (display side stays type-only)', async () => {
    // Regression guard: the editor drops attributed comments, but the thread
    // must keep showing every NOTE descendant — including legacy ones, which
    // render as Unknown/Undated.
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([
        makeBlock({
          id: 'c1',
          clean_text: 'attributed comment',
          author: 'alice',
          timestamp: '2026-07-01T09:00:00'
        }),
        makeBlock({ id: 'legacy', clean_text: 'legacy prose note' })
      ])
    })
    mount({ ctx })
    await flush()
    expect(document.querySelectorAll('[role="comment"]').length).toBe(2)
    expect(screen.getByText('attributed comment')).toBeInTheDocument()
    expect(screen.getByText('legacy prose note')).toBeInTheDocument()
  })
})
