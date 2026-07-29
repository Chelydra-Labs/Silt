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
})
