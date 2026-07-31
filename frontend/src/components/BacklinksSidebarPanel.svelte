<script lang="ts">
  import { onMount } from 'svelte'
  import { Events } from '@wailsio/runtime'
  import { EventName, IPCErrorCode } from '../generated/enums'
  import {
    GetBacklinksPaged,
    GetUnlinkedMentionsPaged,
    PromoteUnlinkedMention
  } from '../../bindings/silt/app.js'
  import { pushNotification } from '../notifications/store.svelte'
  import { coerceIPCError } from '../lib/ipcError'

  export type Backlink = {
    linkKind: 'page' | 'block-ref' | 'embed'
    sourceNotebook: string
    sourceSection: string
    sourcePage: string
    sourceBlockId: string
    snippet: string
    source?: string
  }

  type BacklinksPage = {
    results: Backlink[]
    cursor: string
    hasMore: boolean
  }

  type PagePath = {
    source: string
    notebook: string
    section: string
    page: string
  }

  type UnlinkedMention = {
    source: string
    sourceNotebook: string
    sourceSection: string
    sourcePage: string
    sourceBlockIds: string[]
    /** Parallel to sourceBlockIds — contextual excerpt per matched block. */
    sourceSnippets?: string[]
    matchCount: number
    title: string
    ambiguous: boolean
    candidates?: PagePath[]
    /** True when more leaf collisions exist than candidates[] (capped server-side). */
    candidatesTruncated?: boolean
    /** Full leaf-collision count before the wire cap (0 when not ambiguous). */
    candidatesTotal?: number
  }

  /** True when ch is a Unicode letter, number, or underscore (word char). */
  function isWordChar(ch: string | undefined): boolean {
    if (!ch) return false
    return /[\p{L}\p{N}_]/u.test(ch)
  }

  /**
   * Split snippet into text segments with the first residual plain title match
   * marked. Mirrors backend FirstPlainTitleOccurrence: case-insensitive
   * word-boundary match, skipping hits inside [[…]] wiki-link spans so mixed
   * snippets emphasize the promotable plain occurrence, not the already-linked one.
   */
  function emphasizeTitle(
    snippet: string,
    title: string
  ): { text: string; mark: boolean }[] {
    if (!snippet) return []
    if (!title) return [{ text: snippet, mark: false }]

    // Wiki-link spans for residual skip. Inner body excludes brackets so the
    // span cannot cross nested `[`/`]` — aligned with parser.PageLinkRegex
    // (backend FirstPlainTitleOccurrence). Still covers #heading / |alias.
    const linked: { start: number; end: number }[] = []
    const pageLinkRe = /\[\[[^[\]]*?\]\]/g
    for (const m of snippet.matchAll(pageLinkRe)) {
      if (m.index === undefined) continue
      linked.push({ start: m.index, end: m.index + m[0].length })
    }
    const inLinked = (s: number, e: number) =>
      linked.some((sp) => s < sp.end && e > sp.start)

    const lower = snippet.toLowerCase()
    const needle = title.toLowerCase()
    let from = 0
    let idx = -1
    while (from <= lower.length - needle.length) {
      const at = lower.indexOf(needle, from)
      if (at < 0) break
      const end = at + needle.length
      const before = at > 0 ? snippet[at - 1] : undefined
      const after = end < snippet.length ? snippet[end] : undefined
      if (!isWordChar(before) && !isWordChar(after) && !inLinked(at, end)) {
        idx = at
        break
      }
      from = at + 1
    }
    if (idx < 0) return [{ text: snippet, mark: false }]
    const parts: { text: string; mark: boolean }[] = []
    if (idx > 0) parts.push({ text: snippet.slice(0, idx), mark: false })
    parts.push({
      text: snippet.slice(idx, idx + title.length),
      mark: true
    })
    if (idx + title.length < snippet.length) {
      parts.push({ text: snippet.slice(idx + title.length), mark: false })
    }
    return parts
  }

  function candidatePathLabel(c: PagePath): string {
    return [c.notebook, c.section, c.page].filter(Boolean).join('/')
  }

  function pathLabel(nb: string, sec: string, pg: string): string {
    return [nb, sec, pg].filter(Boolean).join('/')
  }

  /** Drop one block from a mention, keeping sourceBlockIds ∥ sourceSnippets paired. */
  function dropMentionBlock(
    mention: UnlinkedMention,
    blockId: string
  ): UnlinkedMention | null {
    const snips = mention.sourceSnippets ?? []
    const nextIds: string[] = []
    const nextSnips: string[] = []
    let removed = false
    for (let i = 0; i < mention.sourceBlockIds.length; i++) {
      if (!removed && mention.sourceBlockIds[i] === blockId) {
        removed = true
        continue
      }
      nextIds.push(mention.sourceBlockIds[i])
      // Keep parallel length: missing snip slots become ''.
      nextSnips.push(snips[i] ?? '')
    }
    if (!removed) return mention
    if (nextIds.length === 0) return null
    return {
      ...mention,
      sourceBlockIds: nextIds,
      sourceSnippets: nextSnips,
      matchCount: Math.max(0, mention.matchCount - 1)
    }
  }

  type UnlinkedMentionsPage = {
    results: UnlinkedMention[]
    cursor: string
    hasMore: boolean
    /** FTS candidate batch hit the scan cap — residual list may be incomplete. */
    truncated: boolean
    /** Opaque keyset for the next FTS batch when truncated (empty otherwise). */
    scanCursor: string
  }

  // Wails encodes Go structs with encoding/json tags (mostly snake_case). Vitest
  // mocks may still use camelCase. Dual-read at the IPC boundary so production
  // wire and tests both normalize to the camelCase view model below.
  function pick(
    obj: Record<string, unknown> | null | undefined,
    camel: string,
    snake: string
  ): unknown {
    if (!obj) return undefined
    if (camel in obj && obj[camel] !== undefined) return obj[camel]
    if (snake in obj && obj[snake] !== undefined) return obj[snake]
    return undefined
  }

  function asString(v: unknown, fallback = ''): string {
    return typeof v === 'string' ? v : fallback
  }

  function asStringArray(v: unknown): string[] {
    return Array.isArray(v) ? v.map((x) => String(x)) : []
  }

  function asBool(v: unknown): boolean {
    return Boolean(v)
  }

  function asInt(v: unknown, fallback = 0): number {
    return typeof v === 'number' && Number.isFinite(v) ? v : fallback
  }

  function mapBacklink(raw: unknown): Backlink {
    const row = (raw ?? {}) as Record<string, unknown>
    const kind = asString(pick(row, 'linkKind', 'link_kind'), 'page')
    return {
      linkKind:
        kind === 'block-ref' || kind === 'embed' || kind === 'page'
          ? kind
          : 'page',
      source: asString(row.source, 'vault'),
      sourceNotebook: asString(pick(row, 'sourceNotebook', 'source_notebook')),
      sourceSection: asString(pick(row, 'sourceSection', 'source_section')),
      sourcePage: asString(pick(row, 'sourcePage', 'source_page')),
      sourceBlockId: asString(pick(row, 'sourceBlockId', 'source_block_id')),
      snippet: asString(row.snippet)
    }
  }

  function mapPagePath(raw: unknown): PagePath {
    const row = (raw ?? {}) as Record<string, unknown>
    return {
      source: asString(row.source, 'vault'),
      notebook: asString(row.notebook),
      section: asString(row.section),
      page: asString(row.page)
    }
  }

  function mapUnlinkedMention(raw: unknown): UnlinkedMention {
    const row = (raw ?? {}) as Record<string, unknown>
    const candidatesRaw = pick(row, 'candidates', 'candidates')
    return {
      source: asString(row.source, 'vault'),
      sourceNotebook: asString(pick(row, 'sourceNotebook', 'source_notebook')),
      sourceSection: asString(pick(row, 'sourceSection', 'source_section')),
      sourcePage: asString(pick(row, 'sourcePage', 'source_page')),
      sourceBlockIds: asStringArray(
        pick(row, 'sourceBlockIds', 'source_block_ids')
      ),
      sourceSnippets: asStringArray(
        pick(row, 'sourceSnippets', 'source_snippets')
      ),
      matchCount: asInt(pick(row, 'matchCount', 'match_count'), 0),
      title: asString(row.title),
      ambiguous: asBool(row.ambiguous),
      candidates: Array.isArray(candidatesRaw)
        ? candidatesRaw.map(mapPagePath)
        : undefined,
      candidatesTruncated: asBool(
        pick(row, 'candidatesTruncated', 'candidates_truncated')
      ),
      candidatesTotal: asInt(
        pick(row, 'candidatesTotal', 'candidates_total'),
        0
      )
    }
  }

  function mapBacklinksPage(raw: unknown): BacklinksPage {
    const r = (raw ?? {}) as Record<string, unknown>
    const results = Array.isArray(r.results) ? r.results.map(mapBacklink) : []
    return {
      results,
      cursor: asString(r.cursor),
      hasMore: asBool(pick(r, 'hasMore', 'has_more'))
    }
  }

  function mapUnlinkedMentionsPage(raw: unknown): UnlinkedMentionsPage {
    const r = (raw ?? {}) as Record<string, unknown>
    const results = Array.isArray(r.results)
      ? r.results.map(mapUnlinkedMention)
      : []
    return {
      results,
      cursor: asString(r.cursor),
      hasMore: asBool(pick(r, 'hasMore', 'has_more')),
      truncated: asBool(pick(r, 'truncated', 'truncated')),
      scanCursor: asString(pick(r, 'scanCursor', 'scan_cursor'))
    }
  }

  interface Props {
    notebook: string
    section: string
    page: string
  }

  let { notebook, section, page }: Props = $props()
  let backlinks = $state<Backlink[]>([])
  let initialLoading = $state(false)
  let refreshing = $state(false)
  let loadingMore = $state(false)
  let cursor = $state('')
  let hasMore = $state(false)
  let error = $state('')
  let errorAction = $state<'initial' | 'refresh' | 'more'>('initial')
  let request = 0
  let refreshTimer: ReturnType<typeof setTimeout> | null = null
  const pageSize = 50

  // Unlinked-mentions leg.
  let unlinked = $state<UnlinkedMention[]>([])
  let unlinkedLoading = $state(false)
  let unlinkedExpanded = $state(false)
  let unlinkedBusy = $state<Set<string>>(new Set())
  let unlinkedError = $state('')
  /** Which unlinked fetch failed — drives Try again (reload vs load-more vs scan). */
  let unlinkedErrorAction = $state<'initial' | 'more' | 'scan'>('initial')
  let unlinkedRequest = 0
  let unlinkedCursor = $state('')
  let unlinkedHasMore = $state(false)
  let unlinkedLoadingMore = $state(false)
  let unlinkedScanningMore = $state(false)
  /** Pool-level: FTS batch hit unlinkedScanCap (orthogonal to page hasMore). */
  let unlinkedTruncated = $state(false)
  /** Next FTS batch keyset from the API (Scan more). Empty when not truncated. */
  let unlinkedScanCursor = $state('')
  /**
   * scanCursor INPUT used for the batch currently being residual-paged.
   * Empty string = first batch. Distinct from unlinkedScanCursor (next batch out).
   */
  let unlinkedBatchScanIn = $state('')
  /**
   * One-shot polite status after Scan more finishes the last FTS batch (button
   * unmounts). Cleared on the next load/scan so the live region stays accurate.
   */
  let unlinkedScanStatus = $state('')
  let unlinkedSectionToggleEl = $state<HTMLButtonElement | undefined>(undefined)

  const groups = $derived.by(() => {
    // Ephemeral grouping inside $derived — plain Map is correct.
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local derived helper
    const grouped = new Map<string, { key: string; links: Backlink[] }>()
    for (const link of backlinks) {
      const key = `${link.source ?? 'vault'}\u0000${link.sourceNotebook}\u0000${link.sourceSection}\u0000${link.sourcePage}`
      const current = grouped.get(key)
      if (current) current.links.push(link)
      else grouped.set(key, { key, links: [link] })
    }
    return [...grouped.values()]
  })

  const countLabel = $derived(
    groups.length === 1
      ? '1 page links here'
      : `${groups.length} pages link here`
  )

  async function getBacklinksPage(
    activeNotebook: string,
    activeSection: string,
    activePage: string,
    activeCursor: string
  ): Promise<BacklinksPage> {
    const result = await GetBacklinksPaged(
      activeNotebook,
      activeSection,
      activePage,
      activeCursor,
      pageSize
    )
    return mapBacklinksPage(result)
  }

  function backlinkKey(link: Backlink): string {
    return [
      link.source ?? 'vault',
      link.sourceNotebook,
      link.sourceSection,
      link.sourcePage,
      link.linkKind,
      link.sourceBlockId
    ].join('\u0000')
  }

  function uniqueLinks(current: Backlink[], incoming: Backlink[]): Backlink[] {
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local helper set
    const seen = new Set(current.map(backlinkKey))
    return [
      ...current,
      ...incoming.filter((link) => {
        const key = backlinkKey(link)
        if (seen.has(key)) return false
        seen.add(key)
        return true
      })
    ]
  }

  async function loadFirstPage(
    activeNotebook: string,
    activeSection: string,
    activePage: string,
    retainProjection: boolean
  ): Promise<void> {
    const sequence = ++request
    cursor = ''
    hasMore = false
    loadingMore = false
    error = ''
    errorAction = retainProjection ? 'refresh' : 'initial'
    if (!activeNotebook || !activePage) {
      backlinks = []
      initialLoading = false
      refreshing = false
      return
    }
    if (!retainProjection) backlinks = []
    initialLoading = !retainProjection
    refreshing = retainProjection
    try {
      const result = await getBacklinksPage(
        activeNotebook,
        activeSection,
        activePage,
        ''
      )
      if (sequence !== request) return
      backlinks = uniqueLinks([], result.results ?? [])
      cursor = result.cursor ?? ''
      hasMore = Boolean(result.hasMore)
      error = ''
    } catch (cause) {
      if (sequence !== request) return
      error =
        cause instanceof Error
          ? cause.message
          : 'Backlinks could not be loaded.'
    } finally {
      if (sequence === request) {
        initialLoading = false
        refreshing = false
      }
    }
  }

  function refresh(): void {
    void loadFirstPage(notebook, section, page, backlinks.length > 0)
    // Same-page refresh: keep residual rows + truncated until success/error.
    void loadUnlinked(notebook, section, page, false)
  }

  async function loadMore(): Promise<void> {
    if (loadingMore || !hasMore || !cursor) return
    const sequence = request
    const nextCursor = cursor
    loadingMore = true
    error = ''
    errorAction = 'more'
    try {
      const result = await getBacklinksPage(notebook, section, page, nextCursor)
      if (sequence !== request) return
      backlinks = uniqueLinks(backlinks, result.results ?? [])
      cursor = result.cursor ?? ''
      hasMore = Boolean(result.hasMore)
    } catch (cause) {
      if (sequence !== request) return
      error =
        cause instanceof Error
          ? cause.message
          : 'More backlinks could not be loaded.'
    } finally {
      if (sequence === request) loadingMore = false
    }
  }

  async function getUnlinkedPage(
    activeNotebook: string,
    activeSection: string,
    activePage: string,
    activeCursor: string,
    activeScanCursor: string
  ): Promise<UnlinkedMentionsPage> {
    const result = await GetUnlinkedMentionsPaged(
      activeNotebook,
      activeSection,
      activePage,
      activeCursor,
      activeScanCursor,
      pageSize
    )
    return mapUnlinkedMentionsPage(result)
  }

  function clearUnlinkedProjection(): void {
    unlinked = []
    unlinkedCursor = ''
    unlinkedHasMore = false
    unlinkedTruncated = false
    unlinkedScanCursor = ''
    unlinkedBatchScanIn = ''
    unlinkedScanStatus = ''
  }

  // loadUnlinked fetches the unlinked-mentions leg. Runs alongside the backlinks
  // refresh (same debounced block:changed trigger) but with its own request
  // sequence so a slow backlinks page never suppresses an unlinked refresh.
  //
  // resetProjection: true on page navigation (drop prior rows/flags immediately).
  // false on same-page refresh so a failed reload keeps residual rows and the
  // incompleteness cue (mirrors backlinks retain-on-error).
  async function loadUnlinked(
    activeNotebook: string,
    activeSection: string,
    activePage: string,
    resetProjection = false
  ): Promise<void> {
    const sequence = ++unlinkedRequest
    if (!activeNotebook || !activePage) {
      clearUnlinkedProjection()
      unlinkedError = ''
      return
    }
    unlinkedLoading = true
    // Full reload supersedes in-flight Load more / Scan more (sequence bump alone
    // would leave loading flags stuck true in their finally blocks).
    unlinkedLoadingMore = false
    unlinkedScanningMore = false
    unlinkedError = ''
    unlinkedErrorAction = 'initial'
    unlinkedScanStatus = ''
    if (resetProjection) {
      clearUnlinkedProjection()
    }
    try {
      const page = await getUnlinkedPage(
        activeNotebook,
        activeSection,
        activePage,
        '',
        ''
      )
      if (sequence !== unlinkedRequest) return
      unlinked = page.results ?? []
      unlinkedCursor = page.cursor ?? ''
      unlinkedHasMore = Boolean(page.hasMore)
      unlinkedTruncated = Boolean(page.truncated)
      unlinkedScanCursor = page.scanCursor ?? ''
      unlinkedBatchScanIn = ''
    } catch (cause) {
      if (sequence !== unlinkedRequest) return
      unlinkedErrorAction = 'initial'
      unlinkedError =
        cause instanceof Error
          ? cause.message
          : 'Unlinked mentions could not be loaded.'
      // Keep prior unlinked rows + truncated flag on failed refresh.
    } finally {
      if (sequence === unlinkedRequest) unlinkedLoading = false
    }
  }

  function unlinkedMentionKey(mention: UnlinkedMention): string {
    return [
      mention.source ?? 'vault',
      mention.sourceNotebook,
      mention.sourceSection,
      mention.sourcePage
    ].join('\u0000')
  }

  // Merge residual/scan pages: same source page can reappear with additional
  // blocks (within a batch or across Scan more). Concatenate ids/snippets.
  function mergeUnlinked(
    current: UnlinkedMention[],
    incoming: UnlinkedMention[]
  ): UnlinkedMention[] {
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local helper map
    const byKey = new Map<string, UnlinkedMention>()
    const order: string[] = []
    for (const m of current) {
      const k = unlinkedMentionKey(m)
      byKey.set(k, {
        ...m,
        sourceBlockIds: [...m.sourceBlockIds],
        sourceSnippets: [...(m.sourceSnippets ?? [])]
      })
      order.push(k)
    }
    for (const m of incoming) {
      const k = unlinkedMentionKey(m)
      const ex = byKey.get(k)
      if (!ex) {
        byKey.set(k, {
          ...m,
          sourceBlockIds: [...m.sourceBlockIds],
          sourceSnippets: [...(m.sourceSnippets ?? [])]
        })
        order.push(k)
        continue
      }
      // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local helper set
      const seenIds = new Set(ex.sourceBlockIds)
      const snippets = ex.sourceSnippets ?? []
      const inSnips = m.sourceSnippets ?? []
      for (let i = 0; i < m.sourceBlockIds.length; i++) {
        const id = m.sourceBlockIds[i]
        if (seenIds.has(id)) continue
        seenIds.add(id)
        ex.sourceBlockIds.push(id)
        snippets.push(inSnips[i] ?? '')
      }
      ex.sourceSnippets = snippets
      ex.matchCount = ex.sourceBlockIds.length
    }
    return order.map((k) => byKey.get(k)!)
  }

  async function loadMoreUnlinked(
    activeNotebook: string,
    activeSection: string,
    activePage: string
  ): Promise<void> {
    // Match backlinks loadMore: snapshot sequence without bumping so a concurrent
    // full reload (++unlinkedRequest) supersedes this page without leaving
    // unlinkedLoadingMore stuck true.
    if (
      unlinkedLoading ||
      unlinkedLoadingMore ||
      unlinkedScanningMore ||
      !unlinkedHasMore ||
      !unlinkedCursor
    ) {
      return
    }
    const sequence = unlinkedRequest
    const nextCursor = unlinkedCursor
    const batchScan = unlinkedBatchScanIn
    unlinkedLoadingMore = true
    unlinkedError = ''
    unlinkedErrorAction = 'more'
    // One-shot final-scan status must not pin the subtitle during residual paging.
    unlinkedScanStatus = ''
    try {
      const page = await getUnlinkedPage(
        activeNotebook,
        activeSection,
        activePage,
        nextCursor,
        batchScan
      )
      if (sequence !== unlinkedRequest) return
      // Merge (not drop-dedup): a page already listed can gain more blocks on
      // a later residual page of the same FTS batch.
      unlinked = mergeUnlinked(unlinked, page.results ?? [])
      unlinkedCursor = page.cursor ?? ''
      unlinkedHasMore = Boolean(page.hasMore)
      // Pool-level flag: keep true if either page reports the FTS cap.
      unlinkedTruncated = unlinkedTruncated || Boolean(page.truncated)
      if (page.scanCursor) unlinkedScanCursor = page.scanCursor
    } catch (cause) {
      if (sequence !== unlinkedRequest) return
      unlinkedErrorAction = 'more'
      unlinkedError =
        cause instanceof Error
          ? cause.message
          : 'More unlinked mentions could not be loaded.'
    } finally {
      if (sequence === unlinkedRequest) unlinkedLoadingMore = false
    }
  }

  // scanMoreUnlinked fetches the next capped FTS candidate batch (beyond the
  // current window). Residual Load more stays on unlinkedBatchScanIn; this path
  // advances the batch and merges residual pages (same page may gain blocks).
  // Blocked while residual has_more so unread pages in the current batch are
  // not abandoned when the residual cursor resets to the next batch.
  async function scanMoreUnlinked(
    activeNotebook: string,
    activeSection: string,
    activePage: string
  ): Promise<void> {
    if (
      unlinkedLoading ||
      unlinkedLoadingMore ||
      unlinkedScanningMore ||
      unlinkedHasMore ||
      !unlinkedTruncated ||
      !unlinkedScanCursor
    ) {
      return
    }
    const sequence = unlinkedRequest
    const nextScan = unlinkedScanCursor
    unlinkedScanningMore = true
    unlinkedError = ''
    unlinkedErrorAction = 'scan'
    unlinkedScanStatus = ''
    try {
      const page = await getUnlinkedPage(
        activeNotebook,
        activeSection,
        activePage,
        '',
        nextScan
      )
      if (sequence !== unlinkedRequest) return
      const beforeCount = unlinked.length
      unlinked = mergeUnlinked(unlinked, page.results ?? [])
      unlinkedCursor = page.cursor ?? ''
      unlinkedHasMore = Boolean(page.hasMore)
      unlinkedTruncated = Boolean(page.truncated)
      unlinkedScanCursor = page.scanCursor ?? ''
      unlinkedBatchScanIn = nextScan
      // Final batch: Scan more button unmounts — announce and restore focus.
      if (!unlinkedScanCursor) {
        const added = unlinked.length - beforeCount
        unlinkedScanStatus =
          added > 0
            ? `Scan complete. ${added} more page${added === 1 ? '' : 's'} found.`
            : 'Scan complete. No additional pages in this batch.'
        queueMicrotask(() => unlinkedSectionToggleEl?.focus())
      }
    } catch (cause) {
      if (sequence !== unlinkedRequest) return
      unlinkedErrorAction = 'scan'
      unlinkedError =
        cause instanceof Error
          ? cause.message
          : 'More mention candidates could not be scanned.'
    } finally {
      if (sequence === unlinkedRequest) unlinkedScanningMore = false
    }
  }

  function retryUnlinked(): void {
    if (unlinkedErrorAction === 'more') {
      void loadMoreUnlinked(notebook, section, page)
    } else if (unlinkedErrorAction === 'scan') {
      void scanMoreUnlinked(notebook, section, page)
    } else {
      // Retry keeps projection so a second failure does not blank the section.
      void loadUnlinked(notebook, section, page, false)
    }
  }

  // promoteMention wraps the first plain-text mention of the page title in the
  // chosen source block with a [[shortest]] link. target overrides the active
  // page path when the author picks an ambiguous candidate chip. On success the
  // entry migrates into the backlinks leg on the next refresh; optimistic
  // removal keeps the row from flickering until the debounced refresh lands.
  async function promoteMention(
    mention: UnlinkedMention,
    blockId: string,
    target?: { notebook: string; section: string; page: string }
  ) {
    if (unlinkedBusy.has(blockId)) return
    // Non-ambiguous rows use the active page; ambiguous rows require an explicit
    // candidate target (chip click). Guard against a bare Link on ambiguous.
    if (mention.ambiguous && !target) return
    const destNb = target?.notebook ?? notebook
    const destSec = target?.section ?? section
    const destPage = target?.page ?? page
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- immutable reassign triggers $state
    unlinkedBusy = new Set(unlinkedBusy).add(blockId)
    try {
      await PromoteUnlinkedMention(blockId, destNb, destSec, destPage)
      const destLabel = pathLabel(destNb, destSec, destPage)
      pushNotification({
        kind: 'success',
        message: target
          ? `Linked “${mention.title}” as ${destLabel} in ${mention.sourcePage}.`
          : `Linked “${mention.title}” in ${mention.sourcePage}.`
      })
      // Optimistically drop the promoted block with ids∥snippets kept paired;
      // the debounced block:changed refresh reconciles the rest.
      unlinked = unlinked
        .map((m) => (m === mention ? dropMentionBlock(m, blockId) : m))
        .filter((m): m is UnlinkedMention => m != null)
      refresh()
    } catch (cause) {
      const err = coerceIPCError(cause)
      const message =
        err.code === IPCErrorCode.CodeBlockBeingEdited
          ? 'Save or close the file in the editor first, then retry.'
          : err.code === IPCErrorCode.CodeAmbiguousTarget
            ? 'This title now matches multiple pages — pick a candidate to link.'
            : err.message || 'Could not link the mention.'
      pushNotification({ kind: 'error', message })
    } finally {
      // eslint-disable-next-line svelte/prefer-svelte-reactivity -- immutable reassign triggers $state
      const cleared = new Set(unlinkedBusy)
      cleared.delete(blockId)
      unlinkedBusy = cleared
    }
  }

  function openPage(link: Backlink): void {
    window.dispatchEvent(
      new CustomEvent('navigate-to-page', {
        detail: {
          notebook: link.sourceNotebook,
          section: link.sourceSection,
          page: link.sourcePage,
          source: link.source ?? 'vault'
        }
      })
    )
  }

  function openExactBlock(link: Backlink): void {
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: link.sourceNotebook,
          section: link.sourceSection,
          page: link.sourcePage,
          blockId: link.sourceBlockId,
          source: link.source ?? 'vault'
        }
      })
    )
  }

  function kindLabel(kind: Backlink['linkKind']): string {
    if (kind === 'block-ref') return 'Block reference'
    if (kind === 'embed') return 'Embed'
    return 'Page link'
  }

  function kindMark(kind: Backlink['linkKind']): string {
    if (kind === 'block-ref') return '(('
    if (kind === 'embed') return '{{'
    return '[['
  }

  function sourceLabel(source?: string): string {
    return source?.startsWith('linked:') ? 'Linked' : 'Vault'
  }

  onMount(() => {
    const offBlock = Events.On(EventName.EventBlockChanged, () => {
      if (refreshTimer) clearTimeout(refreshTimer)
      refreshTimer = setTimeout(() => {
        refreshTimer = null
        refresh()
      }, 200)
    })
    return () => {
      request++
      unlinkedRequest++
      if (refreshTimer) clearTimeout(refreshTimer)
      offBlock()
    }
  })

  $effect(() => {
    const activeNotebook = notebook
    const activeSection = section
    const activePage = page
    if (refreshTimer) {
      clearTimeout(refreshTimer)
      refreshTimer = null
    }
    unlinkedExpanded = false
    void loadFirstPage(activeNotebook, activeSection, activePage, false)
    // Page navigation: drop prior unlinked projection immediately.
    void loadUnlinked(activeNotebook, activeSection, activePage, true)
  })
