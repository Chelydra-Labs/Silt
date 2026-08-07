// Reactive controller for the typed-notes surfaces (inline strip + bottom
// panel). Owns the GetPageType/GetPageProperties round-trip, the SetPageType
// keep-and-flag result, the type-picker listing (ListTypes), the panel-open
// flag, the `types:changed` / `block:changed` subscriptions that re-fetch the
// active page when the type set or page content changes externally, and the
// `types:projection-error` subscription that refreshes + warns when a
// post-write re-parse fails.
//
// Built via the proven createX(deps) factory idiom (mirrors
// useGlobalHotkeyDispatch): every read of the active locator is a closure the
// host provides, so the controller has no direct reference to App's $state.
// App drives reactivity by calling refresh() from a $effect on the locator;
// the controller owns only the one-time event subscriptions.
import { Events } from '@wailsio/runtime'
import { EventName } from '../generated/enums'
import {
  GetPageCoreMetadata,
  GetPageProperties,
  GetPageType,
  ListTypes,
  SetPageCoreMetadata
} from '../../bindings/silt/app.js'
import { coerceIPCError } from '../lib/ipcError'
import { pushNotification } from '../notifications/store.svelte'
import type {
  CoreFieldUpdate,
  ListTypesResult,
  PageCoreMetadata,
  PageLocator,
  PagePropertyValue,
  PageTypeInfo,
  TypeDef,
  TypeLoadError
} from './types'

const EMPTY_INFO: PageTypeInfo = {
  typeId: '',
  type: { id: '', name: '', properties: [] },
  isSet: false,
  rawType: ''
}

