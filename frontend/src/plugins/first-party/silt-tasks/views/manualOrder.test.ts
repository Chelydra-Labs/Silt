// Unit tests for nextManualOrder, the shared cross-column order-token calc.
// Pins the gap-tolerant max+1 contract (and the undefined → 0 coercion) that
// every manual-sort drop path in BoardView relies on.
import { describe, it, expect } from 'vitest'
import { nextManualOrder } from './manualOrder'

describe('nextManualOrder', () => {
  it('returns 1 for an empty destination', () => {
    expect(nextManualOrder([])).toBe(1)
  })

  it('returns max+1 for a single item', () => {
    expect(nextManualOrder([{ manual_order: 1 }])).toBe(2)
  })

  it('returns max+1 for non-contiguous (gap-tolerant) orders', () => {
    expect(nextManualOrder([{ manual_order: 1 }, { manual_order: 5 }])).toBe(6)
  })

  it('treats an undefined manual_order as 0', () => {
    expect(nextManualOrder([{ manual_order: undefined }])).toBe(1)
  })

  it('handles a mixed set including 0 and undefined', () => {
    expect(
      nextManualOrder([
        { manual_order: 0 },
        { manual_order: undefined },
        { manual_order: 3 }
      ])
    ).toBe(4)
  })
})