</script>

<nav
  aria-label="Backlinks to current page"
  class="flex-grow flex flex-col min-h-0 bg-surface-sidebar"
>
  <header
    class="px-3 py-3 border-b border-surface-sidebar-border flex-shrink-0"
  >
    <div class="flex items-center gap-2">
      <span
        class="material-symbols-outlined text-accent-primary-start text-type-2xl"
        aria-hidden="true">hub</span
      >
      <div class="min-w-0">
        <h2 class="font-headline-md text-headline-md text-text-primary">
          Backlinks
        </h2>
        <p
          class="m-0 text-type-2xs text-surface-sidebar-text-muted font-label-sm"
          aria-live="polite"
          aria-atomic="true"
        >
          {initialLoading
            ? 'Loading backlinks…'
            : refreshing
              ? 'Refreshing backlinks…'
              : countLabel}
        </p>
      </div>
    </div>
  </header>

  <div class="flex-grow overflow-y-auto custom-scrollbar p-2">
    {#if !notebook || !page}
      <div class="text-center px-4 py-10 text-text-muted">
        <span
          class="material-symbols-outlined text-icon-2xl opacity-60"
          aria-hidden="true">draft</span
        >
        <p class="mt-2 text-text-primary font-label-sm-bold">
          Open a page first
        </p>
        <p class="mt-1 text-type-xs leading-relaxed">
          Backlinks follow the page you are viewing.
        </p>
      </div>
    {:else if initialLoading}
      <div
        class="flex flex-col items-center px-4 py-10 text-text-muted"
        role="status"
        aria-live="polite"
      >
        <span
          class="material-symbols-outlined text-icon-2xl text-accent-primary-start animate-pulse"
          aria-hidden="true">hub</span
        >
        <p class="mt-2 text-text-primary font-label-sm-bold">
          Loading backlinks…
        </p>
        <p class="mt-1 text-type-xs">Finding pages that point here.</p>
      </div>
    {:else}
      {#if error}
        <div
          class="m-1 mb-2 p-3 rounded-lg border border-status-warn/35 bg-status-warn/10"
          role="alert"
        >
          <p class="m-0 text-type-xs font-label-sm-bold text-text-primary">
            {errorAction === 'more'
              ? 'More backlinks could not be loaded.'
              : errorAction === 'refresh'
                ? 'Backlinks could not be refreshed.'
                : 'Backlinks could not be loaded.'}
          </p>
          <p class="mt-1 mb-0 text-type-2xs text-text-muted">{error}</p>
          <button
            type="button"
            class="mt-2 p-0 border-none bg-transparent text-accent-primary-start text-type-xs underline cursor-pointer focus-visible:ring-2 focus-visible:ring-accent-primary-start rounded"
            onclick={errorAction === 'more' ? loadMore : refresh}
            >Try again</button
          >
        </div>
      {/if}
      {#if groups.length === 0 && !error}
        <div class="text-center px-4 py-10 text-text-muted">
          <span
            class="material-symbols-outlined text-icon-2xl opacity-60"
            aria-hidden="true">link_off</span
          >
          <p class="mt-2 text-text-primary font-label-sm-bold">
            No pages link here yet
          </p>
          <p class="mt-1 text-type-xs leading-relaxed">
            Link this page with <code class="text-accent-secondary-start"
              >[[page]]</code
            >
            or reference one of its blocks with
            <code class="text-accent-secondary-start">((block))</code>.
          </p>
        </div>
      {:else}
        <div class="flex flex-col gap-2">
          {#each groups as group (group.key)}
            {@const source = group.links[0]}
            <section
              class="rounded-lg border border-surface-sidebar-border bg-surface-panel/35 overflow-hidden"
            >
              <h3 class="m-0">
                <button
                  type="button"
                  class="w-full px-2.5 py-2 border-none bg-transparent text-left flex items-center gap-2 text-surface-sidebar-text hover:bg-hover cursor-pointer transition-colors focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-primary-start"
                  onclick={() => openPage(source)}
                  aria-label={`Open page ${source.sourcePage}`}
                >
                  <span
                    class="material-symbols-outlined text-icon-md text-accent-primary-start"
                    aria-hidden="true">description</span
                  >
                  <span class="min-w-0 flex-1">
                    <span class="flex items-center gap-1.5 min-w-0">
                      <span class="truncate font-label-sm-bold text-label-sm"
                        >{source.sourcePage}</span
                      >
                      <span
                        class="flex-shrink-0 rounded-full border border-surface-sidebar-border bg-surface-sidebar px-1.5 py-0.5 text-type-3xs uppercase tracking-wider text-surface-sidebar-text-muted font-label-sm-bold"
                        >{sourceLabel(source.source)}</span
                      >
                    </span>
                    <span
                      class="block truncate text-type-3xs text-surface-sidebar-text-muted"
                    >
                      {[source.sourceNotebook, source.sourceSection]
                        .filter(Boolean)
                        .join(' / ')}
                    </span>
                  </span>
                  <span
                    class="text-type-3xs text-surface-sidebar-text-muted"
                    aria-label={`${group.links.length} references`}
                    >{group.links.length}</span
                  >
                </button>
              </h3>
              <ul
                class="m-0 p-0 list-none border-t border-surface-sidebar-border/70"
              >
                {#each group.links as link, index (`${link.linkKind}:${link.sourceBlockId}:${index}`)}
                  <li
                    class="flex items-stretch hover:bg-hover transition-colors"
                  >
                    <button
                      type="button"
                      class="group min-w-0 flex-1 px-2.5 py-2 border-none bg-transparent text-left flex items-start gap-2 cursor-pointer focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-primary-start"
                      onclick={() => openPage(link)}
                      aria-label={`Open ${kindLabel(link.linkKind).toLowerCase()} in page ${link.sourcePage}: ${link.snippet}`}
                    >
                      <span
                        class="mt-0.5 min-w-7 text-center rounded bg-accent-primary-start/10 text-accent-primary-start font-mono text-type-2xs font-bold"
                        title={kindLabel(link.linkKind)}
                        aria-hidden="true">{kindMark(link.linkKind)}</span
                      >
                      <span class="min-w-0 flex-1">
                        <span
                          class="block text-type-3xs uppercase tracking-wider text-surface-sidebar-text-muted font-label-sm-bold"
                        >
                          {kindLabel(link.linkKind)}
                        </span>
                        <span
                          class="block mt-0.5 text-type-xs text-surface-sidebar-text leading-snug line-clamp-3"
                        >
                          {link.snippet || 'Reference in this page'}
                        </span>
                      </span>
                      <span
                        class="material-symbols-outlined text-icon-sm text-surface-sidebar-text-muted group-hover:text-accent-primary-start"
                        aria-hidden="true"
                      >
                        arrow_forward
                      </span>
                    </button>
                    {#if link.sourceBlockId}
                      <button
                        type="button"
                        class="self-stretch w-9 flex-shrink-0 border-none border-l border-surface-sidebar-border/70 bg-transparent text-surface-sidebar-text-muted hover:bg-accent-primary-start/10 hover:text-accent-primary-start cursor-pointer transition-colors focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-primary-start"
                        onclick={() => openExactBlock(link)}
                        aria-label={`Jump to exact block for ${kindLabel(link.linkKind).toLowerCase()} in ${link.sourcePage}: ${link.snippet}`}
                        title="Jump to exact block"
                      >
                        <span
                          class="material-symbols-outlined text-icon-sm"
                          aria-hidden="true">my_location</span
                        >
                      </button>
                    {/if}
                  </li>
                {/each}
              </ul>
            </section>
          {/each}
          {#if hasMore}
            <button
              type="button"
              class="w-full rounded-lg border border-surface-sidebar-border bg-surface-panel/35 px-3 py-2 text-label-sm font-label-sm-bold text-accent-primary-start hover:bg-hover cursor-pointer transition-colors disabled:cursor-wait disabled:opacity-65 focus-visible:ring-2 focus-visible:ring-accent-primary-start"
              disabled={loadingMore}
              aria-label={loadingMore
                ? 'Loading more backlinks'
                : 'Load more backlinks'}
              onclick={loadMore}
            >
              {loadingMore ? 'Loading more…' : 'Load more'}
            </button>
          {/if}
        </div>
      {/if}
      {#if unlinked.length > 0 || unlinkedLoading || unlinkedError || unlinkedTruncated}
        <section
          class="mt-2 rounded-lg border border-surface-sidebar-border bg-surface-panel/35 overflow-hidden"
          aria-labelledby="unlinked-mentions-title"
        >
          <h3 class="m-0">
            <button
              type="button"
              bind:this={unlinkedSectionToggleEl}
              class="w-full px-2.5 py-2 border-none bg-transparent text-left flex items-center gap-2 text-surface-sidebar-text hover:bg-hover cursor-pointer transition-colors focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-primary-start"
              aria-expanded={unlinkedExpanded}
              aria-controls="unlinked-mentions-body"
              onclick={() => (unlinkedExpanded = !unlinkedExpanded)}
            >
              <span
                class="material-symbols-outlined text-icon-md text-accent-primary-start transition-transform {unlinkedExpanded
                  ? 'rotate-90'
                  : ''}"
                aria-hidden="true">chevron_right</span
              >
              <span class="min-w-0 flex-1">
                <span
                  id="unlinked-mentions-title"
                  class="block truncate font-label-sm-bold text-label-sm"
                >
                  Unlinked mentions
                </span>
                <span
                  class="block truncate text-type-3xs text-surface-sidebar-text-muted"
                  aria-live="polite"
                  aria-atomic="true"
                >
                  {#if unlinkedLoading}
                    Finding unlinked mentions…
                  {:else if unlinkedScanningMore}
                    Scanning more mentions…
                  {:else if unlinkedScanStatus}
                    {unlinkedScanStatus}
                  {:else if unlinkedTruncated && unlinked.length === 0}
                    No promotable plain mentions in this batch · may be
                    incomplete
                  {:else if unlinkedHasMore}
                    {unlinked.length}+ pages mention this title{unlinkedTruncated
                      ? ' · may be incomplete'
                      : ''}
                  {:else if unlinked.length === 1}
                    1 page mentions this title{unlinkedTruncated
                      ? ' · may be incomplete'
                      : ''}
                  {:else}
                    {unlinked.length} pages mention this title{unlinkedTruncated
                      ? ' · may be incomplete'
                      : ''}
                  {/if}
                </span>
              </span>
            </button>
          </h3>
          {#if unlinkedError}
            <div
              class="m-1.5 p-2.5 rounded-lg border border-status-warn/35 bg-status-warn/10"
              role="alert"
            >
              <p class="m-0 text-type-xs font-label-sm-bold text-text-primary">
                {unlinkedErrorAction === 'more'
                  ? 'More unlinked mentions could not be loaded.'
                  : unlinkedErrorAction === 'scan'
                    ? 'More mention candidates could not be scanned.'
                    : 'Unlinked mentions could not be loaded.'}
              </p>
              <p class="mt-1 mb-0 text-type-2xs text-text-muted">
                {unlinkedError}
              </p>
              <button
                type="button"
                class="mt-2 p-0 border-none bg-transparent text-accent-primary-start text-type-xs underline cursor-pointer focus-visible:ring-2 focus-visible:ring-accent-primary-start rounded"
                onclick={retryUnlinked}>Try again</button
              >
            </div>
          {/if}
          {#if unlinkedExpanded}
            <div
              id="unlinked-mentions-body"
              class="border-t border-surface-sidebar-border/70"
            >
              {#if unlinkedTruncated}
                <!-- Visual reinforcement only; subtitle is the single live region. -->
                <div
                  class="px-2.5 py-2 text-type-2xs font-body-md text-status-warn bg-status-warn/10 border-b border-status-warn/30"
                >
                  <p class="m-0">
                    {unlinked.length === 0
                      ? 'Matching text is capped for performance — no promotable plain mentions in this batch.'
                      : 'Results may be incomplete — matching text is capped for performance.'}
                  </p>
                  {#if unlinkedScanCursor}
                    {@const scanBlockedByResidual = unlinkedHasMore}
                    <button
                      type="button"
                      class="mt-2 w-full rounded border border-status-warn/40 bg-surface-panel/50 px-2.5 py-1.5 text-label-sm font-label-sm-bold text-accent-primary-start hover:bg-hover cursor-pointer transition-colors disabled:cursor-not-allowed disabled:opacity-65 focus-visible:ring-2 focus-visible:ring-accent-primary-start"
                      disabled={unlinkedLoading ||
                        unlinkedLoadingMore ||
                        unlinkedScanningMore ||
                        scanBlockedByResidual}
                      title={scanBlockedByResidual
                        ? 'Load remaining pages in this batch first'
                        : undefined}
                      aria-label={unlinkedScanningMore
                        ? 'Scanning more unlinked mention candidates'
                        : scanBlockedByResidual
                          ? 'Load remaining unlinked pages in this batch before scanning more candidates'
                          : 'Scan more unlinked mention candidates'}
                      onclick={() => scanMoreUnlinked(notebook, section, page)}
                    >
                      {unlinkedScanningMore
                        ? 'Scanning…'
                        : 'Scan more mentions'}
                    </button>
                    {#if scanBlockedByResidual}
                      <p
                        class="mt-1 mb-0 text-type-3xs text-surface-sidebar-text-muted"
                      >
                        Load remaining pages in this batch before scanning more.
                      </p>
                    {/if}
                  {/if}
                </div>
              {/if}
              <ul class="m-0 p-0 list-none">
                {#each unlinked as mention, index (`${mention.source}\u0000${mention.sourceNotebook}\u0000${mention.sourceSection}\u0000${mention.sourcePage}:${index}`)}
                  <li
                    class="border-b border-surface-sidebar-border/60 last:border-b-0"
                  >
                    <div
                      class="px-2.5 py-2 flex items-center gap-2 text-surface-sidebar-text"
                    >
                      <button
                        type="button"
                        class="min-w-0 flex-1 border-none bg-transparent text-left p-0 cursor-pointer hover:text-accent-primary-start focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-primary-start rounded"
                        onclick={() =>
                          openPage({
                            sourceNotebook: mention.sourceNotebook,
                            sourceSection: mention.sourceSection,
                            sourcePage: mention.sourcePage,
                            source: mention.source ?? 'vault'
                          } as Backlink)}
                        aria-label={`Open page ${mention.sourcePage}`}
                      >
                        <span class="flex items-center gap-1.5 min-w-0">
                          <span
                            class="truncate font-label-sm-bold text-label-sm"
                            >{mention.sourcePage}</span
                          >
                          <span
                            class="flex-shrink-0 rounded-full border border-surface-sidebar-border bg-surface-sidebar px-1.5 py-0.5 text-type-3xs uppercase tracking-wider text-surface-sidebar-text-muted font-label-sm-bold"
                            aria-label={`${mention.matchCount} mention${mention.matchCount === 1 ? '' : 's'}`}
                            >{mention.matchCount}</span
                          >
                          {#if mention.ambiguous}
                            <span
                              class="flex-shrink-0 rounded-full border border-status-warn/40 bg-status-warn/10 px-1.5 py-0.5 text-type-3xs uppercase tracking-wider text-status-warn font-label-sm-bold"
                              title={mention.candidatesTruncated &&
                              (mention.candidatesTotal ?? 0) >
                                (mention.candidates?.length ?? 0)
                                ? `${mention.candidatesTotal} matching paths (showing ${mention.candidates?.length ?? 0})`
                                : (mention.candidates
                                    ?.map(
                                      (c) =>
                                        `${c.notebook}/${c.section}/${c.page}`
                                    )
                                    .join(', ') ?? 'Ambiguous title')}
                              >Ambiguous</span
                            >
                          {/if}
                        </span>
                        <span
                          class="block truncate text-type-3xs text-surface-sidebar-text-muted"
                        >
                          {[mention.sourceNotebook, mention.sourceSection]
                            .filter(Boolean)
                            .join(' / ')}
                        </span>
                      </button>
                    </div>
                    <ul class="m-0 p-0 pb-1 list-none">
                      {#each mention.sourceBlockIds as blockId, bIndex (`${blockId}:${bIndex}`)}
                        {@const snip = mention.sourceSnippets?.[bIndex] ?? ''}
                        <li
                          class="px-2.5 py-0.5 flex flex-col gap-1 {mention.ambiguous
                            ? 'items-stretch'
                            : 'sm:flex-row sm:items-center sm:justify-between'}"
                        >
                          <span
                            class="min-w-0 line-clamp-2 text-type-3xs text-surface-sidebar-text-muted leading-snug"
                            title={snip || undefined}
                          >
                            {#each emphasizeTitle(snip, mention.title) as part, pIdx (`${bIndex}-${pIdx}`)}
                              {#if part.mark}
                                <mark
                                  class="bg-editor-highlight/50 text-inherit rounded-sm px-0.5"
                                  >{part.text}</mark
                                >
                              {:else}
                                {part.text}
                              {/if}
                            {:else}
                              <span
                                class="italic text-surface-sidebar-text-muted"
                                >No preview</span
                              >
                            {/each}
                          </span>
                          {#if mention.ambiguous}
                            <div
                              class="flex flex-wrap gap-1 pb-0.5"
                              role="group"
                              aria-label={`Link targets for mention of ${mention.title}`}
                            >
                              {#each mention.candidates ?? [] as cand, cIdx (`${cand.notebook}/${cand.section}/${cand.page}:${cIdx}`)}
                                <button
                                  type="button"
                                  class="max-w-full truncate rounded-full border border-surface-sidebar-border bg-surface-sidebar px-2 py-0.5 text-type-3xs text-accent-primary-start hover:bg-accent-primary-start/10 cursor-pointer transition-colors disabled:cursor-wait disabled:opacity-65 focus-visible:ring-2 focus-visible:ring-accent-primary-start"
                                  disabled={unlinkedBusy.has(blockId)}
                                  title={candidatePathLabel(cand)}
                                  aria-label={`Link mention of ${mention.title} as ${candidatePathLabel(cand)} in block ${blockId.slice(0, 8)} on page ${mention.sourcePage}`}
                                  onclick={() =>
                                    promoteMention(mention, blockId, {
                                      notebook: cand.notebook,
                                      section: cand.section,
                                      page: cand.page
                                    })}
                                >
                                  {unlinkedBusy.has(blockId)
                                    ? 'Linking…'
                                    : candidatePathLabel(cand)}
                                </button>
                              {/each}
                              {#if mention.candidatesTruncated && (mention.candidatesTotal ?? 0) > (mention.candidates?.length ?? 0)}
                                {@const moreCount =
                                  (mention.candidatesTotal ?? 0) -
                                  (mention.candidates?.length ?? 0)}
                                <span
                                  class="max-w-full truncate rounded-full border border-status-warn/30 bg-status-warn/5 px-2 py-0.5 text-type-3xs text-status-warn"
                                  title={`${mention.candidatesTotal} paths share this title; showing ${mention.candidates?.length ?? 0}. Open the target page or rename to disambiguate.`}
                                  aria-label={`${moreCount} more matching paths not shown`}
                                  >+{moreCount} more</span
                                >
                              {/if}
                            </div>
                          {:else}
                            <button
                              type="button"
                              class="flex-shrink-0 self-end sm:self-auto rounded border border-surface-sidebar-border bg-transparent px-2 py-0.5 text-type-3xs text-accent-primary-start hover:bg-accent-primary-start/10 cursor-pointer transition-colors disabled:cursor-wait disabled:opacity-65 focus-visible:ring-2 focus-visible:ring-accent-primary-start"
                              disabled={unlinkedBusy.has(blockId)}
                              aria-label={`Link mention of ${mention.title} in block ${blockId.slice(0, 8)} on page ${mention.sourcePage}`}
                              onclick={() => promoteMention(mention, blockId)}
                            >
                              {unlinkedBusy.has(blockId) ? 'Linking…' : 'Link'}
                            </button>
                          {/if}
                        </li>
                      {/each}
                    </ul>
                  </li>
                {/each}
              </ul>
              {#if unlinkedHasMore}
                <button
                  type="button"
                  class="w-full border-t border-surface-sidebar-border/70 bg-transparent px-2.5 py-2 text-label-sm font-label-sm-bold text-accent-primary-start hover:bg-hover cursor-pointer transition-colors disabled:cursor-wait disabled:opacity-65 focus-visible:ring-2 focus-visible:ring-accent-primary-start"
                  disabled={unlinkedLoading ||
                    unlinkedLoadingMore ||
                    unlinkedScanningMore}
                  aria-label={unlinkedLoadingMore
                    ? 'Loading more unlinked mentions'
                    : 'Load more unlinked mentions'}
                  onclick={() => loadMoreUnlinked(notebook, section, page)}
                >
                  {unlinkedLoadingMore ? 'Loading more…' : 'Load more'}
                </button>
              {/if}
            </div>
          {/if}
        </section>
      {/if}
    {/if}
  </div>
</nav>
