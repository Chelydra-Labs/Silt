// Reactive controller for TipTapEditor's five suggestion-popover typeaheads:
// task-metadata (%), @-mention, block-reference ((())), #tag, and page-link
// ([[]]). Each popover is a state machine with the same shape — a reactive
// popup cell (null when closed), an item list, a highlighted index navigated
// by arrow keys, an onChange query handler fed by the TipTap Suggest
// extension, a selectActive/pick apply path, and (for the server-backed ones)
// a debounce timer + request-generation race guard that drops superseded
// results.
//
// Behaviour-preserving relocation from TipTapEditor.svelte: the five hand-
// rolled copies collapse into one parameterised builder
// (createSuggestController) that owns the shared scaffolding — the popup
// $state cell, the up/down index cycle, the close path, the editor-liveness
// helper, and the shared live-region status string. Each popover's unique
// filtering / IPC / error logic lives in its own thin factory
// (createMetaSuggest, createMentionSuggest, …) layered on top of the builder
// via the returned api. Same accessor-injection pattern as
// createPopoversController / createEditorEvents: a factory that closes over
// its deps; state is exposed via getters so template reads stay reactive
// (returning $state in a plain object would snapshot the initial value). The
// factory runs during the component's init so $effect / $derived register
// against the component's effect scope (not orphaned).
import { untrack } from 'svelte'
import type { Editor } from 'svelte-tiptap'
import {
  createRequestRace,
  createDebouncedRunner,
  cycleSelected,
  ctxStillMatches
} from '../../../lib/editor/useSuggestPopup.svelte'
import {
  filterMetaKeys,
  applyMetaSuggestion,
  filterOwners,
  applyMentionSuggestion,
  applyBlockRefSuggestion,
  filterTags,
  flattenTagHierarchy,
  applyTagSuggestion,
  applyPageLinkSuggestion,
  dismissPageLinkSuggestion
} from '../../../lib/editor'
import type {
  MetaKey,
  SuggestContext,
  MentionContext,
  BlockRefContext,
  TagContext,
  TagItem,
  TagTreeNode,
  PageLinkContext,
  PageLinkItem
} from '../../../lib/editor'
import {
  DistinctOwners,
  SearchBlocks,
  QueryTagHierarchy,
  RecordTagUsage,
  SearchPages,
  ResolvePageLink
} from '../../../../bindings/silt/app.js'
import { settings } from '../../../settings/store.svelte'

// --- Popup shapes ---------------------------------------------------------

/** Block-search row returned by the SearchBlocks IPC. */
export interface BlockSearchItem {
  id: string
  source: string
  notebook: string
  section: string
  page: string
  clean_content: string
}

interface MetaPopup {
  ctx: SuggestContext
  items: MetaKey[]
  selected: number
}

interface MentionPopup {
  ctx: MentionContext
  items: string[]
  selected: number
}

interface BlockRefPopup {
  ctx: BlockRefContext
  items: BlockSearchItem[]
  selected: number
  searching: boolean
  error: boolean
}

interface TagPopup {
  ctx: TagContext
  items: TagItem[]
  selected: number
}

interface PageLinkPopup {
  ctx: PageLinkContext
  items: PageLinkItem[]
  selected: number
  searching: boolean
  resolving: boolean
  resolvingItem: PageLinkItem | null
  error: 'search' | 'resolve' | null
  aliasEnabled: boolean
  alias: string
}

// --- Deps -----------------------------------------------------------------

export interface SuggestDeps {
  /** Current ProseMirror editor (bindable prop; may be null during teardown).
   *  Re-read on every handler call so deferred callbacks see the live editor
   *  after an edit↔source switch, not a stale capture. */
  getEditor: () => Editor | null
}

/** Internal: the shared status writer is threaded in from useSuggests so all
 *  five popovers feed the single live-region string. */
interface FactoryDeps {
  getEditor: () => Editor | null
  setStatus: (status: string) => void
}

// --- Shared builder -------------------------------------------------------

/**
 * Shared scaffolding for the five suggest popovers. Owns only the pieces that
 * are genuinely identical across all five: the reactive popup cell (null when
 * closed), the up/down index cycle, the close path (popup=null + clear
 * status), and accessors for the live editor and the shared status string.
 * Each popover's unique filtering / IPC / error logic is layered on top by
 * its own factory via the returned api.
 */
function createSuggestController<
  TPopup extends { selected: number; items: { length: number } }
