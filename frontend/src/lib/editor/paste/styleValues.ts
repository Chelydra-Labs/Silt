// Shared policy for CSS values that carry no portable formatting.
//
// Two consumers must agree on this or they drift:
//   - the TextColor/Highlight mark parse rules in schema.ts, and
//   - the paste normalizer, which filters the same declarations before the
//     DOM parser ever sees them.
//
// CSS-wide and system color keywords carry no portable color. Storing them on
// a mark would render the inherited color (`inherit`, `currentColor`), hide
// the text (`transparent`), or persist a Word system color (`auto`,
// `windowtext`), so styled page wrappers pasted as text must not turn into
// color marks.
export const INERT_COLOR_VALUES = new Set([
  'inherit',
  'initial',
  'unset',
  'revert',
  'revert-layer',
  'currentcolor',
  'transparent',
  'auto',
  'windowtext'
])

export function colorFromStyle(
  value: string | null | undefined
): string | null {
  const color = value?.trim()
  if (!color) return null
  return INERT_COLOR_VALUES.has(color.toLowerCase()) ? null : color
}
