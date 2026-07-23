/**
 * Coerce unknown IPC / attr values to a string without relying on Object's
 * default "[object Object]" stringification (satisfies no-base-to-string).
 */
export function asString(value: unknown, fallback = ''): string {
  if (value == null) return fallback
  if (typeof value === 'string') return value
  if (
    typeof value === 'number' ||
    typeof value === 'boolean' ||
    typeof value === 'bigint'
  ) {
    return String(value)
  }
  if (typeof value === 'symbol') return value.description ?? fallback
  if (value instanceof Error) return value.message || value.name || fallback
  return fallback
}
