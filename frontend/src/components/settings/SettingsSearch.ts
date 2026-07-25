// Shared "ring" highlight used by settings tabs when the search panel jumps
// to a control. Centralized so General / AI / etc. render the same ring.
export function ringClass(id: string, anchor: string | null): string {
  return anchor === id
    ? 'ring-2 ring-accent-primary-start/50 ring-offset-2 ring-offset-surface-app'
    : ''
}
