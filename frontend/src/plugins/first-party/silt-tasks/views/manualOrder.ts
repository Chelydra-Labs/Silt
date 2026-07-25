// Pure manual-order token math shared by the cross-column drop paths.
// Source columns are gap-tolerant (orders need not be contiguous), so the
// next token is one past the destination's current maximum.
export function nextManualOrder(items: { manual_order?: number }[]): number {
  return items.length > 0
    ? Math.max(...items.map((i) => i.manual_order ?? 0)) + 1
    : 1
}