const EMPTY_CORE: PageCoreMetadata = {
  notebook: '',
  section: '',
  page: '',
  type: '',
  date: '',
  tags: [],
  aliases: [],
  created: '',
  modified: '',
  tagsAreReadOnly: false
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
  /** Per-file load errors from ListTypes (broken type YAML). Empty when clean. */
  readonly typeLoadErrors: TypeLoadError[]
  readonly panelOpen: boolean
  /** Blocking edit modal open flag (#873). Independent of the non-blocking
   *  peek `panelOpen` — both surfaces read the same reactive data; the modal's
   *  translucent backdrop covers the peek while open. */
  readonly modalOpen: boolean
  readonly heroValue: string
  /** Type-independent core metadata (#867). Always defined (EMPTY_CORE when
   *  no page is active). The panel renders this as a Core section above the
   *  type-defined section. */
  readonly core: PageCoreMetadata
  /** Monotonic request counter the panel watches (slash-command-driven). */
  readonly typeMenuRequest: number
  refresh: () => Promise<void>
  open: () => void
  close: () => void
  toggle: () => void
  /** Open the blocking properties edit modal (#873). */
  openModal: () => void
  /** Close the blocking properties edit modal. */
  closeModal: () => void
  /** Keep-and-flag warnings from a type switch (surfaces them on the fields). */
  setMismatched: (names: string[]) => void
  setError: (message: string) => void
  /** Apply a field-granular core-metadata update (#867) and refetch the core
   *  payload. The panel calls this for each editable core field edit. */
  commitCore: (update: CoreFieldUpdate) => Promise<void>
  /**
   * Monotonic counter the host bumps to request the panel open its type menu
   * (used by the /type slash command). The panel watches it via a prop.
   */
  requestTypeMenu: () => void
  /** Subscribe to `types:changed` / `block:changed` / `types:projection-error`;
   *  returns a disposer for onMount cleanup. */
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
  let typeLoadErrors = $state<TypeLoadError[]>([])
  let panelOpen = $state(false)
  // Blocking edit modal flag (#873). The peek (panelOpen) stays non-blocking;
  // the modal is a separate, focused-edit surface that reuses the same data.
  let modalOpen = $state(false)
  // Core metadata (#867). Always defined — EMPTY_CORE when no page is active
  // or before the first fetch lands. Wiped on locator change so a stale prior
  // page's core fields never paint over the new page.
  let core = $state<PageCoreMetadata>(EMPTY_CORE)
  // The locator of the page whose data is currently in `core` (captured when
  // the core payload lands, NOT on every wipe). commitCore reads THIS, not
  // deps.getLocator(), so an in-flight blur-commit on page A's unmounting
  // input cannot route A's draft to page B when the user navigates A→B
  // mid-edit. Left untouched on locator-change wipes: the {#key pageLocator}
  // gate in CoreMetadataSection discards A's draft visually, and a stale-edit
  // write should land on A (whose data seeded it) — never be silently
  // retargeted to B by the post-navigation locator.
  let coreLocator: PageLocator = { notebook: '', section: '', page: '' }
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
      core = EMPTY_CORE
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
      // A page switch ends any in-flight focused-edit session: the modal
      // edits one page's properties, so navigating away closes it (the
      // peek is non-blocking and intentionally follows the active page).
      // Without this the modal would stay mounted over the wiped info/values
      // below and briefly paint empty fields during the re-fetch.
      closeModal()
      info = EMPTY_INFO
      values = []
      mismatched = []
      error = ''
      core = EMPTY_CORE
    } else {
      error = ''
    }
    const token = ++refreshToken
    // Skeleton only when there is nothing on screen yet (first load / after wipe).
    if (values.length === 0) loading = true
    try {
      const [typeInfo, props, coreMeta] = await Promise.all([
        GetPageType(notebook, section, page),
        GetPageProperties(notebook, section, page),
        GetPageCoreMetadata(notebook, section, page)
      ])
      if (token !== refreshToken) return
      const nextInfo = (typeInfo as PageTypeInfo) ?? EMPTY_INFO
      const nextValues = (props as PagePropertyValue[]) ?? []
      const nextCore = (coreMeta as PageCoreMetadata | null) ?? EMPTY_CORE
      // Normalize tags/aliases to non-null arrays so the panel's .length / map
      // never NPEs on a stale or partial payload.
      if (!Array.isArray(nextCore.tags)) nextCore.tags = []
      if (!Array.isArray(nextCore.aliases)) nextCore.aliases = []
      // Schema change (type switch / property set reshape): replace wholesale.
      // Same schema: still assign the new arrays so values update, but the
      // panel's keyed {#each values as v (v.name)} keeps matching field
      // instances mounted — focus and sibling edits survive.
      info = nextInfo
      values = nextValues
      core = nextCore
      // Capture the page this core payload belongs to. commitCore uses this
      // (not deps.getLocator() at commit time) so a navigation A→B during an
      // in-flight edit cannot retarget A's draft to B.
      coreLocator = {
        notebook: nextCore.notebook,
        section: nextCore.section,
        page: nextCore.page
      }
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
      // Normalize errors: backend sends {file,message}[]; tolerate legacy
      // string[] shapes so a stale binding never crashes the picker.
      const raw = res?.errors ?? []
      typeLoadErrors = raw.map((e) =>
        typeof e === 'string'
          ? { file: '', message: e }
          : {
              file: e.file ?? '',
              message:
                typeof e.message === 'string' ? e.message : 'unknown error'
            }
      )
    } catch (e) {
      // A failed listing is non-fatal — the menu just shows what it has. The
      // banner is reserved for edit failures the user can act on.
      console.error('types: ListTypes failed:', e)
      typeLoadErrors = []
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

  function openModal(): void {
    modalOpen = true
  }

  function closeModal(): void {
    modalOpen = false
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

  // commitCore applies a field-granular core-metadata update (#867) via the
  // IPC setter, then refetches only the core payload (info/values stay
  // mounted — a core edit does not reshape the type-defined section). Reuses
  // the same refreshToken so a stale response is discarded. Errors surface
  // through the existing error banner via setError.
  async function commitCore(update: CoreFieldUpdate): Promise<void> {
    // Source the write target from `coreLocator` (the page whose data is in
    // `core`, captured at load time) — NOT deps.getLocator() at commit time.
    // If the user edited tags/aliases on page A then navigated to page B
    // before the blur-commit fired, the blur lands on A's unmounting input
    // while deps.getLocator() already returns B; without this guard A's
    // draft would silently write to B. The {#key pageLocator} remount in
    // CoreMetadataSection discards A's draft visually; the write must land
    // on A (the page whose data seeded it) or be skipped — never retargeted.
    const { notebook, section, page } = coreLocator
    if (!notebook || !page) return
    try {
      // The Wails JSDoc generator emits the CoreFieldUpdate model fields as
      // required-but-nullable (`T | null | undefined`) rather than optional,
      // so the local Partial-style interface is structurally incompatible at
      // the type layer even though the runtime shape is identical (omitted
      // keys / null / value). Cast through the bindings model at the boundary.
      await SetPageCoreMetadata(
        notebook,
        section,
        page,
        update as unknown as Parameters<typeof SetPageCoreMetadata>[3]
      )
    } catch (e) {
      setError(coerceIPCError(e).message)
      // Rethrow so CoreMetadataSection.commit()'s catch fires — that path
      // renders the aria-live banner (onError → liveError) and bumps
      // rollbackNonce so the input re-seeds from the unchanged committed core.
      // Swallowing here left the success path to run: onChanged() cleared the
      // banner and the rejected value lingered as a ghost with no feedback.
      throw e
    }
    // Skip the post-write refetch if the user navigated away from the page
    // whose core was just written — painting the fresh core onto a different
    // page's panel would be a stale snapshot. The write itself already landed
    // on the correct page (coreLocator); only the refetch is gated on the
    // current locator.
    const now = deps.getLocator()
    if (
      now.notebook !== notebook ||
      now.section !== section ||
      now.page !== page
    )
      return
    setError('')
    // Refresh the core payload from disk truth (the write succeeded; re-derive
    // type/date/tags/aliases/created/modified). info/values/mismatched are
    // untouched — a core edit never changes the type-defined fields.
    const token = ++refreshToken
    try {
      const nextCore = (await GetPageCoreMetadata(
        notebook,
        section,
        page
      )) as PageCoreMetadata | null
      if (token !== refreshToken) return
      const c = nextCore ?? EMPTY_CORE
      if (!Array.isArray(c.tags)) c.tags = []
      if (!Array.isArray(c.aliases)) c.aliases = []
      core = c
      coreLocator = {
        notebook: c.notebook,
        section: c.section,
        page: c.page
      }
    } catch (e) {
      if (token !== refreshToken) return
      setError(coerceIPCError(e).message)
    }
  }

  function attach(): () => void {
    void loadTypes()
    // Debounce so a burst of type-file / page-content changes coalesces into
    // one reload + refresh (mirrors the templates store's handler).
    let timer: ReturnType<typeof setTimeout> | null = null
    const scheduleRefresh = (reloadTypes: boolean) => {
      if (timer !== null) clearTimeout(timer)
      timer = setTimeout(() => {
        timer = null
        if (reloadTypes) void loadTypes()
        void refresh()
      }, 100)
    }
    const offChanged = Events.On(EventName.EventTypesChanged, () => {
      scheduleRefresh(true)
    })
    // External/sync/second-window frontmatter edits emit block:changed (not
    // types:changed). Refresh the open panel so a subsequent edit does not
    // overwrite the external value with a stale snapshot.
    const offBlockChanged = Events.On(EventName.EventBlockChanged, (event) => {
      const loc = deps.getLocator()
      if (!loc.page) return
      const ev = event?.data as
        { notebook?: string; section?: string; page?: string } | undefined
      if (
        ev &&
        (ev.notebook != null || ev.section != null || ev.page != null)
      ) {
        if (
          (ev.notebook ?? '') !== (loc.notebook ?? '') ||
          (ev.section ?? '') !== (loc.section ?? '') ||
          (ev.page ?? '') !== (loc.page ?? '')
        ) {
          return
        }
      }
      scheduleRefresh(false)
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
      offBlockChanged()
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
    get typeLoadErrors() {
      return typeLoadErrors
    },
    get panelOpen() {
      return panelOpen
    },
    get modalOpen() {
      return modalOpen
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
    get core() {
      return core
    },
    refresh,
    open,
    close,
    toggle,
    openModal,
    closeModal,
    requestTypeMenu,
    setMismatched,
    setError,
    commitCore,
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