>(opts: {
  getEditor: () => Editor | null
  setStatus: (status: string) => void
  /** Optional gate checked before cycling the highlighted index — pageLink
   *  blocks navigation while a link resolve is in flight. */
  canNavigate?: (popup: TPopup) => boolean
}) {
  const { getEditor, setStatus, canNavigate } = opts
  let popup = $state<TPopup | null>(null)

  function navigate(dir: 1 | -1): void {
    if (!popup) return
    if (canNavigate && !canNavigate(popup)) return
    const next = cycleSelected(popup.selected, dir, popup.items.length)
    if (next !== null) popup.selected = next
  }

  // The onChange(null) tail: drop the popup + clear the shared status string.
  // Controllers that own in-flight work (debounce / race / request) cancel it
  // BEFORE calling close() so the ordering matches the original inline code.
  function close(): void {
    popup = null
    setStatus('')
  }

  return {
    get popup(): TPopup | null {
      return popup
    },
    setPopup(next: TPopup | null): void {
      popup = next
    },
    navigate,
    close,
    getEditor,
    editorAlive(): boolean {
      const editor = getEditor()
      return !!editor && !editor.isDestroyed
    },
    setStatus
  }
}

// --- Task metadata suggest (%-autocomplete) -------------------------------

function createMetaSuggest(deps: FactoryDeps) {
  const ctrl = createSuggestController<MetaPopup>({
    getEditor: deps.getEditor,
    setStatus: deps.setStatus
  })

  // `popup` is null when closed. While open it carries the active context
  // (range/position), the filtered key list, and the highlighted index.
  function onChange(ctx: SuggestContext | null): void {
    if (!ctx) {
      ctrl.close()
      return
    }
    const items = filterMetaKeys(ctx.query)
    ctrl.setPopup(items.length === 0 ? null : { ctx, items, selected: 0 })
    deps.setStatus(
      items.length
        ? `${items.length} metadata key${items.length === 1 ? '' : 's'} available`
        : 'No matching metadata keys'
    )
  }

  function selectActive(): void {
    const editor = ctrl.getEditor()
    const p = ctrl.popup
    if (!p || !editor || editor.isDestroyed) {
      ctrl.setPopup(null)
      return
    }
    const item = p.items[p.selected]
    ctrl.setPopup(null)
    if (item) applyMetaSuggestion(editor, item.key)
  }

  function pick(key: string): void {
    const editor = ctrl.getEditor()
    if (!editor || editor.isDestroyed) {
      ctrl.setPopup(null)
      return
    }
    ctrl.setPopup(null)
    applyMetaSuggestion(editor, key)
  }

  return {
    get popup() {
      return ctrl.popup
    },
    onChange,
    navigate: ctrl.navigate,
    selectActive,
    pick
  }
}

// --- @-mention typeahead (#184, #332) -------------------------------------
// Owners come from the read-only DistinctOwners index projection. #332 fixes
// two scale problems: (1) the unbounded SELECT was narrowed to a server-side
// prefix filter, and (2) the per-focus re-fetch was replaced by a TTL cache +
// in-flight guard so a focus blip within OWNERS_TTL_MS reuses the cached set
// instead of round-tripping through SQLite over IPC.

