<script lang="ts">
  // Settings → Appearance tab (#47, #48).
  //
  // Live, accessible theme picker + dark/light/system mode toggle +
  // custom-theme import/export. Fully data-driven from themesState (the
  // listing store populated by ListThemes); zero per-theme code
  // branches. Live preview on hover/focus is implemented via a local
  // previewTokens state that overrides the active theme's tokens for
  // the duration of the hover; pressing Esc or moving focus off the row
  // restores the active theme.
  import { onMount } from 'svelte'
  import { injectTokens } from '../../theme/inject'
  import { displayFamilyName } from '../../theme/fonts'
  import {
    applyTheme,
    clearStatus,
    exportActiveTheme,
    loadThemes,
    pickAndImportTheme,
    restoreActiveTheme,
    systemScheme,
    themeState,
    themesState,
    themeStatus,
    type ThemeMode,
    type ThemeStatus
  } from '../../theme/store.svelte'

  type Props = Record<string, never>

  let {}: Props = $props()

  // Per-row preview state. When non-null, the effect at the bottom of
  // the file injects the preview theme's tokens in place of the active
  // theme's; on blur / mouseleave / Esc, the row clears it.
  let previewId: string | null = $state(null)

  // Currently-focused row index (for roving tabindex). null = no row focused.
  let focusIndex: number | null = $state(null)
  let rowRefs: HTMLButtonElement[] = $state([])

  onMount(() => {
    // Initial load: if the listing hasn't been populated by App.svelte's
    // initThemes() yet (e.g. user opened Settings before the async load
    // completed), kick off a refresh here too.
    if (themesState.items.length === 0 && !themesState.loading) {
      void loadThemes()
    }
    // Drag-drop: a *.json file dropped anywhere on the tab is imported
    // through the same code path as the picker button.
    // TODO(wails-v3): restore drag-drop import. The v3 runtime dropped the
    // global OnFileDrop/OnFileDropOff pair in favour of the per-element
    // `data-file-drop-target` HTML attribute + the window's EnableFileDrop
    // option. Wire the drop zone up against that API and re-enable the
    // .json import path below.
    return () => {
      // Clear any in-flight preview so a navigated-away tab doesn't
      // leave the page in a non-active theme.
      previewId = null
    }
  })

  // --- Mode toggle ---------------------------------------------------------

  const modes: { id: ThemeMode; label: string; icon: string }[] = [
    { id: 'dark', label: 'Dark', icon: 'dark_mode' },
    { id: 'light', label: 'Light', icon: 'light_mode' },
    { id: 'system', label: 'System', icon: 'desktop_windows' }
  ]

  function setMode(mode: ThemeMode) {
    if (mode === themeState.mode) return
    void applyTheme(themeState.id || 'cyber_forest', mode)
  }

  // --- Theme picker --------------------------------------------------------

  function isActive(t: { id: string }): boolean {
    return themeState.id === t.id
  }

  function selectTheme(id: string) {
    if (id === themeState.id) return
    void applyTheme(id, themeState.mode)
  }

  function onRowEnter(id: string) {
    previewId = id
  }
  function onRowLeave() {
    previewId = null
  }
  function onRowKey(e: KeyboardEvent, index: number) {
    const last = themesState.items.length - 1
    if (e.key === 'ArrowDown' || e.key === 'ArrowRight') {
      e.preventDefault()
      const next = Math.min(last, index + 1)
      focusIndex = next
      rowRefs[next]?.focus()
    } else if (e.key === 'ArrowUp' || e.key === 'ArrowLeft') {
      e.preventDefault()
      const prev = Math.max(0, index - 1)
      focusIndex = prev
      rowRefs[prev]?.focus()
    } else if (e.key === 'Home') {
      e.preventDefault()
      focusIndex = 0
      rowRefs[0]?.focus()
    } else if (e.key === 'End') {
      e.preventDefault()
      focusIndex = last
      rowRefs[last]?.focus()
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      const item = themesState.items[index]
      if (item) selectTheme(item.id)
    } else if (e.key === 'Escape') {
      // Esc cancels any in-flight preview (matches the live-preview AC).
      e.preventDefault()
      previewId = null
    }
  }

  // Window-level key handler: Esc also cancels preview when focus is
  // outside the list (e.g. on the mode toggle).
  function onWindowKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && previewId !== null) {
      previewId = null
    }
  }

  // --- Live preview injection ---------------------------------------------

  /**
   * The token map to inject for the current paint frame. When a preview
   * is active, the preview theme's tokens for the current mode are
   * used; otherwise the active theme's tokens. Falls back to the
   * active theme's map if the preview theme's tokens aren't available
   * (e.g. mid-import) so the picker never blanks the page.
   */
  let previewTokens: Record<string, string> | null = $derived.by(() => {
    if (previewId === null) return null
    const ft = themesState.flatTokens[previewId]
    if (!ft) return null
    if (themeState.mode === 'light') return ft.light
    if (themeState.mode === 'system') {
      const prefersLight =
        typeof window !== 'undefined' &&
        window.matchMedia &&
        window.matchMedia('(prefers-color-scheme: light)').matches
      return prefersLight ? ft.light : ft.dark
    }
    return ft.dark
  })

  // Side-effect: when previewTokens changes (hover/unhover) or the
  // active mode flips, re-inject the right token map. The injector
  // uses a single textContent rewrite so the repaint is same-tick.
  // The else branch is critical: when a preview ends (mouseleave,
  // blur, Esc), previewTokens goes null and we MUST re-inject the
  // active theme's tokens — otherwise the page stays visually locked
  // to the last-hovered theme until the user manually clicks one.
  $effect(() => {
    if (previewTokens !== null) {
      injectTokens(previewTokens)
    } else {
      restoreActiveTheme()
    }
  })

  // --- Helpers -------------------------------------------------------------

  // Active-theme typography overrides (theme-level; both modes carry the same
  // --font-* values). When non-empty, the Appearance tab surfaces an indicator
  // so the user knows the active theme is overriding their General-tab font
  // choices, and the General tab shows a "Reset to theme default" affordance.
  let themeTypographyOverrides = $derived.by(() => {
    const tokens = themeState.darkTokens
    const out: { label: string; value: string }[] = []
    if (tokens['--font-body'])
      out.push({ label: 'Body', value: tokens['--font-body'] })
    if (tokens['--font-mono'])
      out.push({ label: 'Mono', value: tokens['--font-mono'] })
    if (tokens['--font-headline'])
      out.push({ label: 'Headline', value: tokens['--font-headline'] })
    return out
  })

  function handleImport() {
    void pickAndImportTheme()
  }
  function handleExport() {
    void exportActiveTheme()
  }

  function statusAriaRole(s: ThemeStatus | null): 'status' | 'alert' | null {
    if (!s || !s.message) return null
    return s.kind === 'error' ? 'alert' : 'status'
  }

  // A class string driven by theme status, used for the live region
  // styling. Kept simple — bg/border + text colour, no per-status icon
  // (the status kind is conveyed by the aria role).
  function statusClasses(s: ThemeStatus | null): string {
    if (!s || !s.message) return ''
    if (s.kind === 'error') {
      return 'bg-error-bg border border-error-border text-error'
    }
    if (s.kind === 'success') {
      return 'bg-accent-primary-start/10 border border-accent-primary-start/30 text-accent-primary-start'
    }
    return 'bg-surface-panel border border-surface-panel-border text-text-muted'
  }

  const effectiveMode = $derived(
    themeState.mode === 'system' ? systemScheme.mode : themeState.mode
  )

  // SR companion to the visible `· Dark`/`· Light` suffix (which is
  // aria-hidden to avoid colliding with the Dark/Light radio names).
  // Empty outside system mode so the live region stays silent — the
  // Dark/Light radios already convey their own selection.
  const systemSchemeAnnouncement = $derived(
    themeState.mode === 'system' ? `Using ${systemScheme.mode} appearance` : ''
  )
