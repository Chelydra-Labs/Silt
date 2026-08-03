import { describe, expect, it, vi } from 'vitest'
import { optimisticField } from './optimisticField.svelte'

// Controllable deferred — lets each test resolve/reject a specific write call
// in its own time so the queue-replay paths are observable.
function makeDeferred<T = void>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('optimisticField', () => {
  it('commits a single edit and clears the pending flag on success', async () => {
    const write = vi
      .fn<(v: string) => Promise<unknown>>()
      .mockResolvedValue(undefined)
    const field = optimisticField({ initial: 'start', write })

    const ok = await field.commit('A')
    expect(ok).toBe(true)
    expect(write).toHaveBeenCalledWith('A')
    expect(field.value).toBe('A')
    expect(field.pending).toBe(false)
  })

  it('reverts to the last-known-persisted value when a write fails (no onResync)', async () => {
    const write = vi
      .fn<(v: string) => Promise<unknown>>()
      .mockRejectedValueOnce(new Error('boom'))
    const onError = vi.fn()
    const field = optimisticField({ initial: 'start', write, onError })

    const ok = await field.commit('A')
    expect(ok).toBe(false)
    expect(field.value).toBe('start')
    expect(onError).toHaveBeenCalledWith('boom')
  })

  it('on write failure with onResync, re-fetches instead of blindly reverting to prev', async () => {
    // A timeout/connection error can fire AFTER the backend persisted. Blind
    // revert would diverge from the file; onResync (wired to page refresh)
    // pulls truth so field.reset() reseeds from disk.
    const write = vi
      .fn<(v: string) => Promise<unknown>>()
      .mockRejectedValueOnce(new Error('timeout'))
    const onError = vi.fn()
    const onResync = vi.fn()
    const field = optimisticField({
      initial: 'start',
      write,
      onError,
      onResync
    })

    const ok = await field.commit('A')
    expect(ok).toBe(false)
    expect(onError).toHaveBeenCalledWith('timeout')
    expect(onResync).toHaveBeenCalledOnce()
    // Optimistic value kept until the resync path reseeds via reset() —
    // not snapped back to prev (which may be stale if the write landed).
    expect(field.value).toBe('A')

    // Simulate the controller refresh landing the server truth (write landed).
    field.reset('A')
    expect(field.value).toBe('A')
  })

  it('on write failure with onResync, reset from server can correct a write that never landed', async () => {
    const write = vi
      .fn<(v: string) => Promise<unknown>>()
      .mockRejectedValueOnce(new Error('boom'))
    const onResync = vi.fn()
    const field = optimisticField({
      initial: 'start',
      write,
      onResync
    })

    await field.commit('A')
    expect(field.value).toBe('A')
    expect(onResync).toHaveBeenCalledOnce()
    // Server says write never landed — resync reseeds to start.
    field.reset('start')
    expect(field.value).toBe('start')
  })

  it('queues a fast second edit while a write is in flight and replays it once the write settles', async () => {
    const a = makeDeferred()
    const b = makeDeferred()
    const write = vi
      .fn<(v: string) => Promise<unknown>>()
      .mockReturnValueOnce(a.promise)
      .mockReturnValueOnce(b.promise)

    const field = optimisticField({ initial: 'start', write })

    const pA = field.commit('A')
    expect(field.pending).toBe(true)
    expect(field.value).toBe('A')

    // B arrives before A's write settles — must be queued, not dropped.
    expect(await field.commit('B')).toBe(false)
    expect(field.value).toBe('B') // optimistic UI reflects the latest keystroke
    expect(write).toHaveBeenCalledTimes(1) // B not written yet

    a.resolve()
    await pA

    // The replay fired commit(B) as soon as A settled.
    await vi.waitFor(() => expect(write).toHaveBeenCalledWith('B'))
    expect(write).toHaveBeenCalledTimes(2)
    expect(field.value).toBe('B')
    expect(field.pending).toBe(true)

    b.resolve()
    await vi.waitFor(() => expect(field.pending).toBe(false))
    expect(field.value).toBe('B')
  })

  it('commits a queued edit against the reverted value when the in-flight write fails', async () => {
    const a = makeDeferred()
    const b = makeDeferred()
    const write = vi
      .fn<(v: string) => Promise<unknown>>()
      .mockReturnValueOnce(
        a.promise.then(() => {
          throw new Error('boom')
        })
      )
      .mockReturnValueOnce(b.promise)
    const onError = vi.fn()

    const field = optimisticField({ initial: 'start', write, onError })

    const pA = field.commit('A')
    // Queue B while A is still in flight.
    field.commit('B')

    a.resolve()
    await pA

    // A failed → value reverted to 'start', then the queued B replayed
    // against that reverted state. The user's latest intent wins.
    await vi.waitFor(() => expect(write).toHaveBeenCalledWith('B'))
    expect(field.value).toBe('B')
    expect(field.pending).toBe(true)
    expect(onError).toHaveBeenCalledWith('boom') // A's failure surfaced
    expect(onError).toHaveBeenCalledWith('') // B's replay clears the banner

    b.resolve()
    await vi.waitFor(() => expect(field.pending).toBe(false))
    expect(field.value).toBe('B')
  })

  it('reverts to the post-A-revert value when the queued replay also fails (double failure)', async () => {
    const a = makeDeferred()
    const b = makeDeferred()
    const write = vi
      .fn<(v: string) => Promise<unknown>>()
      .mockReturnValueOnce(
        a.promise.then(() => {
          throw new Error('boom-A')
        })
      )
      .mockReturnValueOnce(
        b.promise.then(() => {
          throw new Error('boom-B')
        })
      )
    const onError = vi.fn()

    const field = optimisticField({ initial: 'start', write, onError })

    const pA = field.commit('A')
    field.commit('B') // queued while A is in flight

    a.resolve()
    await pA

    // A failed → value reverted to 'start' (prev still 'start'), then the
    // queued B replayed.
    await vi.waitFor(() => expect(write).toHaveBeenCalledWith('B'))
    expect(field.value).toBe('B')
    expect(field.pending).toBe(true)
    expect(onError).toHaveBeenCalledWith('boom-A')

    // B's replay fails too → reverts to prev, which is still the post-A-revert
    // 'start' (not 'B' — failed writes never advance the snapshot).
    b.resolve()
    await vi.waitFor(() => expect(field.pending).toBe(false))
    expect(field.value).toBe('start')
    expect(onError).toHaveBeenCalledWith('boom-B')
  })

  it('reset() during a pending write preserves the queued edit (mid-write refresh does not drop it)', async () => {
    const a = makeDeferred()
    const b = makeDeferred()
    const write = vi
      .fn<(v: string) => Promise<unknown>>()
      .mockReturnValueOnce(a.promise)
      .mockReturnValueOnce(b.promise)

    const field = optimisticField({ initial: 'start', write })
    field.commit('A') // pending = true, write A in flight
    field.commit('B') // queued while A is in flight

    // A types:changed / projection-error refresh races the in-flight write.
    // The reset must be skipped so the user's latest keystroke survives.
    field.reset('stale-from-refresh')
    expect(field.value).toBe('B') // optimistic UI intact, reset was a no-op

    a.resolve()
    // The replay fires commit(B) once A settles — the edit was not dropped.
    await vi.waitFor(() => expect(write).toHaveBeenCalledWith('B'))
    expect(write).toHaveBeenCalledTimes(2)

    b.resolve()
    await vi.waitFor(() => expect(field.pending).toBe(false))
    expect(field.value).toBe('B')
  })

  it('reset() when no write is pending re-seeds and cancels a queued replay', async () => {
    const a = makeDeferred()
    const write = vi
      .fn<(v: string) => Promise<unknown>>()
      .mockReturnValueOnce(a.promise)

    const field = optimisticField({ initial: 'start', write })
    const pA = field.commit('A')
    a.resolve()
    await pA // pending is now false

    // No write in flight: reset re-seeds normally.
    field.reset('external')
    expect(field.value).toBe('external')
    expect(field.pending).toBe(false)

    // Give any would-be replay a chance to (not) fire.
    await new Promise((r) => setTimeout(r, 0))
    expect(write).toHaveBeenCalledTimes(1)
  })

  it('markPersisted seeds the revert target so a failed write after a clear reverts to the cleared value', async () => {
    const write = vi
      .fn<(v: string) => Promise<unknown>>()
      .mockResolvedValueOnce(undefined) // commit('A') succeeds
      .mockRejectedValueOnce(new Error('boom')) // commit('B') fails

    const field = optimisticField({ initial: 'start', write })

    await field.commit('A') // persists 'A'; prev advances to 'A'

    // Simulate the clear-field path: optimistically clear, then mark the
    // cleared state as persisted (advances the revert snapshot).
    field.value = ''
    field.markPersisted('')

    // A subsequent commit fails → reverts to the cleared state, NOT the
    // pre-clear value 'A' (the resurrected-value bug).
    const ok = await field.commit('B')
    expect(ok).toBe(false)
    expect(field.value).toBe('')
  })

  it('without markPersisted, a failed write after a clear resurrects the pre-clear value (the bug)', async () => {
    const write = vi
      .fn<(v: string) => Promise<unknown>>()
      .mockResolvedValueOnce(undefined) // commit('A') succeeds
      .mockRejectedValueOnce(new Error('boom')) // commit('B') fails

    const field = optimisticField({ initial: 'start', write })

    await field.commit('A') // prev = 'A'

    // Clear WITHOUT advancing the snapshot — prev is still 'A'.
    field.value = ''

    const ok = await field.commit('B')
    expect(ok).toBe(false)
    // The pre-clear value is resurrected (this is the bug markPersisted fixes).
    expect(field.value).toBe('A')
  })
})