function createMentionSuggest(deps: FactoryDeps) {
  const ctrl = createSuggestController<MentionPopup>({
    getEditor: deps.getEditor,
    setStatus: deps.setStatus
  })

  let owners = $state<string[]>([])
  let ownersLoadedAt = 0
  let ownersLoading = false
  const OWNERS_TTL_MS = 5000 // a focus blip within 5s reuses the cached set
  async function loadOwners(): Promise<void> {
    // TTL + in-flight guard: a rapid focus blip (or repeated onFocus) reuses
    // the cached set instead of re-querying SQLite over IPC every time (#332).
    if (ownersLoading) return
    if (Date.now() - ownersLoadedAt < OWNERS_TTL_MS) return
    ownersLoading = true
    try {
      owners = (await DistinctOwners('')) ?? []
      ownersLoadedAt = Date.now()
    } catch (e) {
      console.error('DistinctOwners failed:', e)
    } finally {
      ownersLoading = false
    }
  }

  // Debounces the onFocus owner re-fetch so a focus blip doesn't immediately
  // trigger an IPC round-trip. Cleared on destroy. #332.
  let focusLoadTimer: ReturnType<typeof setTimeout> | null = null
  function refreshOwners(): void {
    if (focusLoadTimer) clearTimeout(focusLoadTimer)
    focusLoadTimer = setTimeout(() => void loadOwners(), 150)
  }

  // Debounced, race-guarded server refine for non-empty mention queries. The
  // instant popup comes from the cached full set (filterOwners stays pure); for
  // a non-empty query we also fire a prefix-bounded DistinctOwners(query) so a
  // 10k-owner vault never has to filter client-side. The req-id gate discards a
  // late-resolving fetch whose result no longer matches the current popup (#332).
  const mentionDebounce = createDebouncedRunner()
  const mentionRace = createRequestRace()
  const MENTION_QUERY_DEBOUNCE_MS = 120

  function onChange(ctx: MentionContext | null): void {
    mentionDebounce.cancel()
    if (!ctx) {
      ctrl.close()
      return
    }
    // Preserve the highlighted owner across keystrokes: if the previously
    // selected owner is still in the new list, keep it highlighted; otherwise
    // fall back to the top. Without this, typing after arrow-navigating snapped
    // the highlight back to item 0 every keystroke (#332 review feedback).
    const prevName = ctrl.popup
      ? ctrl.popup.items[ctrl.popup.selected]
      : undefined
    const pickSelected = (items: string[]): number => {
      if (!prevName) return 0
      const idx = items.indexOf(prevName)
      return idx >= 0 ? idx : 0
    }
    // Instant feedback from the cached full set — small vaults never wait.
    const instant = filterOwners(owners, ctx.query)
    ctrl.setPopup(
      instant.length === 0
        ? null
        : { ctx, items: instant, selected: pickSelected(instant) }
    )
    deps.setStatus(
      instant.length
        ? `${instant.length} owner${instant.length === 1 ? '' : 's'} available`
        : 'No matching owners'
    )

    // For a non-empty query, refine from the server (prefix filter bounds the
    // result at scale so a 10k-owner vault never filters client-side). Debounced
    // + race-guarded: a stale result cannot overwrite the current popup.
    const q = ctx.query.trim()
    if (q) {
      const myId = mentionRace.begin()
      mentionDebounce.schedule(MENTION_QUERY_DEBOUNCE_MS, () => {
        void (async () => {
          try {
            const serverItems = (await DistinctOwners(q)) ?? []
            // Superseded by a later keystroke — drop this result.
            if (!mentionRace.isCurrent(myId)) return
            // Only apply if the popup is still open for this same context/query.
            const cur = ctrl.popup
            if (!cur || !ctxStillMatches(cur, ctx)) return
            ctrl.setPopup(
              serverItems.length === 0
                ? null
                : {
                    ctx,
                    items: serverItems,
                    selected: pickSelected(serverItems)
                  }
            )
            deps.setStatus(
              serverItems.length
                ? `${serverItems.length} owner${serverItems.length === 1 ? '' : 's'} available`
                : 'No matching owners'
            )
          } catch (e) {
            console.error('DistinctOwners(prefix) failed:', e)
          }
        })()
      })
    }
  }

  function selectActive(): void {
    const editor = ctrl.getEditor()
    const p = ctrl.popup
    if (!p || !editor || editor.isDestroyed) {
      ctrl.setPopup(null)
      return
    }
    const item = p.items[p.selected]
    ctrl.setPopup(null)
    if (item) applyMentionSuggestion(editor, item)
  }

  function pick(name: string): void {
    const editor = ctrl.getEditor()
    if (!editor || editor.isDestroyed) {
      ctrl.setPopup(null)
      return
    }
    ctrl.setPopup(null)
    applyMentionSuggestion(editor, name)
  }

  function destroy(): void {
    mentionDebounce.cancel()
    if (focusLoadTimer) {
      clearTimeout(focusLoadTimer)
      focusLoadTimer = null
    }
  }

  return {
    get popup() {
      return ctrl.popup
    },
    get owners() {
      return owners
    },
    onChange,
    navigate: ctrl.navigate,
    selectActive,
    pick,
    loadOwners,
    refreshOwners,
    destroy
  }
}

// --- Block-reference typeahead --------------------------------------------

