<script lang="ts">
  import { onMount } from 'svelte'
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
   * via ctx.deleteBlock. The host re-queries comments_count through
   * onCommentsChanged after each mutation.
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
      pending: false
    }
  }

  async function load() {
    loading = true
    errorMsg = ''
    try {
      const subtree = await ctx.fetchSubtree(taskId)
      const mapped = (subtree ?? []).map(toComment)
      // Sort by timestamp ascending; undated (empty ts) fall to the end,
      // keeping their original file order via the line-number tiebreaker so
      // legacy comments don't reshuffle when a new dated comment arrives.
      mapped.sort((a, b) => {
        const at = a.timestamp || '9999'
        const bt = b.timestamp || '9999'
        if (at !== bt) return at < bt ? -1 : 1
        return a.line - b.line
      })
      comments = mapped
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
      if (composerPending) return
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
      line: comments.length,
      pending: true
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

  function onComposerKey(e: KeyboardEvent) {
    // Enter posts; Shift+Enter inserts a newline (default). Preventing the
    // newline on Enter keeps the composer single-line-by-default, matching
    // the issue's "type + Post (or Enter)" contract.
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void submit()
    }
  }

  async function onDelete(comment: Comment) {
    if (!window.confirm('Delete this comment?')) return
    // Capture the index before filtering so a failed delete restores the
    // comment at its original position — appending at the end would reshuffle
    // the thread order on every transient failure.
    const idx = comments.findIndex((c) => c.id === comment.id)
    comments = comments.filter((c) => c.id !== comment.id)
    try {
      await ctx.deleteBlock(comment.id)
      onCommentsChanged?.()
      liveMessage = 'Comment deleted'
    } catch (e) {
      // Restore on failure so the user sees the delete didn't take.
      comments = [...comments.slice(0, idx), comment, ...comments.slice(idx)]
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

<section
  aria-labelledby="comment-thread-heading"
  class="pt-3 border-t border-surface-card-border"
>
  <div class="flex items-center gap-2 mb-2">
    <h3
      id="comment-thread-heading"
      class="text-[12px] font-label-sm-bold text-text-primary"
    >
      Comments
    </h3>
    <span
      class="text-[10px] font-label-sm text-text-muted bg-surface-card border border-surface-card-border rounded-full px-1.5 py-0.5"
      aria-label="{comments.length} comments"
    >
      {comments.length}
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
    <p class="text-[11px] text-text-muted" data-testid="comment-loading">
      Loading comments…
    </p>
  {:else if comments.length === 0}
    <p class="text-[11px] text-text-muted" data-testid="comment-empty-state">
      No comments yet. Start the conversation.
    </p>
  {:else}
    <ul class="flex flex-col gap-2">
      {#each comments as c (c.key)}
        <li>
          <!-- svelte-ignore a11y_unknown_role -->
          <!-- role="comment" is a valid WAI-ARIA 1.3 role; svelte-check's
               allowlist predates it. Semantic intent: each item is a
               standalone comment in the thread. -->
          <article
            role="comment"
            class="rounded border border-surface-card-border bg-surface-card p-2"
            transition:fly={{ duration: 120, y: -4 }}
          >
            <header class="flex items-center justify-between gap-2 mb-1">
              <p class="text-[10px] font-label-sm text-text-muted">
                <span class="text-text-primary">{c.author || 'Unknown'}</span>
                <span aria-hidden="true"> · </span>
                <time datetime={c.timestamp || undefined}
                  >{formatTimestamp(c.timestamp)}</time
                >
                {#if c.pending}<span aria-hidden="true"> · saving…</span>{/if}
              </p>
              <button
                type="button"
                class="material-symbols-outlined text-[14px] text-text-muted hover:text-error transition-colors"
                aria-label="Delete comment"
                title="Delete comment"
                onclick={() => void onDelete(c)}
              >
                delete
              </button>
            </header>
            <div class="text-[12px] text-text-primary leading-snug">
              {#each splitBold(c.body) as run}
                {#if run.bold}
                  <strong>
                    <RichText
                      text={run.text}
                      {notebook}
                      {section}
                      {page}
                      {fileDate}
                    />
                  </strong>
                {:else}
                  <RichText
                    text={run.text}
                    {notebook}
                    {section}
                    {page}
                    {fileDate}
                  />
                {/if}
              {/each}
            </div>
          </article>
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
      class="w-full resize-y min-h-[2.5rem] rounded border border-surface-card-border bg-surface-card px-2 py-1 text-[12px] text-text-primary focus:outline-none focus:border-accent-primary-start"
      rows="2"></textarea>
    <p id="comment-composer-help-{taskId}" class="sr-only">
      Press Enter to post, Shift+Enter for a new line.
    </p>
    <div class="flex items-center gap-2">
      <label
        for="comment-author-{taskId}"
        class="text-[10px] font-label-sm text-text-muted"
      >
        Author
      </label>
      <input
        id="comment-author-{taskId}"
        bind:value={composerAuthor}
        onblur={onAuthorBlur}
        aria-label="Default comment author"
        class="flex-1 rounded border border-surface-card-border bg-surface-card px-2 py-0.5 text-[11px] text-text-primary focus:outline-none focus:border-accent-primary-start"
      />
      <button
        type="button"
        onclick={() => void submit()}
        disabled={!canSubmit}
        class="px-2.5 py-1 rounded bg-accent-primary-start text-text-on-accent text-[11px] font-label-sm-bold disabled:opacity-50 disabled:cursor-not-allowed hover:brightness-110 transition-all"
      >
        Post
      </button>
    </div>
  </div>
</section>
