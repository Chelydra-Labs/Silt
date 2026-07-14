<script lang="ts">
  // Inline [[target]] wiki-link chip (#545). Resolves via ResolvePageLink and
  // dispatches navigate-to-page on click/Enter. Mirrors BlockReferenceChip.
  // Ambiguous and unresolved chips offer create-or-pick (#549).
  import { fade } from 'svelte/transition'
  import { onDestroy } from 'svelte'
  import { ResolvePageLink, CreatePage } from '../../bindings/silt/app.js'
  import { getActiveLocation } from '../plugins/location.svelte'

  interface Props {
    target: string
    heading?: string | null
    alias?: string | null
  }

  let { target, heading = null, alias = null }: Props = $props()

  let ref = $state<{
    exists?: boolean
    ambiguous?: boolean
    notebook?: string
    section?: string
    page?: string
    shortest?: string
    candidates?: { notebook: string; section: string; page: string }[]
  } | null>(null)
  let loading = $state(true)
  let showHover = $state(false)
  let creating = $state(false)
  let createError = $state('')
  let hoverTimer: ReturnType<typeof setTimeout> | null = null
  let createErrorTimer: ReturnType<typeof setTimeout> | null = null

  const label = $derived(alias || target)

  function pathLabel(c: {
    notebook: string
    section: string
    page: string
  }): string {
    return [c.notebook, c.section, c.page].filter(Boolean).join(' › ')
  }

  let lastResolvedTarget = ''

  async function load() {
    loading = true
    try {
      ref = await ResolvePageLink(target)
    } catch {
      ref = { exists: false }
    } finally {
      loading = false
    }
  }

  // Re-resolve when target changes (future-proofs reuse in read-mode surfaces
  // where the prop can change without a remount). In the editor NodeView path,
  // PM creates a fresh node instance on attr change so this is a no-op there.
  $effect(() => {
    if (target && target !== lastResolvedTarget) {
      lastResolvedTarget = target
      void load()
    }
  })

  onDestroy(() => {
    if (hoverTimer) clearTimeout(hoverTimer)
    if (createErrorTimer) clearTimeout(createErrorTimer)
  })

  function enter() {
    if (hoverTimer) clearTimeout(hoverTimer)
    hoverTimer = setTimeout(() => (showHover = true), 250)
  }

  function leave() {
    if (hoverTimer) clearTimeout(hoverTimer)
    hoverTimer = setTimeout(() => (showHover = false), 150)
  }

  function navigateTo(notebook: string, section: string, page: string) {
    window.dispatchEvent(
      new CustomEvent('navigate-to-page', {
        detail: {
          notebook,
          section: section ?? '',
          page,
          heading: heading || undefined
        }
      })
    )
  }

  function click() {
    if (ref && ref.exists) {
      navigateTo(ref.notebook!, ref.section ?? '', ref.page!)
    }
  }

  function openCandidate(c: {
    notebook: string
    section: string
    page: string
  }) {
    showHover = false
    navigateTo(c.notebook, c.section ?? '', c.page)
  }

  // Parse a wiki-link target into {notebook, section, page} for CreatePage.
  // Defaults notebook/section to the active location. For path targets like
  // "Section/Page" or "Notebook/Section/Page", splits the path accordingly.
  // Sections may be nested (e.g. "Projects/Active"), so for 3+ segments the
  // first is the notebook, the last is the page, and everything in between is
  // the (multi-segment) section — matching how ResolvePageLink interprets the
  // same target, so the created page resolves back to the original link.
  function parseTargetForCreate(rawTarget: string): {
    notebook: string
    section: string
    page: string
  } {
    const loc = getActiveLocation()
    const activeNotebook = loc.notebook || ''
    const activeSection = loc.section || ''
    const parts = rawTarget.replace(/\\/g, '/').split('/').filter(Boolean)
    if (parts.length >= 3) {
      return {
        notebook: parts[0],
        section: parts.slice(1, -1).join('/'),
        page: parts[parts.length - 1]
      }
    }
    if (parts.length === 2) {
      return { notebook: activeNotebook, section: parts[0], page: parts[1] }
    }
    return {
      notebook: activeNotebook,
      section: activeSection,
      page: parts[0] || rawTarget
    }
  }

  async function createPage() {
    if (creating) return
    creating = true
    createError = ''
    if (createErrorTimer) clearTimeout(createErrorTimer)
    try {
      const { notebook, section, page } = parseTargetForCreate(target)
      if (!notebook) {
        createError = 'Open a notebook first.'
        return
      }
      if (!page) {
        createError = 'No page name in link target.'
        return
      }
      await CreatePage(notebook, section, page, '')
      showHover = false
      navigateTo(notebook, section, page)
      // Re-resolve: the page now exists so the chip becomes a normal link.
      await load()
    } catch (e) {
      createError = e instanceof Error ? e.message : String(e)
      createErrorTimer = setTimeout(() => (createError = ''), 6000)
    } finally {
      creating = false
    }
  }
</script>

