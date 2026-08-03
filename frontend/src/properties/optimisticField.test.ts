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

  it('reverts to the last-known-persisted value when a write fails', async () => {
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

  it('reset() cancels a queued replay (page switch drops the pending edit)', async () => {
    const a = makeDeferred()
    const write = vi
      .fn<(v: string) => Promise<unknown>>()
      .mockReturnValueOnce(a.promise)

    const field = optimisticField({ initial: 'start', write })
    field.commit('A')
    field.commit('B') // queued

    field.reset('external')

    a.resolve()
    // Give the would-be replay a chance to (not) fire.
    await new Promise((r) => setTimeout(r, 0))
    expect(write).toHaveBeenCalledTimes(1) // B was never written
    expect(field.value).toBe('external')
  })
})
