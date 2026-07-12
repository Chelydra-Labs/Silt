<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { fly } from 'svelte/transition'
  import type { PluginContext, SubtreeBlock } from '../../../sdk'
  import RichText from '../../../../components/RichText.svelte'
  import { loadLocalAuthor, persistLocalAuthor } from '../settings'
  import ErrorBanner from './ErrorBanner.svelte'
  import { friendlyCaughtError } from '../errors'

  /**
   * Comment thread for TaskEditDrawer (#430). Each comment is a child NOTE
   * block beneath the task carrying [author:: NAME] and [ts:: ISO] tokens;
   * FetchSubtree re-hydrates author/timestamp on each load via the block_meta
   * projection from #418/#37. Reads via ctx.fetchSubtree, writes via
   * ctx.addTaskComment (composes a NOTE + splices into the sub-tree), deletes
   * via ctx.deleteBlock. Nested replies (#438) use parentCommentId; the UI
   * shows two levels (top-level + first-level replies), flattening deeper
   * nesting under the top-level ancestor. The host re-queries comments_count
   * through onCommentsChanged after each mutation.
   */
  interface Props {
    taskId: string
    notebook: string
    section: string
    page: string
    fileDate: string
    ctx: PluginContext
    onCommentsChanged?: () => void
  }

  let {
    taskId,
    notebook,
    section,
    page,
    fileDate,
    ctx,
    onCommentsChanged
  }: Props = $props()

  interface Comment {
    id: string
    /** Stable render key, distinct from the block id so the optimistic→real
     *  id swap on submit success doesn't change the {#each} key (which would
     *  trigger a fly-out transition and leave a duplicate node in the DOM). */
    key: string
    body: string
    author: string
    timestamp: string
    /** File line order, used as a stable tiebreaker for undated comments. */
    line: number
    /** True until the server-confirmed UUID replaces the optimistic id. */
    pending: boolean
    /** Block parent_id — taskId for top-level, parent comment id for replies. */
    parentId: string
    /** Nested replies flattened to one level under a top-level comment. */
    replies: Comment[]
  }

  let comments = $state<Comment[]>([])
  let loading = $state(true)
  let errorMsg = $state('')
  // errorRetryable is true only when the last error was a failed comment POST
  // whose draft text was restored to the composer — the one surface where a
  // "Try again" CTA cleanly re-invokes the action (#459). Load/delete failures
  // have no clean retry path from the banner.
  let errorRetryable = $state(false)
  let composerText = $state('')
  let composerAuthor = $state('')
  let composerPending = $state(false)
  let liveMessage = $state('')
  // Inline reply composer: which comment is being replied to, plus its draft.
  let replyToId = $state<string | null>(null)
  let replyText = $state('')
  let replyPending = $state(false)
  let replyTextareaEl = $state<HTMLTextAreaElement | null>(null)

  // YYYY-MM-DD HH:MM (local) for display; "Undated" fallback for legacy
  // NOTEs without a [ts::] token. Mirrors the TaskEditDrawer inline formatter
  // — duplicated because the drawer's is module-private and this component
  // lives in a different folder (Phase 10 collapses them).
  const dtf = new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
  function formatTimestamp(iso: string): string {
    if (!iso) return 'Undated'
    const d = new Date(iso)
    return isNaN(d.getTime()) ? 'Undated' : dtf.format(d)
  }

  let renderKeySeq = 0
  function nextKey(): string {
    renderKeySeq += 1
    return `c-${renderKeySeq}`
  }

  function toComment(b: SubtreeBlock): Comment {
    return {
      id: b.id,
      key: b.id || nextKey(),
      body: b.clean_text ?? '',
      author: b.author ?? '',
      timestamp: b.timestamp ?? '',
      line: b.line_number ?? 0,
      pending: false,
      parentId: b.parent_id ?? '',
      replies: []
    }
  }

  function isNoteBlock(b: SubtreeBlock): boolean {
    return (b.type ?? '').toUpperCase() === 'NOTE'
  }

  function sortComments(list: Comment[]): Comment[] {
    // Sort by timestamp ascending; undated (empty ts) fall to the end,
    // keeping their original file order via the line-number tiebreaker so
    // legacy comments don't reshuffle when a new dated comment arrives.
    return [...list].sort((a, b) => {
      const at = a.timestamp || '9999'
      const bt = b.timestamp || '9999'
      if (at !== bt) return at < bt ? -1 : 1
      return a.line - b.line
    })
  }

  /**
   * Build a two-level thread from flat NOTE blocks (#438). Top-level =
   * parent_id === taskId (or missing parent). Replies attach under their
   * top-level ancestor; deeper nesting is flattened into that first reply
   * list so the UI stays two levels deep.
   */
  function buildThread(blocks: SubtreeBlock[], taskId: string): Comment[] {
    const notes = (blocks ?? []).filter(isNoteBlock)
    const byId = new Map(notes.map((b) => [b.id, b]))

    function isTopLevel(b: SubtreeBlock): boolean {
      return !b.parent_id || b.parent_id === taskId
    }

    /** Walk parent_id until a top-level NOTE; null if none found. */
    function topLevelAncestorId(b: SubtreeBlock): string | null {
      let pid = b.parent_id
      let guard = 0
      while (pid && pid !== taskId && guard++ < 64) {
        const parent = byId.get(pid)
        if (!parent) return null
        if (isTopLevel(parent)) return parent.id
        pid = parent.parent_id
      }
      return null
    }

    const tops: Comment[] = []
    const repliesByTop = new Map<string, Comment[]>()

    for (const b of notes) {
      const c = toComment(b)
      if (isTopLevel(b)) {
        tops.push(c)
        continue
      }
      const topId = topLevelAncestorId(b)
      if (!topId) {
        // Orphan / non-NOTE parent chain — treat as top-level so it still
        // renders rather than vanishing from the thread.
        tops.push(c)
        continue
      }
      const list = repliesByTop.get(topId) ?? []
      list.push(c)
      repliesByTop.set(topId, list)
    }

    const sortedTops = sortComments(tops)
    for (const top of sortedTops) {
      top.replies = sortComments(repliesByTop.get(top.id) ?? [])
    }
    return sortedTops
  }

  /** Flat count for the badge (top-level + all replies). */
  function countComments(list: Comment[]): number {
    return list.reduce((n, c) => n + 1 + c.replies.length, 0)
  }

  async function load() {
    loading = true
    errorMsg = ''
    try {
      const subtree = await ctx.fetchSubtree(taskId)
      comments = buildThread(subtree ?? [], taskId)
    } catch (e) {
      errorMsg = friendlyCaughtError(e)
      errorRetryable = false
    } finally {
      loading = false
    }
  }

  async function loadAuthor() {
    const saved = loadLocalAuthor()
    if (saved !== undefined) {
      // Respect both a saved name AND an explicit clear (''); only re-seed
      // from the OS username when the key is absent (never set).
      composerAuthor = saved
      return
    }
    // Seed from the OS user once, then persist so later opens skip the IPC
    // and the user keeps a stable identity across task switches.
    try {
      const who = await ctx.getLocalAuthor()
      composerAuthor = who || ''
      if (who) await persistLocalAuthor(who)
    } catch {
      // Non-fatal: composerAuthor stays '' and the comment posts with no
      // author token (renderer shows "Unknown").
    }
  }

  onMount(() => {
    // loadAuthor + the block:changed subscription run once for the
    // component's lifetime; load() is driven by the taskId $effect below so
    // switching tasks re-fetches without re-subscribing.
    void loadAuthor()
    const off = ctx.on('block:changed', () => {
      // External edits (sub-editor, another surface) refresh the thread so
      // a comment added elsewhere appears without a manual reload. Skip
      // while a comment post is in flight: the optimistic entry is already
      // in `comments`, and a wholesale replace now would flicker out the
      // pending entry until the post completes and re-triggers block:changed.
      if (composerPending || replyPending) return
      void load()
    })
    return () => {
      off()
    }
  })

  // Reload when the user switches tasks (taskId prop changes) and on first
  // mount. Tracked explicitly because onMount runs once for the component's
  // lifetime, not per prop change.
  $effect(() => {
    void taskId
    void load()
  })

  let canSubmit = $derived(composerText.trim().length > 0 && !composerPending)
  let canSubmitReply = $derived(replyText.trim().length > 0 && !replyPending)
  let totalCount = $derived(countComments(comments))

  async function submit() {
    const text = composerText.trim()
    if (!text || composerPending) return
    const author = composerAuthor.trim()
    const now = new Date().toISOString().slice(0, 19)
    const optimisticKey = nextKey()
    // Optimistic push so the user sees the comment immediately; the
    // pending flag is cleared (and id corrected) once the server replies.
    const optimistic: Comment = {
      id: `optimistic-${Date.now()}`,
      key: optimisticKey,
      body: text,
      author,
      timestamp: now,
      line: totalCount,
      pending: true,
      parentId: taskId,
      replies: []
    }
    comments = [...comments, optimistic]
    composerText = ''
    composerPending = true
    errorMsg = ''
    errorRetryable = false
    try {
      const realId = await ctx.addTaskComment(taskId, text, author || undefined)
      comments = comments.map((c) =>
        c.key === optimisticKey ? { ...c, id: realId, pending: false } : c
      )
      onCommentsChanged?.()
      liveMessage = 'Comment added'
    } catch (e) {
      // Revert the optimistic entry so the thread doesn't show a comment
      // that never reached disk, AND restore the draft text so the user can
      // edit + retry (the ErrorBanner surfaces a "Try again" CTA, #459).
      comments = comments.filter((c) => c.key !== optimisticKey)
      composerText = text
      errorMsg = friendlyCaughtError(e)
      errorRetryable = true
      liveMessage = 'Comment failed to post'
    } finally {
      composerPending = false
    }
  }

  async function startReply(comment: Comment) {
    replyToId = comment.id
    replyText = ''
    await tick()
    replyTextareaEl?.focus()
  }

  function cancelReply() {
    replyToId = null
    replyText = ''
  }

  async function submitReply() {
    const text = replyText.trim()
    const parentId = replyToId
    if (!text || !parentId || replyPending) return
    const author = composerAuthor.trim()
    const now = new Date().toISOString().slice(0, 19)
    const optimisticKey = nextKey()
    const optimistic: Comment = {
      id: `optimistic-${Date.now()}`,
      key: optimisticKey,
      body: text,
      author,
      timestamp: now,
      line: totalCount,
      pending: true,
      parentId,
      replies: []
    }

    // Attach under the top-level ancestor that owns parentId (parent may
    // itself be a reply — flatten into that top-level's reply list).
    comments = comments.map((top) => {
      if (top.id === parentId) {
        return { ...top, replies: [...top.replies, optimistic] }
      }
      if (top.replies.some((r) => r.id === parentId)) {
        return { ...top, replies: [...top.replies, optimistic] }
      }
      return top
    })
    replyText = ''
    replyPending = true
    errorMsg = ''
    errorRetryable = false
    try {
      const realId = await ctx.addTaskComment(
        taskId,
        text,
        author || undefined,
        parentId
      )
      comments = comments.map((top) => ({
        ...top,
        replies: top.replies.map((r) =>
          r.key === optimisticKey ? { ...r, id: realId, pending: false } : r
        )
      }))
      replyToId = null
      onCommentsChanged?.()
      liveMessage = 'Reply added'
    } catch (e) {
      comments = comments.map((top) => ({
        ...top,
        replies: top.replies.filter((r) => r.key !== optimisticKey)
      }))
      replyText = text
      errorMsg = friendlyCaughtError(e)
      errorRetryable = false
      liveMessage = 'Reply failed to post'
    } finally {
      replyPending = false
    }
  }

  function onComposerKey(e: KeyboardEvent) {
    // Enter posts; Shift+Enter inserts a newline (default). Preventing the
    // newline on Enter keeps the composer single-line-by-default, matching
    // the issue's "type + Post (or Enter)" contract.
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void submit()
    }
  }

  function onReplyKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void submitReply()
    } else if (e.key === 'Escape') {
      // Stop bubble so TaskEditDrawer's window Escape handler doesn't close
      // the drawer while the user is only dismissing the reply composer.
      e.preventDefault()
      e.stopPropagation()
      cancelReply()
    }
  }

  async function onDelete(comment: Comment) {
    if (!window.confirm('Delete this comment?')) return
    // Snapshot the full tree so a failed delete restores exact structure
    // (top-level index + reply nesting), not a reshuffled flat append.
    const snapshot = comments
    comments = comments
      .filter((c) => c.id !== comment.id)
      .map((top) => ({
        ...top,
        replies: top.replies.filter((r) => r.id !== comment.id)
      }))
    try {
      await ctx.deleteBlock(comment.id)
      onCommentsChanged?.()
      liveMessage = 'Comment deleted'
    } catch (e) {
      comments = snapshot
      errorMsg = friendlyCaughtError(e)
      errorRetryable = false
      liveMessage = 'Comment failed to delete'
    }
  }

  function onAuthorBlur() {
    // Persist on blur so the pref sticks across opens; empty is valid (clears
    // the override and the OS-user fallback re-applies on next load).
    void persistLocalAuthor(composerAuthor.trim())
  }

  // Split a comment body into bold / non-bold runs so **bold** renders as
  // <strong> while still letting RichText handle ((uuid)) refs, {{embed:}}
  // and #tags inside each run. RichText only scans for refs/embeds/tags, so
  // bold must be layered above it (scoped here — RichText itself is unchanged).
  interface BoldRun {
    bold: boolean
    text: string
  }
  function splitBold(text: string): BoldRun[] {
    const runs: BoldRun[] = []
    const re = /\*\*([^*]+?)\*\*/g
    let last = 0
    let m: RegExpExecArray | null
    while ((m = re.exec(text)) !== null) {
      if (m.index > last) {
        runs.push({ bold: false, text: text.slice(last, m.index) })
      }
      runs.push({ bold: true, text: m[1] })
      last = m.index + m[0].length
    }
    if (last < text.length) {
      runs.push({ bold: false, text: text.slice(last) })
    }
    return runs.length ? runs : [{ bold: false, text }]
  }
