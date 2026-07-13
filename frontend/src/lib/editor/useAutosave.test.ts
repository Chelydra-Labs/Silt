import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { AutosaveManager } from './useAutosave'
import type { AutosaveDeps } from './useAutosave'

// Mock the Wails IPC binding.
vi.mock('../../../bindings/silt/app.js', () => ({
  SaveFileBlocks: vi.fn().mockResolvedValue(undefined)
}))

// Mock the perf budget utility.
vi.mock('../perf/frame-budget', () => ({
  measureFrameBudget: vi.fn((_label: string, fn: () => unknown) => fn())
}))

// Mock the converters.
vi.mock('./converters', () => ({
  docToBlocks: vi.fn(() => [{ id: 'block-1', type: 'NOTE', rawText: 'test' }])
}))

// Mock the notification store.
vi.mock('../../notifications/store.svelte', () => ({
  pushNotification: vi.fn()
}))

// Mock the plugin events.
vi.mock('../../plugins/events', () => ({
  dispatch: vi.fn()
}))

function makeDeps(overrides: Partial<AutosaveDeps> = {}): AutosaveDeps {
  return {
    getEditor: () => ({ getJSON: () => ({ type: 'doc', content: [] }) }) as any,
    getNotebook: () => 'Work',
    getSection: () => 'Journal',
    getPage: () => '2026-06-22',
    getDelay: () => 100,
    onUpdate: vi.fn(),
    onStateChange: vi.fn(),
    onSaveStateChange: vi.fn(),
    ...overrides
  }
}

