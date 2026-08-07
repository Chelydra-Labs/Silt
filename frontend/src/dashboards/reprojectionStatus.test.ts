import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'

// Cold-state seed binding. The progress event path does not touch the
// bindings — only attach()'s one-shot GetTypesReprojectionStatus call does.
const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    GetTypesReprojectionStatus: vi.fn()
  })
)
vi.mock('$silt-app', () => appMocks)

// Capture Events.On registrations so the `types:reprojection:progress`
// handler can be fired in-test (mirrors pageTypeState.test.ts's approach).
const eventsHandlers = {} as Record<string, (ev?: { data?: unknown }) => void>
const disposeSpy = vi.fn()
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn((name: string, handler: (ev?: { data?: unknown }) => void) => {
      eventsHandlers[name] = handler
      return () => disposeSpy()
    })
  }
}))

import { createReprojectionStatus } from './reprojectionStatus.svelte'

const PROGRESS_EVENT = 'types:reprojection:progress'

beforeEach(() => {
  appMocks.GetTypesReprojectionStatus.mockReset()
  appMocks.GetTypesReprojectionStatus.mockResolvedValue({
    active: false,
    processed: 0,
    total: 0
  })
  disposeSpy.mockReset()
})

afterEach(() => {
  for (const k of Object.keys(eventsHandlers)) delete eventsHandlers[k]
})

describe('createReprojectionStatus', () => {
  it('seeds from GetTypesReprojectionStatus on attach (idle)', async () => {
    appMocks.GetTypesReprojectionStatus.mockResolvedValue({
      active: false,
      processed: 0,
      total: 0
    })
    const status = createReprojectionStatus()
    const dispose = status.attach()
    await tick()

    expect(appMocks.GetTypesReprojectionStatus).toHaveBeenCalledTimes(1)
    expect(status.active).toBe(false)
    expect(status.processed).toBe(0)
    expect(status.total).toBe(0)
    dispose()
  })

  it('seeds from GetTypesReprojectionStatus on attach (in-flight batch)', async () => {
    appMocks.GetTypesReprojectionStatus.mockResolvedValue({
      active: true,
      processed: 3,
      total: 10
    })
    const status = createReprojectionStatus()
    const dispose = status.attach()
    await vi.waitFor(() => {
      expect(status.active).toBe(true)
    })

    expect(status.processed).toBe(3)
    expect(status.total).toBe(10)
    dispose()
  })

  it('a running progress event sets active + counts', async () => {
    const status = createReprojectionStatus()
    const dispose = status.attach()
    await tick()

    eventsHandlers[PROGRESS_EVENT]?.({
      data: { state: 'running', processed: 7, total: 20 }
    })
    await tick()

    expect(status.active).toBe(true)
    expect(status.processed).toBe(7)
    expect(status.total).toBe(20)
    dispose()
  })

  it('intermediate running updates advance processed', async () => {
    const status = createReprojectionStatus()
    const dispose = status.attach()
    await tick()

    eventsHandlers[PROGRESS_EVENT]?.({
      data: { state: 'running', processed: 0, total: 4 }
    })
    await tick()
    expect(status.active).toBe(true)
    expect(status.processed).toBe(0)

    eventsHandlers[PROGRESS_EVENT]?.({
      data: { state: 'running', processed: 2, total: 4 }
    })
    await tick()
    expect(status.active).toBe(true)
    expect(status.processed).toBe(2)
    dispose()
  })

  it('a done event clears active and pins processed to total', async () => {
    const status = createReprojectionStatus()
    const dispose = status.attach()
    await tick()

    eventsHandlers[PROGRESS_EVENT]?.({
      data: { state: 'running', processed: 0, total: 5 }
    })
    await tick()
    expect(status.active).toBe(true)

    eventsHandlers[PROGRESS_EVENT]?.({
      data: { state: 'done', processed: 5, total: 5 }
    })
    await tick()

    expect(status.active).toBe(false)
    expect(status.processed).toBe(5)
    expect(status.total).toBe(5)
    dispose()
  })

  it('dispose detaches the event handler', async () => {
    const status = createReprojectionStatus()
    const dispose = status.attach()
    await tick()

    dispose()
    expect(disposeSpy).toHaveBeenCalledTimes(1)
  })

  it('a malformed event payload is tolerated (no crash, no state change)', async () => {
    const status = createReprojectionStatus()
    const dispose = status.attach()
    await tick()

    eventsHandlers[PROGRESS_EVENT]?.({ data: undefined })
    await tick()
    expect(status.active).toBe(false)
    expect(status.processed).toBe(0)
    expect(status.total).toBe(0)
    dispose()
  })

  it('cold-state IPC failure leaves the store idle (best-effort seed)', async () => {
    appMocks.GetTypesReprojectionStatus.mockRejectedValue(new Error('ipc down'))
    const status = createReprojectionStatus()
    const dispose = status.attach()
    await tick()

    // The store stays idle; the live event still drives updates.
    expect(status.active).toBe(false)
    expect(status.processed).toBe(0)
    expect(status.total).toBe(0)
    dispose()
  })
})
