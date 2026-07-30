/**
 * Tests for the one-slot pending queue in the custom-dictionary store (#822).
 *
 * Before the fix, `add`/`remove`/`importFile`/`exportFile` were guarded by
 * `if (busy) return`, so a second action fired during an in-flight one was
 * silently dropped — no effect, no error, no feedback. The store now captures
 * the most recent action in a single pending slot and runs it when the
 * in-flight action resolves (last-write-wins on a third call). These tests
 * drive the queue with controlled (deferred) IPC promises so an in-flight
 * action can be held unresolved at will.
 */
import { describe, it, expect, beforeEach, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  GetCustomDictionary: vi.fn(),
  AddCustomDictionaryWord: vi.fn(),
  RemoveCustomDictionaryWord: vi.fn(),
  PickCustomDictionaryExportPath: vi.fn(),
  PickCustomDictionaryImportFile: vi.fn(),
  ExportCustomDictionary: vi.fn(),
  ImportCustomDictionary: vi.fn(),
  mirrorCustomDictionary: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    GetCustomDictionary: mocks.GetCustomDictionary,
    AddCustomDictionaryWord: mocks.AddCustomDictionaryWord,
    RemoveCustomDictionaryWord: mocks.RemoveCustomDictionaryWord,
    PickCustomDictionaryExportPath: mocks.PickCustomDictionaryExportPath,
    PickCustomDictionaryImportFile: mocks.PickCustomDictionaryImportFile,
    ExportCustomDictionary: mocks.ExportCustomDictionary,
    ImportCustomDictionary: mocks.ImportCustomDictionary
  })
)

// Mirror target is a pure setter on the settings snapshot; mock it so the
// store's only observable side effect is the call itself, decoupled from the
// real settings store (and its @wailsio/runtime dependency).
vi.mock('../../../settings/store.svelte', () => ({
  mirrorCustomDictionary: mocks.mirrorCustomDictionary
}))

import { customDictionary, _resetForTests } from './customDictionary.svelte'