describe('AutosaveManager', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('saves after the configured delay', async () => {
    const { SaveFileBlocks } = await import('../../../bindings/silt/app.js')
    const deps = makeDeps()
    const autosave = new AutosaveManager(deps)

    autosave.trigger()

    // Not yet saved.
    expect(SaveFileBlocks).not.toHaveBeenCalled()

    // Advance past the delay.
    await vi.advanceTimersByTimeAsync(150)

    expect(SaveFileBlocks).toHaveBeenCalledWith(
      'Work',
      'Journal',
      '2026-06-22',
      expect.any(Array)
    )
    expect(deps.onStateChange).toHaveBeenCalledWith(false, null)
    expect(deps.onUpdate).toHaveBeenCalled()
  })

  it('debounces rapid triggers', async () => {
    const { SaveFileBlocks } = await import('../../../bindings/silt/app.js')
    const deps = makeDeps()
    const autosave = new AutosaveManager(deps)

    autosave.trigger()
    autosave.trigger()
    autosave.trigger()

    await vi.advanceTimersByTimeAsync(150)

    // Only one save despite three triggers.
    expect(SaveFileBlocks).toHaveBeenCalledTimes(1)
  })

  it('flush() saves immediately', async () => {
    const { SaveFileBlocks } = await import('../../../bindings/silt/app.js')
    const deps = makeDeps()
    const autosave = new AutosaveManager(deps)

    autosave.trigger()
    await autosave.flush()

    expect(SaveFileBlocks).toHaveBeenCalledTimes(1)
  })

  it('flush() is a no-op when no save is pending', async () => {
    const { SaveFileBlocks } = await import('../../../bindings/silt/app.js')
    const deps = makeDeps()
    const autosave = new AutosaveManager(deps)

    await autosave.flush()

    expect(SaveFileBlocks).not.toHaveBeenCalled()
  })

  it('reports errors via onStateChange', async () => {
    const { SaveFileBlocks } = await import('../../../bindings/silt/app.js')
    vi.mocked(SaveFileBlocks).mockRejectedValueOnce(new Error('disk full'))
    const deps = makeDeps()
    const autosave = new AutosaveManager(deps)

    autosave.trigger()
    await vi.advanceTimersByTimeAsync(150)

    // Wait for the async save to settle.
    await vi.advanceTimersByTimeAsync(0)

    expect(deps.onStateChange).toHaveBeenCalledWith(true, 'disk full')
  })

  it('markClean() resets state', () => {
    const deps = makeDeps()
    const autosave = new AutosaveManager(deps)

    autosave.markClean()

    expect(deps.onStateChange).toHaveBeenCalledWith(false, null)
  })

  describe('save phase state machine (#546)', () => {
    it('emits pending on trigger (debounce, not Saving…)', () => {
      const deps = makeDeps()
      const autosave = new AutosaveManager(deps)

      autosave.trigger()

      expect(deps.onSaveStateChange).toHaveBeenCalledWith({
        phase: 'pending',
        dirty: true,
        error: null
      })
    })

    it('emits saving → saved across a successful save', async () => {
      const deps = makeDeps()
      const autosave = new AutosaveManager(deps)

      autosave.trigger()
      // Advance past the debounce; save() starts (saving), IPC resolves, then
      // the SAVING_MIN_DISPLAY_MS floor elapses before 'saved'.
      await vi.advanceTimersByTimeAsync(150)
      await vi.advanceTimersByTimeAsync(0)
      await vi.advanceTimersByTimeAsync(300)

      const phases = (
        deps.onSaveStateChange as unknown as ReturnType<typeof vi.fn>
      ).mock.calls.map((c: any[]) => c[0].phase)
      expect(phases).toEqual(['pending', 'saving', 'saved'])
    })

    it('reverts saved → idle after the hold window', async () => {
      const deps = makeDeps()
      const autosave = new AutosaveManager(deps)

      autosave.trigger()
      await vi.advanceTimersByTimeAsync(150)
      await vi.advanceTimersByTimeAsync(0)
      await vi.advanceTimersByTimeAsync(300) // SAVING_MIN_DISPLAY_MS

      const phasesBefore = (
        deps.onSaveStateChange as unknown as ReturnType<typeof vi.fn>
      ).mock.calls.map((c: any[]) => c[0].phase)
      expect(phasesBefore).toContain('saved')

      // Advance past SAVED_HOLD_MS (2000). The hold timer fires → idle.
      await vi.advanceTimersByTimeAsync(2000)

      const last = (
        deps.onSaveStateChange as unknown as ReturnType<typeof vi.fn>
      ).mock.calls.at(-1)![0]
      expect(last.phase).toBe('idle')
      expect(last.dirty).toBe(false)
    })

    it('cancels the saved→idle revert when a new edit arrives', async () => {
      const deps = makeDeps()
      const autosave = new AutosaveManager(deps)

      autosave.trigger()
      await vi.advanceTimersByTimeAsync(150)
      await vi.advanceTimersByTimeAsync(0)
      await vi.advanceTimersByTimeAsync(300) // SAVING_MIN_DISPLAY_MS

      // A new edit mid-hold flips back to pending and cancels the revert.
      // markDirty() represents the dirty transition without scheduling a new
      // save, isolating the hold-cancellation from a second save cycle.
      autosave.markDirty()

      // Advance past SAVED_HOLD_MS; the revert timer was cancelled, so idle
      // must NOT fire.
      await vi.advanceTimersByTimeAsync(2000)

      const calls = (
        deps.onSaveStateChange as unknown as ReturnType<typeof vi.fn>
      ).mock.calls.map((c: any[]) => c[0].phase)
      expect(calls.at(-1)).toBe('pending')
      const savedIdx = calls.indexOf('saved')
      expect(calls.slice(savedIdx + 1)).not.toContain('idle')
    })

    it('holds saving for at least SAVING_MIN_DISPLAY_MS (non-blocking)', async () => {
      const deps = makeDeps()
      const autosave = new AutosaveManager(deps)

      autosave.trigger()
      await vi.advanceTimersByTimeAsync(150)
      await vi.advanceTimersByTimeAsync(0)
      // IPC done; onUpdate already fired; phase still 'saving' until floor.
      expect(deps.onUpdate).toHaveBeenCalled()
      let phases = (
        deps.onSaveStateChange as unknown as ReturnType<typeof vi.fn>
      ).mock.calls.map((c: any[]) => c[0].phase)
      expect(phases.at(-1)).toBe('saving')

      await vi.advanceTimersByTimeAsync(300)
      phases = (
        deps.onSaveStateChange as unknown as ReturnType<typeof vi.fn>
      ).mock.calls.map((c: any[]) => c[0].phase)
      expect(phases.at(-1)).toBe('saved')
    })

    it('emits error phase on save failure', async () => {
      const { SaveFileBlocks } = await import('../../../bindings/silt/app.js')
      vi.mocked(SaveFileBlocks).mockRejectedValueOnce(new Error('disk full'))
      const deps = makeDeps()
      const autosave = new AutosaveManager(deps)

      autosave.trigger()
      await vi.advanceTimersByTimeAsync(150)
      await vi.advanceTimersByTimeAsync(0)

      const last = (
        deps.onSaveStateChange as unknown as ReturnType<typeof vi.fn>
      ).mock.calls.at(-1)![0]
      expect(last.phase).toBe('error')
      expect(last.dirty).toBe(true)
      expect(last.error).toBe('disk full')
    })

    it('markClean() emits idle after a dirty state', () => {
      const deps = makeDeps()
      const autosave = new AutosaveManager(deps)

      // Put it in a non-idle state first so the idle transition is observable.
      autosave.trigger()
      expect(
        (deps.onSaveStateChange as unknown as ReturnType<typeof vi.fn>).mock
          .calls.length
      ).toBeGreaterThan(0)

      autosave.markClean()

      const last = (
        deps.onSaveStateChange as unknown as ReturnType<typeof vi.fn>
      ).mock.calls.at(-1)![0]
      expect(last).toEqual({ phase: 'idle', dirty: false, error: null })
    })

    it('dedupes consecutive identical states', () => {
      const deps = makeDeps()
      const autosave = new AutosaveManager(deps)

      autosave.trigger()
      autosave.trigger()

      // Both triggers emit the same 'pending' state; only the first emits.
      expect(deps.onSaveStateChange).toHaveBeenCalledTimes(1)
    })
  })

  it('does not save when editor is null', async () => {
    const { SaveFileBlocks } = await import('../../../bindings/silt/app.js')
    const deps = makeDeps({ getEditor: () => null })
    const autosave = new AutosaveManager(deps)

    autosave.trigger()
    await vi.advanceTimersByTimeAsync(150)

    expect(SaveFileBlocks).not.toHaveBeenCalled()
  })

  it('uses minimum delay of 50ms', async () => {
    const deps = makeDeps({ getDelay: () => 0 })
    const autosave = new AutosaveManager(deps)

    autosave.trigger()

    // At 30ms, should not have saved yet (min delay is 50ms).
    await vi.advanceTimersByTimeAsync(30)
    expect(deps.onUpdate).not.toHaveBeenCalled()

    // At 60ms, should have saved.
    await vi.advanceTimersByTimeAsync(30)
    expect(deps.onUpdate).toHaveBeenCalled()
  })

  it('reads current identity after a page rename (stale-capture regression)', async () => {
    const { SaveFileBlocks } = await import('../../../bindings/silt/app.js')
    let currentPage = 'OldPage'
    const deps = makeDeps({ getPage: () => currentPage })
    const autosave = new AutosaveManager(deps)

    // Save with the original page name.
    autosave.trigger()
    await vi.advanceTimersByTimeAsync(150)
    expect(SaveFileBlocks).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'OldPage',
      expect.any(Array)
    )

    // Simulate a rename: the getter now returns the new name.
    currentPage = 'RenamedPage'
    vi.mocked(SaveFileBlocks).mockClear()
    autosave.trigger()
    await vi.advanceTimersByTimeAsync(150)
    expect(SaveFileBlocks).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'RenamedPage',
      expect.any(Array)
    )
  })
})
