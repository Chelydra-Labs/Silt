// Reactive controller for the typed-notes surfaces (inline strip + bottom
// panel). Owns the GetPageType/GetPageProperties round-trip, the SetPageType
// keep-and-flag result, the type-picker listing (ListTypes), the panel-open
// flag, and the `types:changed` event subscription that re-fetches the active
// page when the type set changes externally.
//
// Built via the proven createX(deps) factory idiom (mirrors
// useGlobalHotkeyDispatch): every read of the active locator is a closure the
// host provides, so the controller has no direct reference to App's $state.
// App drives reactivity by calling refresh() from a $effect on the locator;
// the controller owns only the one-time `types:changed` subscription.
import { Events } from '@wailsio/runtime'
import { EventName } from '../generated/enums'
import {
  GetPageProperties,
  GetPageType,
  ListTypes
} from '../../bindings/silt/app.js'
import { coerceIPCError } from '../lib/ipcError'
import type {
  ListTypesResult,
  PageLocator,
  PagePropertyValue,
  PageTypeInfo,
  TypeDef
} from './types'

const EMPTY_INFO: PageTypeInfo = {
  typeId: '',
  type: { id: '', name: '', properties: [] },
  isSet: false,
  rawType: ''
}

export interface PageTypeControllerDeps {
  getLocator: () => PageLocator
}

export interface PageTypeController {
  readonly info: PageTypeInfo
  readonly values: PagePropertyValue[]
  readonly mismatched: string[]
  readonly error: string
  readonly loading: boolean
  readonly types: TypeDef[]
  readonly typesLoading: boolean
  readonly panelOpen: boolean
  readonly heroValue: string
  refresh: () => Promise<void>
  open: () => void
  close: () => void
  toggle: () => void
  /** Keep-and-flag warnings from a type switch (surfaces them on the fields). */
  setMismatched: (names: string[]) => void
  setError: (message: string) => void
  /** Subscribe to `types:changed`; returns a disposer for onMount cleanup. */
  attach: () => () => void
}

export function createPageTypeController(
  deps: PageTypeControllerDeps
): PageTypeController {
  let info = $state<PageTypeInfo>(EMPTY_INFO)
  let values = $state<PagePropertyValue[]>([])
  let mismatched = $state<string[]>([])
  let error = $state('')
  let loading = $state(false)
  let types = $state<TypeDef[]>([])
  let typesLoading = $state(false)
  let panelOpen = $state(false)

  // Track the locator that the in-flight refresh targeted so a stale response
  // (the user navigated mid-fetch) is discarded rather than painted over a new
  // page's data.
  let refreshToken = 0

  async function refresh(): Promise<void> {
    const { notebook, page } = deps.getLocator()
    if (!notebook || !page) {
      info = EMPTY_INFO
      values = []
      mismatched = []
      return
    }
    const token = ++refreshToken
    loading = true
    try {
      const [typeInfo, props] = await Promise.all([
        GetPageType(notebook, deps.getLocator().section, page),
        GetPageProperties(notebook, deps.getLocator().section, page)
      ])
      if (token !== refreshToken) return
      info = (typeInfo as PageTypeInfo) ?? EMPTY_INFO
      values = (props as PagePropertyValue[]) ?? []
      // A fresh fetch clears stale keep-and-flag warnings — they describe the
      // last type switch, not the current state.
      mismatched = []
    } catch (e) {
      if (token !== refreshToken) return
      error = coerceIPCError(e).message
    } finally {
      if (token === refreshToken) loading = false
    }
  }

  async function loadTypes(): Promise<void> {
    typesLoading = true
    try {
      const res = (await ListTypes()) as ListTypesResult | null
      types = res?.types ?? []
    } catch (e) {
      // A failed listing is non-fatal — the menu just shows what it has. The
      // banner is reserved for edit failures the user can act on.
      console.error('types: ListTypes failed:', e)
    } finally {
      typesLoading = false
    }
  }

  function open(): void {
    panelOpen = true
  }

  function close(): void {
    panelOpen = false
  }

  function toggle(): void {
    panelOpen = !panelOpen
  }

  function setMismatched(names: string[]): void {
    mismatched = names
  }

  function setError(message: string): void {
    error = message
  }

  function attach(): () => void {
    void loadTypes()
    // Debounce so a burst of type-file changes coalesces into one reload +
    // refresh (mirrors the templates store's `templates:changed` handler).
    let timer: ReturnType<typeof setTimeout> | null = null
    const off = Events.On(EventName.EventTypesChanged, () => {
      if (timer !== null) clearTimeout(timer)
      timer = setTimeout(() => {
        timer = null
        void loadTypes()
        void refresh()
      }, 100)
    })
    return () => {
      if (timer !== null) {
        clearTimeout(timer)
        timer = null
      }
      off()
    }
  }

  return {
    get info() {
      return info
    },
    get values() {
      return values
    },
    get mismatched() {
      return mismatched
    },
    get error() {
      return error
    },
    get loading() {
      return loading
    },
    get types() {
      return types
    },
    get typesLoading() {
      return typesLoading
    },
    get panelOpen() {
      return panelOpen
    },
    get heroValue() {
      const heroName = info.type?.heroField
      if (!heroName) return ''
      const hit = values.find((v) => v.name === heroName)
      if (!hit || !hit.isSet) return ''
      return formatHero(hit.value)
    },
    refresh,
    open,
    close,
    toggle,
    setMismatched,
    setError,
    attach
  }
}

/** Render the hero field's value as a single compact string for the strip. */
function formatHero(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (Array.isArray(value)) return value.filter(Boolean).join(', ')
  if (typeof value === 'boolean') return value ? 'Yes' : 'No'
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'bigint') {
    return String(value)
  }
  // Non-primitive hero values aren't expected; never emit "[object Object]".
  return ''
}