function createBlockRefSuggest(deps: FactoryDeps) {
  const ctrl = createSuggestController<BlockRefPopup>({
    getEditor: deps.getEditor,
    setStatus: deps.setStatus
  })

  const blkRefDebounce = createDebouncedRunner()
  const blkRefRace = createRequestRace()
  let blkRefRequest:
    | (Promise<BlockSearchItem[]> & { cancel?: () => Promise<void> | void })
    | null = null
  const BLOCK_REF_QUERY_DEBOUNCE_MS = 180

  function cancelBlockRefSearch(): void {
    blkRefDebounce.cancel()
    const request = blkRefRequest
    blkRefRequest = null
    if (request?.cancel) void request.cancel()
  }

  function onChange(ctx: BlockRefContext | null): void {
    cancelBlockRefSearch()
    const myId = blkRefRace.begin()
    if (!ctx) {
      ctrl.close()
      return
    }

    ctrl.setPopup({
      ctx,
      items: [],
      selected: 0,
      searching: ctx.query.trim().length > 0,
      error: false
    })
    if (!ctx.query.trim()) {
      deps.setStatus('Type to search for a block')
      return
    }

    deps.setStatus('Searching blocks')
    blkRefDebounce.schedule(BLOCK_REF_QUERY_DEBOUNCE_MS, () => {
      void (async () => {
        const request = SearchBlocks(ctx.query) as Promise<
          BlockSearchItem[]
        > & {
          cancel?: () => Promise<void> | void
        }
        blkRefRequest = request
        try {
          const items = (await request) ?? []
          if (!blkRefRace.isCurrent(myId)) return
          const current = ctrl.popup
          if (!current || !ctxStillMatches(current, ctx)) return
          blkRefRequest = null
          ctrl.setPopup({ ...current, items, selected: 0, searching: false })
          deps.setStatus(
            items.length
              ? `${items.length} block${items.length === 1 ? '' : 's'} available`
              : 'No matching blocks'
          )
        } catch (error) {
          if (!blkRefRace.isCurrent(myId)) return
          blkRefRequest = null
          const current = ctrl.popup
          if (!current || !ctxStillMatches(current, ctx)) return
          console.error('SearchBlocks failed:', error)
          ctrl.setPopup({
            ...current,
            items: [],
            selected: 0,
            searching: false,
            error: true
          })
          deps.setStatus('Block search unavailable')
        }
      })()
    })
  }

  function selectActive(): void {
    const p = ctrl.popup
    const item = p?.items[p.selected]
    if (item) pick(item.id)
  }

  function pick(blockId: string): void {
    cancelBlockRefSearch()
    blkRefRace.begin()
    ctrl.setPopup(null)
    deps.setStatus('')
    const editor = ctrl.getEditor()
    if (!editor || editor.isDestroyed) return
    applyBlockRefSuggestion(editor, blockId)
  }

  function blockSourceLabel(source?: string): string {
    return source?.startsWith('linked:') ? 'Linked' : 'Vault'
  }

  function destroy(): void {
    cancelBlockRefSearch()
    blkRefRace.begin()
  }

  return {
    get popup() {
      return ctrl.popup
    },
    get items() {
      const p = ctrl.popup
      return p ? p.items : []
    },
    onChange,
    navigate: ctrl.navigate,
    selectActive,
    pick,
    blockSourceLabel,
    destroy
  }
}

// --- Tag typeahead --------------------------------------------------------
// The hierarchy is stable enough to cache across quick focus changes. A
// fresh focus after the TTL catches tags indexed by recent edits.

