// Reactive controller for the typed-notes surfaces (inline strip + bottom
// panel). Owns the GetPageType/GetPageProperties round-trip, the SetPageType
// keep-and-flag result, the type-picker listing (ListTypes), the panel-open
// flag, the `types:changed` subscription that re-fetches the active page when
// the type set changes externally, and the `types:projection-error`
// subscription that refreshes + warns when a post-write re-parse fails.
//
// Built via the proven createX(deps) factory idiom (mirrors
// useGlobalHotkeyDispatch): every read of the active locator is a closure the
// host provides, so the controller has no direct reference to App's $state.
// App drives reactivity by calling refresh() from a $effect on the locator;
// the controller owns only the one-time event subscriptions.
import { Events } from '@wailsio/runtime'
import { EventName } from '../generated/enums'
import {
  GetPageProperties,
  GetPageType,
  ListTypes
} from '../../bindings/silt/app.js'
import { coerceIPCError } from '../lib/ipcError'
import { pushNotification } from '../notifications/store.svelte'
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
  /** Monotonic request counter the panel watches (slash-command-driven). */
  readonly typeMenuRequest: number
  refresh: () => Promise<void>
  open: () => void
  close: () => void
  toggle: () => void
  /** Keep-and-flag warnings from a type switch (surfaces them on the fields). */
  setMismatched: (names: string[]) => void
  setError: (message: string) => void
  /**
   * Monotonic counter the host bumps to request the panel open its type menu
   * (used by the /type slash command). The panel watches it via a prop.
   */
  requestTypeMenu: () => void
  /** Subscribe to `types:changed` + `types:projection-error`; returns a
   *  disposer for onMount cleanup. */
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
  // Monotonic counter the host bumps to request the panel's type menu (the
  // /type slash command). 0 = "no request yet" so the panel's watcher skips
  // its initial run.
  let typeMenuRequest = $state(0)

  // Track the locator that the in-flight refresh targeted so a stale response
  // (the user navigated mid-fetch) is discarded rather than painted over a new
  // page's data.
  let refreshToken = 0
  // Last locator the controller fetched. keep-and-flag warnings describe a
  // type switch on a SPECIFIC page; they must survive a re-fetch of that SAME
  // page (the refresh commitType triggers right after a switch) but clear when
  // the user navigates away. Gate the clear on a locator change, not on every
  // fetch — otherwise the synchronous refresh prologue batches away the
  // mismatched array setMismatched just wrote and the warnings never render.
  let lastLocator = ''

  async function refresh(): Promise<void> {
    const { notebook, section, page } = deps.getLocator()
    if (!notebook || !page) {
      // Bump the token + clear loading BEFORE wiping state so a response from
      // the PREVIOUS page (still in flight when the user navigated here) is
      // discarded by the token check rather than repainting the cleared view.
      refreshToken++
      loading = false
      info = EMPTY_INFO
      values = []
      mismatched = []
      error = ''
      lastLocator = ''
      return
    }
    const locatorKey = `${notebook}/${section}/${page}`
    const locatorChanged = locatorKey !== lastLocator
    lastLocator = locatorKey
    // Full wipe ONLY on locator change (page→page nav). A same-page refresh
    // (post-commit onChanged, types:changed, etc.) must keep info/values so
    // PropertyField instances stay mounted — wiping would flash "Loading…",
    // steal focus, and discard in-progress edits in sibling fields. The panel
    // only shows the skeleton when `loading && values.length === 0`.
    // mismatched clears ONLY on locator change (see lastLocator rationale).
    if (locatorChanged) {
      info = EMPTY_INFO
      values = []
      mismatched = []
      error = ''
    } else {
      error = ''
    }
    const token = ++refreshToken
    // Skeleton only when there is nothing on screen yet (first load / after wipe).
    if (values.length === 0) loading = true
    try {
      const [typeInfo, props] = await Promise.all([
        GetPageType(notebook, section, page),
        GetPageProperties(notebook, section, page)
      ])
      if (token !== refreshToken) return
      const nextInfo = (typeInfo as PageTypeInfo) ?? EMPTY_INFO
      const nextValues = (props as PagePropertyValue[]) ?? []
      // Schema change (type switch / property set reshape): replace wholesale.
      // Same schema: still assign the new arrays so values update, but the
      // panel's keyed {#each values as v (v.name)} keeps matching field
      // instances mounted — focus and sibling edits survive.
      info = nextInfo
      values = nextValues
      // Do NOT clear mismatched here: a same-page fetch (the post-switch
      // refresh) must preserve the warnings commitType just set. They clear
      // on navigation (locatorChanged) or are replaced by the next switch.
      error = ''
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

  function requestTypeMenu(): void {
    typeMenuRequest++
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
    const offChanged = Events.On(EventName.EventTypesChanged, () => {
      if (timer !== null) clearTimeout(timer)
      timer = setTimeout(() => {
        timer = null
        void loadTypes()
        void refresh()
      }, 100)
    })
    // A failed post-write re-parse leaves the type dashboard stale until the
    // next scan. The write itself succeeded, so this is a polite status, not
    // an error alarm: refresh the affected surface and surface a transient
    // toast via the app-wide notification channel.
    const offProjectionError = Events.On(
      EventName.EventTypesProjectionError,
      () => {
        void loadTypes()
        void refresh()
        pushNotification({
          kind: 'info',
          message:
            'A recent edit is being re-indexed. Type dashboards may be briefly stale; refreshing.'
        })
      }
    )
    return () => {
      if (timer !== null) {
        clearTimeout(timer)
        timer = null
      }
      offChanged()
      offProjectionError()
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
    get typeMenuRequest() {
      return typeMenuRequest
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
    requestTypeMenu,
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
