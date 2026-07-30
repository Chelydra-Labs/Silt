// TaskEditorModalHost owns the open/race/teardown lifecycle for the in-page
// task editor modal (#781). Its monotonic openSeq discards stale fetches, the
// same-task dedupe short-circuits a reopen, a ctx-null effect clears the task
// on vault switch, and the not-ready path emits an info notification. These
// branches are silent when broken, so they are exercised directly here. The
// dispatch side (pencil / Shift-Enter / context menu) is covered elsewhere;
// this file covers the host's consumption of silt:open-task-editor.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup } from '@testing-library/svelte'
import { waitFor } from '@testing-library/dom'
import { tick } from 'svelte'
import TaskSubEditorModalStub from '../../plugins/first-party/silt-tasks/components/TaskSubEditorModalStub.stub.svelte'
import { loadedPlugins } from '../../plugins/store.svelte'
import type { TaskDetail } from '../../plugins/first-party/silt-tasks/types'

const mocks = vi.hoisted(() => ({
  fetchTaskDetail: vi.fn(),
  pushNotification: vi.fn()
}))

vi.mock('../../plugins/context', () => ({
  // A truthy context is enough: fetchTaskDetail and the modal are mocked, so
  // nothing dereferences the real PluginContext surface.
  makePluginContext: vi.fn(() => ({ __ctx: true }))
}))
vi.mock('../../plugins/loader', () => ({
  getSessionToken: vi.fn(() => 'session-token')
}))
vi.mock('../../plugins/first-party/silt-tasks/query', () => ({
  fetchTaskDetail: mocks.fetchTaskDetail
}))
vi.mock('../../notifications/store.svelte', () => ({
  pushNotification: mocks.pushNotification
}))
vi.mock(
  '../../plugins/first-party/silt-tasks/components/TaskSubEditorModal.svelte',
  () => ({
    default: TaskSubEditorModalStub
  })
)

import TaskEditorModalHost from './TaskEditorModalHost.svelte'

const MODAL = 'task-sub-editor-modal'

function openEditor(blockId: string): void {
  window.dispatchEvent(
    new CustomEvent('silt:open-task-editor', { detail: { blockId } })
  )
}

// Minimal TaskDetail — the host only reads id/notebook/section/page/
// clean_content; the rest is opaque to this lifecycle test.
function taskDetail(id: string): TaskDetail {
  return {
    id,
    source: 'vault',
    notebook: 'Work',
    section: 'Projects',
    page: 'Plan',
    file_date: '',
    clean_content: `- [ ] Task ${id}`,
    status: 'TODO',
    owner: '',
    start_date: '',
    due_date: '',
    priority: 0,
    pinned: false,
    progress: 0,
    recurrence: '',
    comments_count: 0,
    links_count: 0,
    created_at: ''
  } as unknown as TaskDetail
}

describe('TaskEditorModalHost open lifecycle (#781)', () => {
  beforeEach(() => {
    cleanup()
    mocks.fetchTaskDetail.mockReset()
    mocks.pushNotification.mockReset()
    loadedPlugins.loadersReady = true
  })

  afterEach(() => {
    cleanup()
    loadedPlugins.loadersReady = false
  })

  it('opens the modal for the requested task on silt:open-task-editor', async () => {
    mocks.fetchTaskDetail.mockResolvedValue(taskDetail('block-1'))
    render(TaskEditorModalHost)
    await tick()

    openEditor('block-1')

    const modal = await screen.findByTestId(MODAL)
    expect(modal.getAttribute('data-block-id')).toBe('block-1')
  })

  it('lands on the most-recent task when two opens race (stale fetch discarded)', async () => {
    // Each open returns a pending promise resolved out of order below.
    const resolvers: Array<(v: TaskDetail | null) => void> = []
    mocks.fetchTaskDetail.mockImplementation(
      () =>
        new Promise<TaskDetail | null>((resolve) => {
          resolvers.push(resolve)
        })
    )
    render(TaskEditorModalHost)
    await tick()

    // Open A then B before either resolves. B is the user's last click.
    openEditor('block-A') // fetch call 0 → resolvers[0]
    openEditor('block-B') // fetch call 1 → resolvers[1]
    await tick()

    // Resolve B first (lands), then A last. Without the openSeq guard, A's
    // later resolution would overwrite openTask with the stale task.
    resolvers[1](taskDetail('block-B'))
    await tick()
    resolvers[0](taskDetail('block-A'))
    await tick()

    const modal = await screen.findByTestId(MODAL)
    expect(modal.getAttribute('data-block-id')).toBe('block-B')
  })

  it('clears the open task when the context drops on vault switch', async () => {
    mocks.fetchTaskDetail.mockResolvedValue(taskDetail('block-1'))
    render(TaskEditorModalHost)
    await tick()

    openEditor('block-1')
    await screen.findByTestId(MODAL)

    // Vault switch: loaders tear down, ctx goes null, the host must clear the
    // stale task so it does not re-render once the new context is ready.
    loadedPlugins.loadersReady = false
    await waitFor(() => {
      expect(screen.queryByTestId(MODAL)).toBeNull()
    })
  })

  it('emits an info notification and skips the fetch when the plugin is not ready', async () => {
    loadedPlugins.loadersReady = false
    render(TaskEditorModalHost)
    await tick()

    openEditor('block-9')

    await waitFor(() => {
      expect(mocks.pushNotification).toHaveBeenCalledWith(
        expect.objectContaining({ kind: 'info' })
      )
    })
    expect(mocks.fetchTaskDetail).not.toHaveBeenCalled()
    expect(screen.queryByTestId(MODAL)).toBeNull()
  })
})