function createTagSuggest(deps: FactoryDeps) {
  const ctrl = createSuggestController<TagPopup>({
    getEditor: deps.getEditor,
    setStatus: deps.setStatus
  })

  let tags = $state<TagItem[]>([])
  let tagsLoadedAt = 0
  let tagsLoading = $state(false)
  let tagsLoadError = $state(false)
  const TAGS_TTL_MS = 5000
  let recentTags = $derived(
    (settings.config?.ui as { recent_tags?: string[] } | undefined)
      ?.recent_tags ?? []
  )

  function updateTagPopup(ctx: TagContext): void {
    const p = ctrl.popup
    const previous = p?.items[p.selected]?.path
    const items = filterTags(tags, ctx.query, recentTags)
    const previousIndex = previous
      ? items.findIndex((item) => item.path === previous)
      : -1
    ctrl.setPopup({ ctx, items, selected: Math.max(0, previousIndex) })
    deps.setStatus(
      tagsLoadError
        ? 'Tag suggestions unavailable'
        : tagsLoading
          ? 'Loading tags'
          : items.length
            ? `${items.length} tag${items.length === 1 ? '' : 's'} available`
            : 'No matching tags'
    )
  }

  async function loadTags(): Promise<void> {
    if (tagsLoading || Date.now() - tagsLoadedAt < TAGS_TTL_MS) return
    tagsLoading = true
    tagsLoadError = false
    if (ctrl.popup) updateTagPopup(ctrl.popup.ctx)
    try {
      const tree = ((await QueryTagHierarchy()) ?? []) as TagTreeNode[]
      tags = flattenTagHierarchy(tree)
      tagsLoadedAt = Date.now()
    } catch (error) {
      tagsLoadError = true
      tagsLoadedAt = Date.now()
      console.error('QueryTagHierarchy failed:', error)
    } finally {
      tagsLoading = false
      if (ctrl.popup) updateTagPopup(ctrl.popup.ctx)
    }
  }

  function onChange(ctx: TagContext | null): void {
    if (!ctx) {
      ctrl.close()
      return
    }
    const opening = !ctrl.popup
    updateTagPopup(ctx)
    if (opening) void loadTags()
  }

  function selectActive(): void {
    const p = ctrl.popup
    const item = p?.items[p.selected]
    if (item) pick(item.path)
  }

  function pick(path: string): void {
    ctrl.setPopup(null)
    deps.setStatus('')
    const editor = ctrl.getEditor()
    if (!editor || editor.isDestroyed) return
    if (!applyTagSuggestion(editor, path)) return
    void RecordTagUsage(path).catch((error: unknown) => {
      console.error('RecordTagUsage failed:', error)
    })
  }

  // Config hot reload replaces recent_tags in the shared settings store.
  // Re-rank an open picker in place so the editor and caret stay mounted.
  $effect(() => {
    void recentTags
    const current = untrack(() => ctrl.popup)
    if (current) untrack(() => updateTagPopup(current.ctx))
  })

  return {
    get popup() {
      return ctrl.popup
    },
    get items() {
      const p = ctrl.popup
      return p ? p.items : []
    },
    get tagsLoading() {
      return tagsLoading
    },
    get tagsLoadError() {
      return tagsLoadError
    },
    onChange,
    navigate: ctrl.navigate,
    selectActive,
    pick,
    loadTags
  }
}

// --- Page-link typeahead --------------------------------------------------