</script>

{#snippet commentBody(c: Comment)}
  <div class="text-type-sm text-text-primary leading-snug">
    {#each splitBold(c.body) as run}
      {#if run.bold}
        <strong>
          <RichText text={run.text} {notebook} {section} {page} {fileDate} />
        </strong>
      {:else}
        <RichText text={run.text} {notebook} {section} {page} {fileDate} />
      {/if}
    {/each}
  </div>
{/snippet}

{#snippet commentArticle(c: Comment, nested: boolean)}
  <!-- svelte-ignore a11y_unknown_role -->
  <!-- role="comment" is a valid WAI-ARIA 1.3 role; svelte-check's
       allowlist predates it. Semantic intent: each item is a
       standalone comment in the thread. -->
  <article
    role="comment"
    class="rounded border border-surface-card-border bg-surface-card p-2"
    class:ml-4={nested}
    class:border-l-2={nested}
    class:border-l-accent-primary-start={nested}
    transition:fly={{ duration: 120, y: -4 }}
  >
    <header class="flex items-center justify-between gap-2 mb-1">
      <p class="text-type-2xs font-label-sm text-text-muted">
        <span class="text-text-primary">{c.author || 'Unknown'}</span>
        <span aria-hidden="true"> · </span>
        <time datetime={c.timestamp || undefined}
          >{formatTimestamp(c.timestamp)}</time
        >
        {#if c.pending}<span aria-hidden="true"> · saving…</span>{/if}
      </p>
      <div class="flex items-center gap-1">
        <button
          type="button"
          class="text-type-2xs font-label-sm text-text-muted hover:text-accent-primary-start transition-colors px-1"
          aria-label="Reply to comment"
          title="Reply"
          onclick={() => startReply(c)}
        >
          Reply
        </button>
        <button
          type="button"
          class="material-symbols-outlined text-icon-sm text-text-muted hover:text-error transition-colors"
          aria-label="Delete comment"
          title="Delete comment"
          onclick={() => void onDelete(c)}
        >
          delete
        </button>
      </div>
    </header>
    {@render commentBody(c)}
    {#if replyToId === c.id}
      <div class="mt-2 flex flex-col gap-1.5" data-testid="reply-composer">
        <label for="reply-composer-{c.id}" class="sr-only">Reply text</label>
        <textarea
          id="reply-composer-{c.id}"
          bind:this={replyTextareaEl}
          bind:value={replyText}
          onkeydown={onReplyKey}
          placeholder="Write a reply…"
          aria-label="Reply text"
          class="w-full resize-y min-h-[2rem] rounded border border-surface-card-border bg-surface-card px-2 py-1 text-type-sm text-text-primary focus:outline-none focus:border-accent-primary-start"
          rows="2"></textarea>
        <div class="flex items-center justify-end gap-2">
          <button
            type="button"
            class="px-2 py-0.5 rounded text-type-xs font-label-sm text-text-muted hover:text-text-primary transition-colors"
            onclick={cancelReply}
          >
            Cancel
          </button>
          <button
            type="button"
            onclick={() => void submitReply()}
            disabled={!canSubmitReply}
            class="px-2.5 py-1 rounded bg-accent-primary-start text-text-on-accent text-type-xs font-label-sm-bold disabled:opacity-50 disabled:cursor-not-allowed hover:brightness-110 transition-all"
          >
            Reply
          </button>
        </div>
      </div>
    {/if}
  </article>
{/snippet}

<section
  aria-labelledby="comment-thread-heading"
  class="pt-3 border-t border-surface-card-border"
>
  <div class="flex items-center gap-2 mb-2">
    <h3
      id="comment-thread-heading"
      class="text-type-sm font-label-sm-bold text-text-primary"
    >
      Comments
    </h3>
    <span
      class="text-type-2xs font-label-sm text-text-muted bg-surface-card border border-surface-card-border rounded-full px-1.5 py-0.5"
      aria-label="{totalCount} comments"
    >
      {totalCount}
    </span>
  </div>

  <div class="sr-only" aria-live="polite">{liveMessage}</div>

  {#if errorMsg}
    <ErrorBanner
      message={errorMsg}
      compact
      dataTestId="comment-thread-error"
      onRetry={errorRetryable ? () => void submit() : undefined}
    />
  {/if}

  {#if loading}
    <p class="text-type-xs text-text-muted" data-testid="comment-loading">
      Loading comments…
    </p>
  {:else if comments.length === 0}
    <p class="text-type-xs text-text-muted" data-testid="comment-empty-state">
      No comments yet. Start the conversation.
    </p>
  {:else}
    <ul class="flex flex-col gap-2">
      {#each comments as c (c.key)}
        <li class="flex flex-col gap-2">
          {@render commentArticle(c, false)}
          {#if c.replies.length > 0}
            <ul class="flex flex-col gap-2">
              {#each c.replies as r (r.key)}
                <li>
                  {@render commentArticle(r, true)}
                </li>
              {/each}
            </ul>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  <div class="mt-2 flex flex-col gap-1.5">
    <label for="comment-composer-{taskId}" class="sr-only">Comment text</label>
    <textarea
      id="comment-composer-{taskId}"
      bind:value={composerText}
      onkeydown={onComposerKey}
      placeholder="Add a comment…"
      aria-label="Comment text"
      aria-describedby="comment-composer-help-{taskId}"
      class="w-full resize-y min-h-[2.5rem] rounded border border-surface-card-border bg-surface-card px-2 py-1 text-type-sm text-text-primary focus:outline-none focus:border-accent-primary-start"
      rows="2"></textarea>
    <p id="comment-composer-help-{taskId}" class="sr-only">
      Press Enter to post, Shift+Enter for a new line.
    </p>
    <div class="flex items-center gap-2">
      <label
        for="comment-author-{taskId}"
        class="text-type-2xs font-label-sm text-text-muted"
      >
        Author
      </label>
      <input
        id="comment-author-{taskId}"
        bind:value={composerAuthor}
        onblur={onAuthorBlur}
        aria-label="Default comment author"
        class="flex-1 rounded border border-surface-card-border bg-surface-card px-2 py-0.5 text-type-xs text-text-primary focus:outline-none focus:border-accent-primary-start"
      />
      <button
        type="button"
        onclick={() => void submit()}
        disabled={!canSubmit}
        class="px-2.5 py-1 rounded bg-accent-primary-start text-text-on-accent text-type-xs font-label-sm-bold disabled:opacity-50 disabled:cursor-not-allowed hover:brightness-110 transition-all"
      >
        Post
      </button>
    </div>
  </div>
</section>
