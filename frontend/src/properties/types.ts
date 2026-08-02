// Frontend shape of the typed-notes IPC surface. Plain interfaces mirroring the
// Go structs in app_types_props.go and backend/types/schema.go — decoupled from
// the generated bindings models so chrome code does not reach across the plugin
// boundary and so the components depend only on the data they consume (the same
// convention BacklinksSidebarPanel uses for its IPC result types).

export const PROPERTY_TYPES = [
  'text',
  'number',
  'date',
  'datetime',
  'checkbox',
  'select',
  'multiselect',
  'page',
  'pages'
] as const

export type PropertyType = (typeof PROPERTY_TYPES)[number]

/** One property declaration on a type. Carries min/max the value envelope omits. */
export interface PropertyDef {
  name: string
  label?: string
  type: PropertyType
  required?: boolean
  options?: string[]
  default?: unknown
  min?: number | null
  max?: number | null
  target?: string
  cardinality?: string
  description?: string
}

export interface TypeDef {
  id: string
  name: string
  description?: string
  icon?: string
  /** The property whose value the inline strip surfaces as the page's headline. */
  heroField?: string
  properties?: PropertyDef[]
}

/**
 * GetPageType result. `isSet` is false for an untyped page AND for a page whose
 * `type:` ref does not resolve to a known schema; in the latter case `rawType`
 * is non-empty so the strip can render a subdued chip without crashing.
 */
export interface PageTypeInfo {
  typeId: string
  type: TypeDef
  isSet: boolean
  rawType: string
}

/** One property's schema plus its current value, in declaration order. */
export interface PagePropertyValue {
  name: string
  label: string
  type: PropertyType
  value: unknown
  isSet: boolean
  required: boolean
  options?: string[]
}

export interface ListTypesResult {
  types: TypeDef[]
  errors?: string[]
  warnings?: string[]
}

/** Active page locator — the triple every typed-notes IPC call needs. */
export interface PageLocator {
  notebook: string
  section: string
  page: string
}