{#if loading}
  <span class="text-text-muted italic text-[0.85em] mx-0.5">[[…]]</span>
{:else if ref?.ambiguous}
  <div class="inline-block relative">
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <span
      role="button"
      tabindex="0"
      aria-haspopup="dialog"
      aria-expanded={showHover}
      onmouseenter={enter}
      onmouseleave={leave}
      onkeydown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          showHover = !showHover
        }
      }}
      class="inline-flex items-center align-baseline text-status-warning mx-0.5 text-[0.85em] cursor-help underline decoration-dotted underline-offset-4"
      title="Ambiguous page link — multiple matches. Hover to pick one or create."
    >
      [[{label}]]
    </span>
    {#if showHover && ref.candidates?.length}
      <div
        transition:fade={{ duration: 120 }}
        class="absolute z-50 top-full left-0 mt-1 w-80 max-w-[80vw] glass-palette border border-surface-popover-border rounded-lg shadow-2xl p-3 text-left"
        style="backdrop-filter: blur(16px) saturate(140%); background: color-mix(in srgb, var(--color-surface-popover) 94%, transparent);"
        onmouseenter={enter}
        onmouseleave={leave}
        role="group"
        aria-label="Ambiguous page link matches"
      >
        <div
          class="text-type-2xs text-text-muted uppercase tracking-widest font-label-sm-bold mb-2"
        >
          Multiple matches
        </div>
        <ul class="flex flex-col gap-1 m-0 p-0 list-none">
          {#each ref.candidates as c (pathLabel(c))}
            <li>
              <button
                type="button"
                class="w-full text-left px-2 py-1.5 rounded-md text-sm text-text-primary hover:bg-hover cursor-pointer border-0 bg-transparent"
                onclick={() => openCandidate(c)}
              >
                {pathLabel(c)}
              </button>
            </li>
          {/each}
        </ul>
        <div class="mt-2 pt-2 border-t border-surface-popover-border">
          {#if createError}
            <p class="text-type-2xs text-status-danger mb-1.5" role="alert">
              {createError}
            </p>
          {/if}
          <button
            type="button"
            class="w-full text-left px-2 py-1.5 rounded-md text-sm text-accent-primary-start hover:bg-hover cursor-pointer border-0 bg-transparent inline-flex items-center gap-1.5"
            onclick={() => void createPage()}
            disabled={creating}
            aria-label="Create page '{target}'"
          >
            <span class="material-symbols-outlined text-[1.1em]"
              >add_circle</span
            >
            <span aria-live="polite"
              >{creating ? 'Creating…' : 'Create page'}</span
            >
          </button>
        </div>
      </div>
    {/if}
  </div>
{:else if !ref?.exists}
  <div class="inline-block relative">
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <span
      role="button"
      tabindex="0"
      aria-haspopup="dialog"
      aria-expanded={showHover}
      onmouseenter={enter}
      onmouseleave={leave}
      onkeydown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          showHover = !showHover
        }
      }}
      class="inline-flex items-center align-baseline text-text-muted line-through mx-0.5 text-[0.85em] cursor-help underline decoration-dotted underline-offset-4"
      title="Unresolved page link — hover to create the page."
    >
      [[{label}]]
    </span>
    {#if showHover}
      <div
        transition:fade={{ duration: 120 }}
        class="absolute z-50 top-full left-0 mt-1 w-80 max-w-[80vw] glass-palette border border-surface-popover-border rounded-lg shadow-2xl p-3 text-left"
        style="backdrop-filter: blur(16px) saturate(140%); background: color-mix(in srgb, var(--color-surface-popover) 94%, transparent);"
        onmouseenter={enter}
        onmouseleave={leave}
        role="group"
        aria-label="Unresolved page link"
      >
        <div
          class="text-type-2xs text-text-muted uppercase tracking-widest font-label-sm-bold mb-2"
        >
          Page not found
        </div>
        {#if createError}
          <p class="text-type-2xs text-status-danger mb-1.5" role="alert">
            {createError}
          </p>
        {/if}
        <button
          type="button"
          class="w-full text-left px-2 py-1.5 rounded-md text-sm text-accent-primary-start hover:bg-hover cursor-pointer border-0 bg-transparent inline-flex items-center gap-1.5"
          onclick={() => void createPage()}
          disabled={creating}
          aria-label="Create page '{target}'"
        >
          <span class="material-symbols-outlined text-[1.1em]">add_circle</span>
          <span aria-live="polite"
            >{creating ? 'Creating…' : 'Create page'}</span
          >
        </button>
      </div>
    {/if}
  </div>
{:else}
  <div class="inline-block relative">
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <span
      role="link"
      tabindex="0"
      onclick={click}
      onkeydown={(e) => e.key === 'Enter' && click()}
      onmouseenter={enter}
      onmouseleave={leave}
      class="inline-flex items-center align-baseline gap-1 text-accent-primary-start hover:text-accent-primary-end underline decoration-dotted underline-offset-4 cursor-pointer mx-0.5"
      title={[ref.notebook, ref.section, ref.page].filter(Boolean).join(' › ')}
    >
      <span class="material-symbols-outlined text-[0.9em]">description</span>
      <span class="truncate max-w-[18ch]">{label}</span>
    </span>

    {#if showHover}
      <div
        transition:fade={{ duration: 120 }}
        class="absolute z-50 top-full left-0 mt-1 w-80 max-w-[80vw] glass-palette border border-surface-popover-border rounded-lg shadow-2xl p-3 text-left"
        style="backdrop-filter: blur(16px) saturate(140%); background: color-mix(in srgb, var(--color-surface-popover) 94%, transparent);"
      >
        <div
          class="flex items-center gap-1 text-type-2xs text-text-muted uppercase tracking-widest font-label-sm-bold mb-2"
        >
          <span>{ref.notebook}</span>
          {#if ref.section}
            <span class="material-symbols-outlined text-type-2xs"
              >chevron_right</span
            >
            <span>{ref.section}</span>
          {/if}
          <span class="material-symbols-outlined text-type-2xs"
            >chevron_right</span
          >
          <span class="text-accent-primary-start">{ref.page}</span>
        </div>
        {#if heading}
          <div class="font-body-md text-sm text-text-primary">#{heading}</div>
        {/if}
        {#if ref.shortest}
          <div class="mt-1 text-type-2xs text-text-muted font-mono">
            [[{ref.shortest}]]
          </div>
        {/if}
      </div>
    {/if}
  </div>
{/if}