function createPageLinkSuggest(deps: FactoryDeps) {
  const ctrl = createSuggestController<PageLinkPopup>({
    getEditor: deps.getEditor,
    setStatus: deps.setStatus,
    // A resolve in flight locks navigation so arrow keys don't move the
    // highlight under a spinner that is about to replace the list.
    canNavigate: (p) => p.items.length > 0 && !p.resolving
  })

  const pageLinkDebounce = createDebouncedRunner()
  const pageLinkRace = createRequestRace()
  let pageLinkRequest: ReturnType<typeof SearchPages> | null = null
  const PAGE_LINK_QUERY_DEBOUNCE_MS = 150

  function hasEnoughQuery(query: string): boolean {
    return (
      Array.from(query).filter((character) => !/\s/u.test(character)).length >=
      2
    )
  }

  function cancelPageLinkSearch(): void {
    pageLinkDebounce.cancel()
    const request = pageLinkRequest
    pageLinkRequest = null
    if (request?.cancel) void request.cancel()
  }

  function onChange(ctx: PageLinkContext | null): void {
    cancelPageLinkSearch()
    const myId = pageLinkRace.begin()
    if (!ctx) {
      ctrl.close()
      return
    }

    const previous =
      ctrl.popup?.ctx.triggerPos === ctx.triggerPos ? ctrl.popup : null
    const popup: PageLinkPopup = {
      ctx,
      items: previous?.items ?? [],
      selected: previous
        ? Math.min(previous.selected, Math.max(0, previous.items.length - 1))
        : 0,
      searching: false,
      resolving: false,
      resolvingItem: null,
      error: null,
      aliasEnabled: previous?.aliasEnabled ?? false,
      alias: previous?.alias ?? ''
    }
    ctrl.setPopup(popup)
    if (!hasEnoughQuery(ctx.query)) {
      popup.items = []
      popup.selected = 0
      deps.setStatus('Type at least 2 characters for page suggestions')
      return
    }

    popup.searching = true
    deps.setStatus('Searching pages')
    pageLinkDebounce.schedule(PAGE_LINK_QUERY_DEBOUNCE_MS, () => {
      void (async () => {
        try {
          const request = SearchPages(ctx.query.trim(), 50)
          pageLinkRequest = request
          const items = (await request) ?? []
          if (!pageLinkRace.isCurrent(myId)) return
          const current = ctrl.popup
          if (!current || !ctxStillMatches(current, ctx)) return
          pageLinkRequest = null
          ctrl.setPopup({ ...current, items, selected: 0, searching: false })
          deps.setStatus(
            items.length
              ? `${items.length} page${items.length === 1 ? '' : 's'} available`
              : 'No matching pages'
          )
        } catch (error) {
          if (!pageLinkRace.isCurrent(myId)) return
          pageLinkRequest = null
          const current = ctrl.popup
          if (!current || !ctxStillMatches(current, ctx)) return
          console.error('SearchPages failed:', error)
          ctrl.setPopup({ ...current, searching: false, error: 'search' })
          deps.setStatus('Page search unavailable')
        }
      })()
    })
  }

  function selectActive(): void {
    const p = ctrl.popup
    const item = p?.items[p.selected]
    if (item) void pick(item)
  }

  function aliasKeydown(event: KeyboardEvent): void {
    if (event.isComposing) return

    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      dismissAlias()
      return
    }

    const popup = ctrl.popup
    if (!popup?.items.length || popup.resolving) return

    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      event.stopPropagation()
      ctrl.navigate(event.key === 'ArrowDown' ? 1 : -1)
    } else if (event.key === 'Enter' && popup.items[popup.selected]) {
      event.preventDefault()
      event.stopPropagation()
      selectActive()
    }
  }

  function retry(): void {
    const popup = ctrl.popup
    if (!popup || popup.resolving) return
    if (popup.error === 'resolve' && popup.resolvingItem) {
      void pick(popup.resolvingItem)
      return
    }
    onChange(popup.ctx)
  }

  function dismissAlias(): void {
    cancelPageLinkSearch()
    pageLinkRace.begin()
    ctrl.setPopup(null)
    deps.setStatus('')
    const editor = ctrl.getEditor()
    if (!editor || editor.isDestroyed) return
    dismissPageLinkSuggestion(editor, true)
  }

  async function pick(item: PageLinkItem): Promise<void> {
    const current = ctrl.popup
    const editor = ctrl.getEditor()
    if (!current || current.resolving || !editor?.isEditable) return
    cancelPageLinkSearch()
    const myId = pageLinkRace.begin()
    ctrl.setPopup({
      ...current,
      resolving: true,
      resolvingItem: item,
      error: null
    })
    deps.setStatus('Resolving page link')
    try {
      const alias = current.aliasEnabled
        ? current.alias.trim() || item.page
        : null
      const inserted = await applyPageLinkSuggestion(
        editor,
        item,
        ResolvePageLink,
        alias
      )
      if (!pageLinkRace.isCurrent(myId)) return
      if (!inserted)
        throw new Error('The page-link context is no longer active')
      ctrl.setPopup(null)
      deps.setStatus('')
    } catch (error) {
      if (!pageLinkRace.isCurrent(myId)) return
      console.error('ResolvePageLink failed:', error)
      const popup = ctrl.popup
      if (popup) {
        ctrl.setPopup({ ...popup, resolving: false, error: 'resolve' })
        deps.setStatus('Page link could not be inserted')
      }
    }
  }

  function destroy(): void {
    cancelPageLinkSearch()
    pageLinkRace.begin()
  }

  return {
    get popup() {
      return ctrl.popup
    },
    get items() {
      const p = ctrl.popup
      return p ? p.items : []
    },
    get resolving() {
      return ctrl.popup?.resolving ?? false
    },
    onChange,
    navigate: ctrl.navigate,
    selectActive,
    pick,
    aliasKeydown,
    retry,
    hasEnoughQuery,
    destroy
  }
}

// --- Top-level factory ----------------------------------------------------

export function useSuggests(deps: SuggestDeps) {
  // Visually-hidden live region text shared by all five popovers — typed into
  // by every onChange/pick/error path via setStatus. Exposed as a getter so the
  // template's {suggests.suggestStatus} read tracks the signal.
  let suggestStatus = $state('')
  function setStatus(status: string): void {
    suggestStatus = status
  }

  const factoryDeps: FactoryDeps = { getEditor: deps.getEditor, setStatus }

  const meta = createMetaSuggest(factoryDeps)
  const mention = createMentionSuggest(factoryDeps)
  const blockRef = createBlockRefSuggest(factoryDeps)
  const tag = createTagSuggest(factoryDeps)
  const pageLink = createPageLinkSuggest(factoryDeps)

  return {
    meta,
    mention,
    blockRef,
    tag,
    pageLink,
    get suggestStatus() {
      return suggestStatus
    }
  }
}

export type SuggestsController = ReturnType<typeof useSuggests>
