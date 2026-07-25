// Unit tests for the optimistic-commit primitive shared by TaskEditDrawer's
// metadata editors. No Svelte rendering — the factory's $state is exercised
// directly (same pattern as state.test.ts over state.svelte.ts). No IPC, so
// the Wails bindings are not mocked.
import { describe, expect, it, vi } from 'vitest'
import { optimisticField } from './optimisticField.svelte'

describe('optimisticField', () => {
  it('optimistically sets value, writes, and notifies onChanged on success', async () => {
    const write = vi.fn().mockResolvedValue(undefined)
    const onChanged = vi.fn()
    const f = optimisticField({ initial: 'a', write, onChanged })

    expect(f.value).toBe('a')
    expect(f.pending).toBe(false)

    const ok = await f.commit('b')

    expect(ok).toBe(true)
    expect(f.value).toBe('b')
    expect(f.pending).toBe(false)
    expect(write).toHaveBeenCalledTimes(1)
    expect(write).toHaveBeenCalledWith('b')
    expect(onChanged).toHaveBeenCalledTimes(1)
  })

  it('clears the error banner at commit start (onError with empty string)', async () => {
    const onError = vi.fn()
    const f = optimisticField({
      initial: 'a',
      write: vi.fn().mockResolvedValue(undefined),
      onError
    })
    await f.commit('b')
    // The only onError call on a successful commit is the start-of-commit
    // clear — the component relies on this to drop a stale prior banner.
    expect(onError).toHaveBeenCalledTimes(1)
    expect(onError).toHaveBeenNthCalledWith(1, '')
  })

  it('reverts value and reports the friendly message on write failure', async () => {
    const write = vi.fn().mockRejectedValue(new Error('disk locked'))
    const onError = vi.fn()
    const f = optimisticField({ initial: 'a', write, onError })

    const ok = await f.commit('b')

    expect(ok).toBe(false)
    expect(f.value).toBe('a')
    expect(f.pending).toBe(false)
    // Last onError call carries the friendly message (friendlyCaughtError
    // passes the raw message through when there is no focus-lock code).
    expect(onError).toHaveBeenLastCalledWith('disk locked')
  })

  it('rejects a concurrent commit while one is in flight', async () => {
    let resolveWrite!: () => void
    const write = vi.fn(() => new Promise<void>((r) => (resolveWrite = r)))
    const f = optimisticField({ initial: 'a', write })

    const first = f.commit('b')
    expect(f.pending).toBe(true)

    // A second commit while the first is still pending is dropped.
    const ok = await f.commit('c')
    expect(ok).toBe(false)
    expect(write).toHaveBeenCalledTimes(1)
    expect(write).toHaveBeenNthCalledWith(1, 'b')
    // Optimistic value from the first commit holds.
    expect(f.value).toBe('b')

    resolveWrite()
    await first
    expect(f.pending).toBe(false)
  })

  it('reset() overwrites value without invoking write', () => {
    const write = vi.fn().mockResolvedValue(undefined)
    const f = optimisticField({ initial: 'a', write })
    f.reset('z')
    expect(f.value).toBe('z')
    expect(write).not.toHaveBeenCalled()
  })

  it('exposes a settable value accessor for two-way binding', () => {
    const f = optimisticField({ initial: 'a', write: vi.fn() })
    f.value = 'm'
    expect(f.value).toBe('m')
  })
})