/** Controlled promise so a test can hold an IPC call unresolved at will. */
function deferred<T>(): {
  promise: Promise<T>
  resolve: (v: T) => void
  reject: (e: unknown) => void
} {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('customDictionary one-slot pending queue (#822)', () => {
  beforeEach(() => {
    _resetForTests()
    Object.values(mocks).forEach((m) => m.mockReset())
  })

  it('queues add during an in-flight importFile, then applies it (motivating scenario)', async () => {
    // Hold the import in flight via its merge IPC.
    const importDef = deferred<{ added: number; skipped: number }>()
    mocks.PickCustomDictionaryImportFile.mockResolvedValue('/vocab.txt')
    mocks.ImportCustomDictionary.mockReturnValue(importDef.promise)
    mocks.GetCustomDictionary.mockResolvedValue(['apple'])

    const importP = customDictionary.importFile()
    // The synchronous prefix already acquired busy up to the pending IPC.
    expect(customDictionary.busy).toBe(true)

    // Second action while busy: queued, not dropped.
    mocks.AddCustomDictionaryWord.mockResolvedValue(['apple', 'banana'])
    customDictionary.add('banana')
    expect(mocks.AddCustomDictionaryWord).not.toHaveBeenCalled()

    // Resolve the import; the queued add is drained and runs.
    importDef.resolve({ added: 1, skipped: 0 })
    await importP

    await vi.waitFor(() => {
      expect(mocks.AddCustomDictionaryWord).toHaveBeenCalledWith('banana')
      expect(customDictionary.words).toEqual(['apple', 'banana'])
    })
    expect(mocks.mirrorCustomDictionary).toHaveBeenCalledWith([
      'apple',
      'banana'
    ])
    expect(customDictionary.busy).toBe(false)
  })

  it('queues remove during an in-flight add, then applies it', async () => {
    const addDef = deferred<string[]>()
    mocks.AddCustomDictionaryWord.mockReturnValue(addDef.promise)

    const addP = customDictionary.add('foo')
    expect(customDictionary.busy).toBe(true)

    mocks.RemoveCustomDictionaryWord.mockResolvedValue([])
    customDictionary.remove('bar')
    expect(mocks.RemoveCustomDictionaryWord).not.toHaveBeenCalled()

    addDef.resolve(['foo'])
    await addP

    await vi.waitFor(() => {
      expect(mocks.RemoveCustomDictionaryWord).toHaveBeenCalledWith('bar')
    })
    expect(customDictionary.words).toEqual([])
    expect(mocks.mirrorCustomDictionary).toHaveBeenCalledWith([])
    expect(customDictionary.busy).toBe(false)
  })

  it('overwrites the pending slot on a third call (last-write-wins)', async () => {
    const firstDef = deferred<string[]>()
    mocks.AddCustomDictionaryWord.mockReturnValue(firstDef.promise)

    const firstP = customDictionary.add('first')
    expect(customDictionary.busy).toBe(true)

    // Queue a remove, then overwrite with an add before the in-flight resolves.
    customDictionary.remove('dropped')
    mocks.AddCustomDictionaryWord.mockResolvedValue(['first', 'second'])
    customDictionary.add('second')

    firstDef.resolve(['first'])
    await firstP

    // Latest pending action (add 'second') runs; the remove never does.
    await vi.waitFor(() => {
      expect(mocks.AddCustomDictionaryWord).toHaveBeenCalledWith('second')
      expect(customDictionary.words).toEqual(['first', 'second'])
    })
    expect(mocks.RemoveCustomDictionaryWord).not.toHaveBeenCalled()
    expect(customDictionary.busy).toBe(false)
  })

  it('surfaces an error from the drained action and releases busy', async () => {
    const addDef = deferred<string[]>()
    mocks.AddCustomDictionaryWord.mockReturnValue(addDef.promise)

    const addP = customDictionary.add('x')
    expect(customDictionary.busy).toBe(true)

    // The drained remove rejects.
    mocks.RemoveCustomDictionaryWord.mockRejectedValue(
      new Error('network timeout')
    )
    customDictionary.remove('y')

    addDef.resolve(['x'])
    await addP

    // friendlyPackError maps network failures; the error must not be swallowed.
    await vi.waitFor(() => {
      expect(customDictionary.error).toMatch(/download failed/i)
    })
    expect(customDictionary.busy).toBe(false)
  })

  it('releases busy and runs the queued action when the in-flight action rejects', async () => {
    const importDef = deferred<{ added: number; skipped: number }>()
    mocks.PickCustomDictionaryImportFile.mockResolvedValue('/vocab.txt')
    mocks.ImportCustomDictionary.mockReturnValue(importDef.promise)
    mocks.GetCustomDictionary.mockResolvedValue(['apple'])

    const importP = customDictionary.importFile()
    expect(customDictionary.busy).toBe(true)

    // Queue an add while the import is in flight.
    mocks.AddCustomDictionaryWord.mockResolvedValue(['apple', 'banana'])
    customDictionary.add('banana')
    expect(mocks.AddCustomDictionaryWord).not.toHaveBeenCalled()

    // The in-flight import rejects (network/disk failure).
    importDef.reject(new Error('network timeout'))
    await importP

    // busy is released and the queued add still runs + succeeds despite the
    // in-flight rejection — the rejection must not strand the pending action.
    await vi.waitFor(() => {
      expect(mocks.AddCustomDictionaryWord).toHaveBeenCalledWith('banana')
      expect(customDictionary.words).toEqual(['apple', 'banana'])
    })
    expect(customDictionary.busy).toBe(false)
  })

  it('releases busy with no error when a queued export dialog is cancelled', async () => {
    // Non-empty dict so the drained export passes the empty-check and reaches
    // the save dialog (where the cancel / empty-path early-return lives).
    mocks.GetCustomDictionary.mockResolvedValue(['word'])

    const importDef = deferred<{ added: number; skipped: number }>()
    mocks.PickCustomDictionaryImportFile.mockResolvedValue('/p')
    mocks.ImportCustomDictionary.mockReturnValue(importDef.promise)

    const importP = customDictionary.importFile()
    expect(customDictionary.busy).toBe(true)

    // Queued export whose save dialog is cancelled (empty path).
    mocks.PickCustomDictionaryExportPath.mockResolvedValue('')
    customDictionary.exportFile()

    importDef.resolve({ added: 0, skipped: 0 })
    await importP

    await vi.waitFor(() => {
      expect(mocks.PickCustomDictionaryExportPath).toHaveBeenCalled()
      expect(customDictionary.busy).toBe(false)
    })
    expect(mocks.ExportCustomDictionary).not.toHaveBeenCalled()
    expect(customDictionary.error).toBeNull()
  })

  it('captures the resolved word (not the raw arg) when add() is queued mid-flight', async () => {
    // Drive busy with a held no-arg add; its body clears newWord on resolve.
    const firstDef = deferred<string[]>()
    mocks.AddCustomDictionaryWord.mockReturnValue(firstDef.promise)

    customDictionary.newWord = 'first'
    const firstP = customDictionary.add() // no-arg, in flight
    expect(customDictionary.busy).toBe(true)

    // The queued call is no-arg. Before CHANGE A the closure captured the raw
    // `word` (undefined) and re-read newWord at drain time, so a field change
    // after queueing would target the wrong word (or no-op on a cleared field).
    customDictionary.newWord = 'hello'
    customDictionary.add() // no-arg, queued while busy
    expect(mocks.AddCustomDictionaryWord).toHaveBeenCalledTimes(1) // only first

    // Simulate the user retyping the field before the queue drains. Before the
    // fix the queued add would target 'world' here.
    customDictionary.newWord = 'world'

    // Re-mock so the drained add resolves with the captured word's result.
    mocks.AddCustomDictionaryWord.mockResolvedValue(['hello'])
    firstDef.resolve(['first'])
    await firstP

    await vi.waitFor(() => {
      // 'hello' was captured at queue time; 'world' (current newWord) is not used.
      expect(mocks.AddCustomDictionaryWord).toHaveBeenCalledWith('hello')
      expect(customDictionary.words).toEqual(['hello'])
    })
    expect(mocks.AddCustomDictionaryWord).not.toHaveBeenCalledWith('world')
    // newWord is cleared by the in-flight no-arg add's body on resolve.
    expect(customDictionary.newWord).toBe('')
    expect(customDictionary.busy).toBe(false)
  })

  it('skips load() while a mutation is in flight so the result is not clobbered', async () => {
    // If load() ran while busy, it would overwrite `words` with this list.
    mocks.GetCustomDictionary.mockResolvedValue(['STALE'])

    const addDef = deferred<string[]>()
    mocks.AddCustomDictionaryWord.mockReturnValue(addDef.promise)

    const addP = customDictionary.add('foo')
    expect(customDictionary.busy).toBe(true)

    // Concurrent load while the add is in flight: must no-op (CHANGE B). It
    // returns before touching loading/words, so the in-flight result wins.
    await customDictionary.load()

    expect(mocks.GetCustomDictionary).not.toHaveBeenCalled()
    expect(customDictionary.loading).toBe(false)

    addDef.resolve(['foo'])
    await addP

    expect(customDictionary.words).toEqual(['foo'])
    expect(mocks.GetCustomDictionary).not.toHaveBeenCalled()
  })
})
