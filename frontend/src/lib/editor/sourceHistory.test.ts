import { describe, expect, it, vi } from 'vitest'
import {
  createSourceHistory,
  SOURCE_HISTORY_MAX_DEFAULT
} from './sourceHistory'

function entry(
  value: string,
  start = 0,
  end = 0,
  direction: 'forward' | 'backward' | 'none' = 'forward'
) {
  return { value, selection: { start, end, direction } }
}

describe('createSourceHistory', () => {
  it('starts empty with no current and no undo/redo available', () => {
    const h = createSourceHistory()
    expect(h.current()).toBeNull()
    expect(h.canUndo()).toBe(false)
    expect(h.canRedo()).toBe(false)
    expect(h.size()).toBe(0)
  })

  it('reset seeds a single entry that cannot be undone past', () => {
    const h = createSourceHistory()
    h.reset(entry('seed'))
    expect(h.current()?.value).toBe('seed')
    expect(h.canUndo()).toBe(false)
    expect(h.canRedo()).toBe(false)
    expect(h.size()).toBe(1)
  })

  it('push records edits and undo walks them back in order', () => {
    const h = createSourceHistory({ now: () => 0 })
    h.reset(entry('a'))
    // Force each push to a new entry by advancing the clock past the window.
    const clock = { t: 0 }
    const now = () => (clock.t += 1000)
    const hh = createSourceHistory({ now })
    hh.reset(entry('a'))
    hh.push(entry('ab', 2, 2))
    hh.push(entry('abc', 3, 3))
    expect(hh.current()?.value).toBe('abc')
    expect(hh.canUndo()).toBe(true)

    expect(hh.undo()?.value).toBe('ab')
    expect(hh.undo()?.value).toBe('a')
    expect(hh.canUndo()).toBe(false)
    expect(hh.undo()).toBeNull()
    void h
  })

  it('redo replays the branch that undo left behind', () => {
    const clock = { t: 0 }
    const h = createSourceHistory({ now: () => (clock.t += 1000) })
    h.reset(entry('a'))
    h.push(entry('ab', 2, 2))
    h.push(entry('abc', 3, 3))
    h.undo()
    h.undo()
    expect(h.current()?.value).toBe('a')
    expect(h.canRedo()).toBe(true)

    expect(h.redo()?.value).toBe('ab')
    expect(h.redo()?.value).toBe('abc')
    expect(h.canRedo()).toBe(false)
    expect(h.redo()).toBeNull()
  })

  it('pushing after an undo clears the redo branch (redo invalidation)', () => {
    const clock = { t: 0 }
    const h = createSourceHistory({ now: () => (clock.t += 1000) })
    h.reset(entry('a'))
    h.push(entry('ab', 2, 2))
    h.push(entry('abc', 3, 3))
    h.undo() // back to 'ab'
    expect(h.size()).toBe(3)

    h.push(entry('abX', 3, 3))
    // 'abc' is gone — the new branch replaced the redo tail.
    expect(h.size()).toBe(3)
    expect(h.current()?.value).toBe('abX')
    expect(h.canRedo()).toBe(false)
    h.redo()
    expect(h.current()?.value).toBe('abX')
  })

  it('coalesce replaces the top instead of pushing a new entry', () => {
    const h = createSourceHistory({ coalesceMs: 100, now: () => 5 })
    h.reset(entry('a'))
    h.push(entry('ab', 2, 2), { coalesce: true })
    // Within window — should have merged with the seed... but the seed
    // resets lastPushAt to -Infinity, so the first real push never
    // coalesces. After that first push, the next one can.
    expect(h.size()).toBe(2)
    expect(h.current()?.value).toBe('ab')

    h.push(entry('abc', 3, 3), { coalesce: true })
    expect(h.size()).toBe(2) // merged, not added
    expect(h.current()?.value).toBe('abc')

    h.push(entry('abcd', 4, 4), { coalesce: true })
    expect(h.size()).toBe(2)
    expect(h.current()?.value).toBe('abcd')
  })

  it('coalesce is refused once the time window has elapsed', () => {
    const clock = { t: 0 }
    const h = createSourceHistory({
      coalesceMs: 50,
      now: () => clock.t
    })
    h.reset(entry('a'))
    clock.t = 100
    h.push(entry('ab', 2, 2), { coalesce: true })
    clock.t = 130
    h.push(entry('abc', 3, 3), { coalesce: true }) // within 50ms of 100
    expect(h.size()).toBe(2) // merged
    clock.t = 500
    h.push(entry('abcd', 4, 4), { coalesce: true }) // outside window
    expect(h.size()).toBe(3) // new entry
  })

  it('coalesce is refused while a redo tail exists', () => {
    const clock = { t: 0 }
    const h = createSourceHistory({
      coalesceMs: 1000,
      now: () => (clock.t += 1)
    })
    h.reset(entry('a'))
    h.push(entry('ab', 2, 2))
    h.push(entry('abc', 3, 3))
    h.undo() // pointer at 'ab', redo tail ['abc']

    // Even with coalesce=true and the clock within window, the redo tail
    // must be cleared — otherwise the user's new typing would resurrect a
    // branch they intentionally abandoned.
    h.push(entry('abX', 3, 3), { coalesce: true })
    expect(h.size()).toBe(3)
    expect(h.current()?.value).toBe('abX')
    expect(h.canRedo()).toBe(false)
  })

  it('caps history at max by dropping the oldest entry (LRU)', () => {
    const clock = { t: 0 }
    const h = createSourceHistory({
      max: 3,
      now: () => (clock.t += 1000)
    })
    h.reset(entry('a'))
    h.push(entry('ab', 2, 2))
    h.push(entry('abc', 3, 3))
    h.push(entry('abcd', 4, 4)) // should evict 'a'
    expect(h.size()).toBe(3)
    // Undoing all the way stops at 'ab' — 'a' is gone.
    h.undo()
    h.undo()
    expect(h.current()?.value).toBe('ab')
    expect(h.canUndo()).toBe(false)
  })

  it('default max matches the documented bounded buffer size', () => {
    expect(SOURCE_HISTORY_MAX_DEFAULT).toBe(100)
  })

  it('reset clears the redo branch and re-seeds', () => {
    const clock = { t: 0 }
    const h = createSourceHistory({ now: () => (clock.t += 1000) })
    h.reset(entry('a'))
    h.push(entry('ab', 2, 2))
    h.push(entry('abc', 3, 3))
    h.undo()

    h.reset(entry('fresh'))
    expect(h.size()).toBe(1)
    expect(h.current()?.value).toBe('fresh')
    expect(h.canUndo()).toBe(false)
    expect(h.canRedo()).toBe(false)
  })

  it('does not coalesce across a reset boundary', () => {
    const clock = { t: 0 }
    const h = createSourceHistory({
      coalesceMs: 10000,
      now: () => (clock.t += 1)
    })
    h.reset(entry('a'))
    h.push(entry('ab', 2, 2), { coalesce: true })
    // Resetting stamps lastPushAt to -Infinity, so even with coalesce=true
    // the first push after the boundary is its own entry.
    h.reset(entry('fresh'))
    h.push(entry('freshX', 6, 6), { coalesce: true })
    expect(h.size()).toBe(2)
  })

  it('entries preserve selection so undo can restore the caret', () => {
    const h = createSourceHistory({ now: () => 0 })
    h.reset(entry('hello', 5, 5, 'forward'))
    expect(h.current()?.selection).toEqual({
      start: 5,
      end: 5,
      direction: 'forward'
    })
  })

  it('pushing the same value still records an entry (no-op is the caller responsibility)', () => {
    const h = createSourceHistory({ now: () => 0 })
    h.reset(entry('same'))
    h.push(entry('same'))
    expect(h.size()).toBe(2)
    expect(h.canUndo()).toBe(true)
  })
})

describe('createSourceHistory vi mocked clock', () => {
  it('works with vi.useFakeTimers and Date.now', () => {
    vi.useFakeTimers()
    try {
      const h = createSourceHistory({ coalesceMs: 100 })
      h.reset(entry('a'))
      vi.setSystemTime(1000)
      h.push(entry('ab', 2, 2), { coalesce: true })
      vi.setSystemTime(1050)
      h.push(entry('abc', 3, 3), { coalesce: true })
      expect(h.size()).toBe(2) // both within 100ms of prior push
      vi.setSystemTime(5000)
      h.push(entry('abcd', 4, 4), { coalesce: true })
      expect(h.size()).toBe(3) // outside window
    } finally {
      vi.useRealTimers()
    }
  })
})