</script>

<svelte:window on:keydown={onWindowKey} />

<div class="p-6 max-w-3xl space-y-8">
  <!-- Mode toggle -->
  <section aria-labelledby="mode-heading">
    <h3
      id="mode-heading"
      class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
    >
      Mode
    </h3>
    <div
      role="radiogroup"
      aria-label="Color mode"
      class="inline-flex bg-surface-panel border border-surface-panel-border rounded-lg p-1 gap-1"
    >
      {#each modes as m (m.id)}
        {@const active = themeState.mode === m.id}
        <button
          type="button"
          role="radio"
          aria-checked={active}
          onclick={() => setMode(m.id)}
          class="flex items-center gap-1.5 px-3 py-1.5 rounded-md font-label-sm text-label-sm motion-reduce:transition-none transition-colors border-none cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
          class:bg-hover={active}
          class:text-accent-primary-start={active}
          class:text-text-muted={!active}
          class:hover:text-text-primary={!active}
          class:ring-1={active}
          class:ring-accent-primary-start={active}
        >
          <span class="material-symbols-outlined text-[16px]">{m.icon}</span>
          {m.label}
          {#if m.id === 'system'}
            <span class="text-text-muted font-label-sm" aria-hidden="true"
              >· {systemScheme.mode === 'dark' ? 'Dark' : 'Light'}</span
            >
          {/if}
        </button>
      {/each}
    </div>
    <p class="text-text-muted text-[11px] font-label-sm mt-2">
      "System" follows your OS appearance preference. Switching mode does not
      change the active theme.
    </p>
    <!-- SR-only live region: announces the resolved scheme only while
         System mode is active. The visible `· Dark`/`· Light` suffix
         is aria-hidden (avoids radio-name collisions), so without this
         region a System-mode user would never hear which scheme is
         currently resolved or when the OS flips it. -->
    <div class="sr-only" aria-live="polite">
      {systemSchemeAnnouncement}
    </div>
  </section>

  <!-- Active-theme typography overrides (#82) -->
  {#if themeTypographyOverrides.length > 0}
    <section aria-labelledby="typo-heading">
      <h3
        id="typo-heading"
        class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
      >
        Theme typography
      </h3>
      <div
        class="flex flex-wrap items-start gap-x-4 gap-y-1.5 bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2.5"
      >
        <span class="text-text-muted text-[11px] font-label-sm">
          This theme sets its own fonts:
        </span>
        {#each themeTypographyOverrides as o (o.label)}
          <span class="text-text-primary text-[12px] font-body-md">
            <span class="text-text-muted">{o.label}:</span>
            {displayFamilyName(o.value)}
          </span>
        {/each}
      </div>
      <p class="text-text-muted text-[11px] font-label-sm mt-2">
        Body and Mono can be overridden in General (or reset there to inherit
        these). Headline is set by the theme only.
      </p>
    </section>
  {/if}

  <!-- Theme list -->
  <section aria-labelledby="theme-heading">
    <div class="flex items-center justify-between mb-3">
      <h3
        id="theme-heading"
        class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px]"
      >
        Theme
      </h3>
      <span class="text-text-muted text-[11px] font-label-sm">
        {themesState.items.length}
        {themesState.items.length === 1 ? 'theme' : 'themes'}
      </span>
    </div>

    {#if themesState.loading && themesState.items.length === 0}
      <div
        class="text-text-muted text-[12px] font-body-md animate-pulse py-8 text-center"
      >
        Loading themes…
      </div>
    {:else if themesState.loadError}
      <div
        class="flex items-start gap-2 p-3 rounded-lg bg-error-bg border border-error-border text-error text-[12px] font-body-md"
        role="alert"
      >
        <span class="material-symbols-outlined text-[18px]">error</span>
        <span class="flex-1"
          >Failed to load themes: {themesState.loadError}</span
        >
      </div>
    {:else if themesState.items.length === 0}
      <div class="text-text-muted text-[12px] font-body-md py-8 text-center">
        No themes available. Import a theme .json to get started.
      </div>
    {:else}
      <div role="listbox" aria-label="Available themes" class="space-y-2">
        {#each themesState.items as theme, i (theme.id)}
          {@const active = isActive(theme)}
          {@const modeTokens =
            themesState.flatTokens[theme.id]?.[effectiveMode]}
          <button
            type="button"
            id={`theme-row-${theme.id}`}
            role="option"
            aria-selected={active}
            tabindex={focusIndex === i || (focusIndex === null && i === 0)
              ? 0
              : -1}
            bind:this={rowRefs[i]}
            onclick={() => selectTheme(theme.id)}
            onmouseenter={() => onRowEnter(theme.id)}
            onmouseleave={onRowLeave}
            onfocus={() => {
              focusIndex = i
              onRowEnter(theme.id)
            }}
            onblur={onRowLeave}
            onkeydown={(e) => onRowKey(e, i)}
            class="w-full text-left flex items-center gap-3 px-3 py-2.5 rounded-lg border motion-reduce:transition-none transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 cursor-pointer"
            class:bg-surface-panel={!active}
            class:border-surface-panel-border={!active}
            class:hover:border-border-active={!active}
            class:border-l-4={active}
            class:border-l-accent-primary-start={active}
            class:bg-accent-primary-glow={active}
            class:border-accent-primary-start={active}
          >
            <!-- Swatch: a mini theme card (#405). The dominant visual identity
                 of a theme is its surface temperature (warm Linen taupe, cool
                 Frost, neutral Graphite), not its accents — so the base chip
                 is filled with the theme's app surface bg (read from
                 flatTokens, the same map the injector writes). The two accent
                 starts render as dots on the chip so the accents are still
                 visible but the surface reads first. Data-driven: no per-theme
                 branching. -->
            <div class="flex items-center gap-1.5 flex-shrink-0">
              <span
                aria-hidden="true"
                class="theme-swatch-chip"
                style="background-color: {modeTokens?.['--color-surface-app'] ??
                  'var(--color-surface-app)'}"
              >
                <span
                  class="theme-swatch-dot"
                  style="background-color: {modeTokens?.[
                    '--color-accent-primary-start'
                  ] ??
                    theme.swatches?.[0] ??
                    'var(--color-accent-primary-start)'}"
                ></span>
                <span
                  class="theme-swatch-dot"
                  style="background-color: {modeTokens?.[
                    '--color-accent-secondary-start'
                  ] ??
                    theme.swatches?.[1] ??
                    'var(--color-accent-secondary-start)'}"
                ></span>
              </span>
            </div>
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2">
                <span
                  class="text-text-primary text-[13px] font-body-md truncate"
                >
                  {theme.name}
                </span>
                {#if active}
                  <span
                    class="text-[10px] text-accent-primary-start font-label-sm-bold uppercase tracking-wider flex-shrink-0"
                  >
                    Active
                  </span>
                {/if}
              </div>
              {#if theme.author || theme.description}
                <div class="text-text-muted text-[11px] font-label-sm truncate">
                  {theme.author ? `by ${theme.author}` : ''}
                  {theme.author && theme.description ? ' · ' : ''}
                  {theme.description ?? ''}
                </div>
              {/if}
            </div>
            <span
              class="material-symbols-outlined text-text-muted text-[18px] flex-shrink-0"
            >
              {active ? 'check_circle' : 'chevron_right'}
            </span>
          </button>
        {/each}
      </div>
    {/if}
  </section>

  <!-- Custom theme import/export -->
  <section aria-labelledby="custom-heading">
    <h3
      id="custom-heading"
      class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
    >
      Custom themes
    </h3>
    <div class="flex flex-wrap items-center gap-2">
      <button
        type="button"
        onclick={handleImport}
        class="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-accent-primary-start/20 border border-accent-primary-start/40 text-accent-primary-start font-label-sm-bold hover:brightness-110 motion-reduce:transition-none transition-all cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
      >
        <span class="material-symbols-outlined text-[18px]">upload</span>
        Import .json
      </button>
      <button
        type="button"
        onclick={handleExport}
        disabled={!themeState.id}
        class="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-surface-panel border border-surface-panel-border text-text-primary font-label-sm-bold hover:border-accent-primary-start motion-reduce:transition-none transition-colors cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 disabled:opacity-40 disabled:cursor-not-allowed"
      >
        <span class="material-symbols-outlined text-[18px]">download</span>
        Export active
      </button>
    </div>
    <p class="text-text-muted text-[11px] font-label-sm mt-2">
      Drop a theme .json file anywhere in this tab to import. Imported themes
      are validated against the canonical schema and appear in the list above
      immediately.
    </p>
  </section>

  <!-- Live status region (a11y: aria-live="polite" for success/info, role="alert" for errors) -->
  {#if themeStatus.message}
    <div
      role={statusAriaRole(themeStatus) ?? undefined}
      aria-live={themeStatus.kind === 'error' ? 'assertive' : 'polite'}
      class="rounded-lg px-3 py-2 text-[12px] font-body-md {statusClasses(
        themeStatus
      )}"
    >
      <div class="flex items-start gap-2">
        <span class="material-symbols-outlined text-[16px] flex-shrink-0">
          {themeStatus.kind === 'error'
            ? 'error'
            : themeStatus.kind === 'success'
              ? 'check_circle'
              : 'info'}
        </span>
        <div class="flex-1 min-w-0">
          <div>{themeStatus.message}</div>
          {#if themeStatus.fields.length > 0}
            <ul class="mt-1.5 ml-4 list-disc space-y-0.5">
              {#each themeStatus.fields as f (f.field)}
                <li>
                  <code class="font-mono text-[11px]">{f.field}</code>: {f.message}
                </li>
              {/each}
            </ul>
          {/if}
          <button
            type="button"
            onclick={() => clearStatus()}
            class="mt-1.5 text-[11px] font-label-sm-bold underline opacity-70 hover:opacity-100 bg-transparent border-none cursor-pointer"
          >
            Dismiss
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  /* Visually hidden but available to assistive tech. Used by the
     system-scheme aria-live region. Matches the locally-scoped
     .sr-only in PluginNoteBanners.svelte / Calendar.svelte (no global
     utility exists). */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }

  /* Mini theme-card swatch (#405): a base chip filled with the theme's app
     surface bg (the dominant temperature signal) with the two accent starts
     as dots inside it. The chip is wider than the old two-bar swatch so the
     surface color is the first thing the eye reads — warm Linen taupe vs
     neutral Graphite grey vs cool Frost are distinguishable at a glance. */
  .theme-swatch-chip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 3px;
    width: 36px;
    height: 28px;
    border-radius: 5px;
    border: 1px solid var(--color-surface-panel-border);
    flex-shrink: 0;
  }

  .theme-swatch-dot {
    display: block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    /* Subtle ring so a dot that matches the surface bg (monochrome themes)
       is still visible against the chip. */
    box-shadow: 0 0 0 1px color-mix(in srgb, currentColor 15%, transparent);
  }
</style>
