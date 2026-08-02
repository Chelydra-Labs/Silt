// Classification of how an old property value fares against a new type's schema
// during a "Turn into" conversion. The backend's SetPageType keeps every value
// as-is and re-validates against the new schema (keep-and-flag), so this is an
// advisory preview of what the user should expect — not a client-side coercion.
import type { PropertyType } from './types'

export type Compatibility = 'auto' | 'coerced' | 'flagged'

/**
 * Decide whether a value of `oldType` will cleanly fit a property declared as
 * `newType`. Same type is auto (carries over and validates). A safe widening
 * (most things stringify, a single value wraps into a list) is coerced. Pairs
 * that the new schema will almost certainly reject (text→number, cardinality
 * mismatches, anything into checkbox) are flagged.
 */
export function classifyPair(
  oldType: PropertyType,
  newType: PropertyType
): Compatibility {
  if (oldType === newType) return 'auto'
  // Anything into text accepts the stringified value.
  if (newType === 'text') return 'coerced'
  // Date ↔ datetime keep the day component.
  if (
    (oldType === 'date' || oldType === 'datetime') &&
    (newType === 'date' || newType === 'datetime')
  ) {
    return 'coerced'
  }
  // Single ↔ list within relations: page→pages wraps cleanly; pages→page keeps
  // only the first and is likely surprising, so flag it.
  if (oldType === 'page' && newType === 'pages') return 'coerced'
  if (oldType === 'select' && newType === 'multiselect') return 'coerced'
  // Everything else (text→number, text→date, *→checkbox, multiselect→select,
  // pages→page, etc.) won't validate against the new schema.
  return 'flagged'
}
