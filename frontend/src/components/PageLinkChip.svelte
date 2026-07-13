<script lang="ts">
  // Inline [[target]] wiki-link chip (#545). Resolves via ResolvePageLink and
  // dispatches navigate-to-page on click/Enter. Mirrors BlockReferenceChip.
  import { onMount } from 'svelte'
  import { fade } from 'svelte/transition'
  import { ResolvePageLink } from '../../bindings/silt/app.js'

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
  let hoverTimer: ReturnType<typeof setTimeout> | null = null

  const label = $derived(alias || target)

  function pathLabel(c: {
    notebook: string
    section: string
    page: string
  }): string {
    return [c.notebook, c.section, c.page].filter(Boolean).join(' › ')
  }

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

  onMount(() => {
    load()
  })

  function enter() {
    if (hoverTimer) clearTimeout(hoverTimer)
    hoverTimer = setTimeout(() => (showHover = true), 250)
  }

  function leave() {
    if (hoverTimer) clearTimeout(hoverTimer)
    hoverTimer = setTimeout(() => (showHover = false), 150)
  }

  function click() {
    if (ref && ref.exists) {
      window.dispatchEvent(
        new CustomEvent('navigate-to-page', {
          detail: {
            notebook: ref.notebook,
            section: ref.section ?? '',
            page: ref.page,
            heading: heading || undefined
          }
        })
      )
    }
  }

  function openCandidate(c: {
    notebook: string
    section: string
    page: string
  }) {
    showHover = false
    window.dispatchEvent(
      new CustomEvent('navigate-to-page', {
        detail: {
          notebook: c.notebook,
          section: c.section ?? '',
          page: c.page,
          heading: heading || undefined
        }
      })
    )
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
      onmouseenter={enter}
      onmouseleave={leave}
      onkeydown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          showHover = !showHover
        }
      }}
      class="inline-flex items-center align-baseline text-status-warning mx-0.5 text-[0.85em] cursor-help underline decoration-dotted underline-offset-4"
      title="Ambiguous page link — multiple matches. Hover to pick one."
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
      </div>
    {/if}
  </div>
{:else if !ref?.exists}
  <span
    class="inline-flex items-center align-baseline text-text-muted line-through mx-0.5 text-[0.85em]"
    title="Unresolved page link"
  >
    [[{label}]]
  </span>
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
