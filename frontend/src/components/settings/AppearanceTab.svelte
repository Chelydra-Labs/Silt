<script lang="ts">
  // Settings → Appearance tab (#47, #48, #512).
  //
  // Card grid + details-pane theme picker with a two-stage preview that
  // kills the "strobe": hover only highlights a card (CSS, no token
  // injection); a single click stages a temporary preview that injects the
  // theme's tokens workspace-wide and shows an Apply/Revert banner; Apply
  // (or a double-click) commits via applyTheme, Revert/Esc restores the
  // active theme. Fully data-driven from themesState (the listing store
  // populated by ListThemes); zero per-theme code branches.
  import { onMount } from 'svelte'
  import { Events } from '@wailsio/runtime'
  import { injectTokens } from '../../theme/inject'
  import { displayFamilyName } from '../../theme/fonts'
  import {
    applyTheme,
    clearStatus,
    exportActiveTheme,
    importThemeFromPath,
    loadThemes,
    pickAndImportTheme,
    restoreActiveTheme,
    setStatus,
    systemScheme,
    themeState,
    themesState,
    themeStatus,
    type ThemeMode,
    type ThemeStatus
  } from '../../theme/store.svelte'

  type Props = Record<string, never>

  let {}: Props = $props()

  // Two-stage preview state (#512). The id of the theme currently staged
  // for preview, or null. Hover never touches this — only a click does —
  // so moving the pointer across the grid no longer rewrites :root tokens
  // on every row crossing (the source of the strobe).
  let previewTheme: string | null = $state(null)

  // Roving-tabindex focus for the card grid (one tab stop, arrows move).
  let focusIndex: number | null = $state(null)
  let cardRefs: HTMLButtonElement[] = $state([])

  onMount(() => {
    // Initial load: if the listing hasn't been populated by App.svelte's
    // initThemes() yet (e.g. user opened Settings before the async load
    // completed), kick off a refresh here too.
    if (themesState.items.length === 0 && !themesState.loading) {
      void loadThemes()
    }
    // Drag-drop import: the backend (main.go) only forwards OS file drops
    // that land on #theme-file-drop-target, emitting theme:files-dropped
    // with the dropped paths. We reuse importThemeFromPath — the exact
    // function the picker button's pickAndImportTheme calls after the native
    // open dialog — so success/error feedback routes through the same
    // themeStatus live region either way.
    const offDrop = Events.On('theme:files-dropped', (ev: any) => {
      handleDroppedFiles(ev?.data)
    })
    return () => {
      // Drop any in-flight preview so a navigated-away tab never leaves the
      // workspace locked to a non-active theme.
      previewTheme = null
      offDrop()
    }
  })

  // Accepts the backend's dropped-paths payload and imports exactly one
  // theme .json. Anything else (no file, a non-json file, or several files)
  // is reported through themeStatus so the guidance is announced in the
  // same live region the picker uses — no separate toast path.
  function handleDroppedFiles(paths: unknown): void {
    if (!Array.isArray(paths)) return
    const json = paths.filter(
      (p): p is string =>
        typeof p === 'string' && p.toLowerCase().endsWith('.json')
    )
    if (json.length === 0) {
      setStatus({
        kind: 'error',
        message: 'Drop a theme .json file to import it.',
        fields: []
      })
      return
    }
    if (json.length > 1) {
      setStatus({
        kind: 'error',
        message: 'Drop one theme file at a time.',
        fields: []
      })
      return
    }
    void importThemeFromPath(json[0])
  }

  // --- Mode toggle ---------------------------------------------------------
  // The Dark/Light/System toggle controls which mode the *selected* theme
  // renders in. It is NOT a filter (every theme carries both token maps).

  const modes: { id: ThemeMode; label: string; icon: string }[] = [
    { id: 'dark', label: 'Dark', icon: 'dark_mode' },
    { id: 'light', label: 'Light', icon: 'light_mode' },
    { id: 'system', label: 'System', icon: 'desktop_windows' }
  ]

  function setMode(mode: ThemeMode) {
    if (mode === themeState.mode) return
    void applyTheme(themeState.id || 'cyber_forest', mode)
  }

  // --- Theme cards ---------------------------------------------------------

  function isActive(t: { id: string }): boolean {
    return themeState.id === t.id
  }
  function isPreviewing(id: string): boolean {
    return previewTheme === id
  }

  // Stage 1: a single click (or keyboard activation of the <button>) enters
  // the temporary preview. Re-clicking the same card is a no-op so the
  // banner stays stable instead of flickering.
  function activateCard(id: string) {
    if (previewTheme === id) return
    previewTheme = id
  }

  // Stage 2: commit the previewed theme permanently. Kept async so a failed
  // apply (e.g. disk write error) leaves the preview staged — the user can
  // read the status-region error and then Revert.
  async function commitPreview() {
    if (previewTheme === null) return
    const id = previewTheme
    const ok = await applyTheme(id, themeState.mode)
    if (ok) previewTheme = null
  }

  // Double-click commits directly. The preceding single-clicks have already
  // staged this card, but we set it explicitly so the commit targets the
  // card under the pointer even if event ordering ever varies.
  function onCardDblClick(id: string) {
    previewTheme = id
    void commitPreview()
  }

  function revertPreview() {
    previewTheme = null
  }

  function onCardKey(e: KeyboardEvent, index: number) {
    const last = themesState.items.length - 1
    if (e.key === 'ArrowDown' || e.key === 'ArrowRight') {
      e.preventDefault()
      const next = Math.min(last, index + 1)
      focusIndex = next
      cardRefs[next]?.focus()
    } else if (e.key === 'ArrowUp' || e.key === 'ArrowLeft') {
      e.preventDefault()
      const prev = Math.max(0, index - 1)
      focusIndex = prev
      cardRefs[prev]?.focus()
    } else if (e.key === 'Home') {
      e.preventDefault()
      focusIndex = 0
      cardRefs[0]?.focus()
    } else if (e.key === 'End') {
      e.preventDefault()
      focusIndex = last
      cardRefs[last]?.focus()
    } else if (e.key === 'Escape') {
      // Esc reverts any in-flight preview.
      e.preventDefault()
      revertPreview()
    }
    // Enter / Space activate the card through native <button> → click; they
    // are deliberately not handled here to avoid a double activation.
  }

  // Window-level Esc: also cancels a preview when focus has moved off the
  // grid (e.g. onto the mode toggle or the import buttons).
  function onWindowKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && previewTheme !== null) {
      revertPreview()
    }
  }

  // --- Preview injection ---------------------------------------------------

  // Resolved dark/light (never "system") for the current mode.
  const effectiveMode = $derived(
    themeState.mode === 'system' ? systemScheme.mode : themeState.mode
  )

  // Token map for the staged preview theme in the current mode. null when
  // nothing is staged or the listing hasn't loaded that theme's tokens yet
  // (the $effect then leaves the workspace untouched rather than blanking).
  let previewTokens = $derived.by(() => {
    if (previewTheme === null) return null
    const ft = themesState.flatTokens[previewTheme]
    if (!ft) return null
    return effectiveMode === 'light' ? ft.light : ft.dark
  })

  // Inject the staged theme's tokens while previewing, and restore the
  // active theme when the preview ends (Revert/Esc/commit). The else branch
  // is load-bearing: without it the workspace would stay locked to the last
  // previewed theme after Revert. The guard on `previewTheme === null`
  // skips a premature restore when tokens aren't loaded yet for a staged id.
  $effect(() => {
    if (previewTokens !== null) {
      injectTokens(previewTokens)
    } else if (previewTheme === null) {
      restoreActiveTheme()
    }
  })

  // --- Details pane --------------------------------------------------------
  // Follows the staged preview when one is active, otherwise the saved
  // active theme, so the pane always shows something meaningful.

  let detailTheme = $derived.by(() => {
    const id = previewTheme ?? themeState.id
    return themesState.items.find((t) => t.id === id) ?? null
  })

  let detailModeTokens = $derived.by(() => {
    if (!detailTheme) return null
    return themesState.flatTokens[detailTheme.id]?.[effectiveMode] ?? null
  })

  // Theme-authored typography (fonts live identically in both token maps).
  let detailTypography = $derived.by(() => {
    const id = previewTheme ?? themeState.id
    const ft = id ? themesState.flatTokens[id] : null
    if (!ft) return [] as { label: string; value: string }[]
    const t = ft.dark
    const out: { label: string; value: string }[] = []
    if (t['--font-body']) out.push({ label: 'Body', value: t['--font-body'] })
    if (t['--font-mono']) out.push({ label: 'Mono', value: t['--font-mono'] })
    if (t['--font-headline'])
      out.push({ label: 'Headline', value: t['--font-headline'] })
    return out
  })

  // Swatches the details-pane color strip renders (surface + the two
  // accents), pulled from the same flat-token map the injector uses.
  let detailSwatches = $derived.by(() => {
    if (!detailModeTokens) return [] as string[]
    return [
      detailModeTokens['--color-surface-app'],
      detailModeTokens['--color-accent-primary-start'],
      detailModeTokens['--color-accent-secondary-start']
    ].filter((v): v is string => typeof v === 'string' && v.length > 0)
  })

  function sourceLabel(s: string | undefined | null): string {
    switch (s) {
      case 'disk':
        return 'Custom'
      case 'bundled':
        return 'Bundled'
      default:
        return 'Built-in'
    }
  }

  // Active-theme typography overrides (theme-level; both modes carry the
  // same --font-* values). When non-empty, the Appearance tab surfaces an
  // indicator so the user knows the active theme is overriding their
  // Editor-tab font choices, and the Editor tab shows a "Reset to theme
  // default" affordance.
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
  // styling.
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

  // SR companion to the visible `· Dark`/`· Light` suffix (which is
  // aria-hidden to avoid colliding with the Dark/Light radio names).
  const systemSchemeAnnouncement = $derived(
    themeState.mode === 'system' ? `Using ${systemScheme.mode} appearance` : ''
  )

  let previewName = $derived(
    previewTheme
      ? (themesState.items.find((t) => t.id === previewTheme)?.name ??
          previewTheme)
      : ''
  )
</script>

<svelte:window onkeydown={onWindowKey} />

<div class="p-6 max-w-6xl mx-auto w-full space-y-8">
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
          <span class="material-symbols-outlined text-[16px]" aria-hidden="true"
            >{m.icon}</span
          >
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
        Body and Mono can be overridden in Editor (or reset there to inherit
        these). Headline is set by the theme only.
      </p>
    </section>
  {/if}

  <!-- Theme grid + details -->
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

    <!-- Preview banner: workspace-wide state shown while a theme is staged.
         role=status + aria-live so an SR user hears "Previewing X" the
         moment the preview starts, and again when it is applied/reverted. -->
    {#if previewTheme !== null}
      <div
        role="status"
        aria-live="polite"
        class="flex items-center gap-3 mb-4 rounded-lg border border-accent-secondary-start/50 bg-accent-secondary-start/10 px-3 py-2.5"
      >
        <span
          class="material-symbols-outlined text-accent-secondary-start text-[18px] flex-shrink-0"
          aria-hidden="true">visibility</span
        >
        <div class="flex-1 min-w-0">
          <div
            class="text-text-primary text-[12px] font-label-sm-bold truncate"
          >
            Previewing {previewName}
          </div>
          <div class="text-text-muted text-[11px] font-label-sm">
            Not saved yet. Apply to keep, or Revert.
          </div>
        </div>
        <button
          type="button"
          onclick={commitPreview}
          class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-accent-primary-start/20 border border-accent-primary-start/50 text-accent-primary-start font-label-sm-bold hover:brightness-110 motion-reduce:transition-none transition-all cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
        >
          <span class="material-symbols-outlined text-[16px]" aria-hidden="true"
            >check</span
          >
          Apply
        </button>
        <button
          type="button"
          onclick={revertPreview}
          class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary font-label-sm-bold hover:border-border-active motion-reduce:transition-none transition-colors cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
        >
          <span class="material-symbols-outlined text-[16px]" aria-hidden="true"
            >undo</span
          >
          Revert
        </button>
      </div>
    {/if}

    <!-- Drop target: Wails adds .file-drop-target-active to this element
         while OS files are dragged over it (main.go only forwards drops that
         land here). Wraps all list states so a drop registers whether the
         list is populated, loading, or empty. -->
    <div
      id="theme-file-drop-target"
      data-file-drop-target
      class="rounded-xl transition motion-reduce:transition-none"
    >
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
          <span class="material-symbols-outlined text-[18px]" aria-hidden="true"
            >error</span
          >
          <span class="flex-1"
            >Failed to load themes: {themesState.loadError}</span
          >
        </div>
      {:else if themesState.items.length === 0}
        <div class="text-text-muted text-[12px] font-body-md py-8 text-center">
          No themes available. Import or drop a theme .json to get started.
        </div>
      {:else}
        <div class="grid grid-cols-1 lg:grid-cols-[2fr_1fr] gap-4">
          <!-- Card grid -->
          <div
            role="group"
            aria-label="Available themes"
            class="grid grid-cols-1 sm:grid-cols-2 2xl:grid-cols-3 gap-3"
          >
            {#each themesState.items as theme, i (theme.id)}
              {@const active = isActive(theme)}
              {@const previewing = isPreviewing(theme.id)}
              {@const modeTokens =
                themesState.flatTokens[theme.id]?.[effectiveMode]}
              <button
                type="button"
                id={`theme-card-${theme.id}`}
                tabindex={focusIndex === i || (focusIndex === null && i === 0)
                  ? 0
                  : -1}
                bind:this={cardRefs[i]}
                onclick={() => activateCard(theme.id)}
                ondblclick={() => onCardDblClick(theme.id)}
                onkeydown={(e) => onCardKey(e, i)}
                class="relative text-left flex flex-col gap-2.5 p-3 rounded-xl border motion-reduce:transition-none transition-all focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 cursor-pointer"
                class:bg-surface-panel={!active && !previewing}
                class:border-surface-panel-border={!active && !previewing}
                class:hover:border-border-active={!active && !previewing}
                class:hover:bg-hover={!active && !previewing}
                class:border-accent-primary-start={active}
                class:bg-accent-primary-glow={active}
                class:border-accent-secondary-start={previewing && !active}
                class:ring-2={previewing && !active}
                class:ring-accent-secondary-start={previewing && !active}
              >
                <div class="flex items-center justify-between gap-2">
                  <!-- Mini theme-card swatch (#405): a base chip filled with
                       the theme's app surface bg (the dominant temperature
                       signal) with the two accent starts as dots inside. -->
                  <span
                    aria-hidden="true"
                    class="theme-swatch-chip"
                    style="background-color: {modeTokens?.[
                      '--color-surface-app'
                    ] ?? 'var(--color-surface-app)'}"
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
                  {#if active}
                    <span
                      class="inline-flex items-center gap-1 text-[10px] text-accent-primary-start font-label-sm-bold uppercase tracking-wider flex-shrink-0"
                    >
                      <span
                        class="material-symbols-outlined text-[14px]"
                        aria-hidden="true">check_circle</span
                      >
                      Active
                    </span>
                  {:else if previewing}
                    <span
                      class="inline-flex items-center gap-1 text-[10px] text-accent-secondary-start font-label-sm-bold uppercase tracking-wider flex-shrink-0"
                    >
                      <span
                        class="material-symbols-outlined text-[14px]"
                        aria-hidden="true">visibility</span
                      >
                      Preview
                    </span>
                  {/if}
                </div>
                <div class="min-w-0">
                  <div
                    class="text-text-primary text-[13px] font-body-md truncate"
                  >
                    {theme.name}
                  </div>
                  <div
                    class="text-text-muted text-[11px] font-label-sm truncate"
                  >
                    {theme.author
                      ? `by ${theme.author}`
                      : sourceLabel(theme.source)}
                  </div>
                </div>
              </button>
            {/each}
          </div>

          <!-- Details pane -->
          <aside
            aria-labelledby="theme-details-heading"
            class="rounded-xl border border-surface-panel-border bg-surface-card p-4 self-start"
          >
            <h4
              id="theme-details-heading"
              class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
            >
              Theme details
            </h4>
            {#if detailTheme}
              {@const detailActive = isActive(detailTheme)}
              {@const detailPreviewing = isPreviewing(detailTheme.id)}
              <div class="flex items-start justify-between gap-2 mb-3">
                <h5
                  class="text-text-primary text-[16px] font-headline-md leading-tight"
                >
                  {detailTheme.name}
                </h5>
                {#if detailActive}
                  <span
                    class="inline-flex items-center gap-1 text-[10px] text-accent-primary-start font-label-sm-bold uppercase tracking-wider flex-shrink-0 mt-1"
                  >
                    <span
                      class="material-symbols-outlined text-[13px]"
                      aria-hidden="true">check_circle</span
                    >
                    Active
                  </span>
                {:else if detailPreviewing}
                  <span
                    class="inline-flex items-center gap-1 text-[10px] text-accent-secondary-start font-label-sm-bold uppercase tracking-wider flex-shrink-0 mt-1"
                  >
                    <span
                      class="material-symbols-outlined text-[13px]"
                      aria-hidden="true">visibility</span
                    >
                    Preview
                  </span>
                {/if}
              </div>

              {#if detailSwatches.length > 0}
                <div class="flex gap-1.5 mb-3">
                  {#each detailSwatches as c (c)}
                    <span
                      aria-hidden="true"
                      class="h-9 flex-1 rounded-md border border-surface-panel-border"
                      style="background-color: {c}"
                    ></span>
                  {/each}
                </div>
              {/if}

              {#if detailTheme.description}
                <p
                  class="text-text-muted text-[12px] font-body-md mb-3 leading-relaxed"
                >
                  {detailTheme.description}
                </p>
              {/if}

              <dl class="space-y-1.5 text-[12px] font-body-md">
                {#if detailTheme.author}
                  <div class="flex gap-2">
                    <dt class="text-text-muted w-20 flex-shrink-0">Author</dt>
                    <dd class="text-text-primary min-w-0 break-words">
                      {detailTheme.author}
                    </dd>
                  </div>
                {/if}
                <div class="flex gap-2">
                  <dt class="text-text-muted w-20 flex-shrink-0">Source</dt>
                  <dd class="text-text-primary">
                    {sourceLabel(detailTheme.source)}
                  </dd>
                </div>
                {#if detailTypography.length > 0}
                  <div class="flex gap-2">
                    <dt class="text-text-muted w-20 flex-shrink-0">Fonts</dt>
                    <dd class="text-text-primary min-w-0">
                      {#each detailTypography as o, idx (o.label)}
                        {#if idx > 0}<span class="text-text-muted">
                            ·
                          </span>{/if}
                        <span class="text-text-muted">{o.label}:</span>
                        {displayFamilyName(o.value)}
                      {/each}
                    </dd>
                  </div>
                {/if}
              </dl>

              {#if previewTheme === null}
                <p
                  class="text-text-muted text-[11px] font-label-sm mt-4 pt-3 border-t border-surface-panel-border"
                >
                  Click a theme to preview it. Double-click or Apply to keep it.
                </p>
              {/if}
            {:else}
              <p class="text-text-muted text-[12px] font-body-md">
                Select a theme to see its details.
              </p>
            {/if}
          </aside>
        </div>
      {/if}
    </div>
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
        <span class="material-symbols-outlined text-[18px]" aria-hidden="true"
          >upload</span
        >
        Import .json
      </button>
      <button
        type="button"
        onclick={handleExport}
        disabled={!themeState.id}
        class="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-surface-panel border border-surface-panel-border text-text-primary font-label-sm-bold hover:border-accent-primary-start motion-reduce:transition-none transition-colors cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 disabled:opacity-40 disabled:cursor-not-allowed"
      >
        <span class="material-symbols-outlined text-[18px]" aria-hidden="true"
          >download</span
        >
        Export active
      </button>
    </div>
    <p class="text-text-muted text-[11px] font-label-sm mt-2">
      You can also drag and drop a .json theme file onto the grid above.
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
        <span
          class="material-symbols-outlined text-[16px] flex-shrink-0"
          aria-hidden="true"
        >
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

  /* Active drag visual for the theme drop target. Wails toggles
     .file-drop-target-active on #theme-file-drop-target while OS files are
     dragged over it; targeted via :global because Wails adds the class at
     runtime (a scoped selector wouldn't catch it). Inset ring + tint — no
     border, so there's no layout shift when the affordance appears. */
  :global(#theme-file-drop-target.file-drop-target-active) {
    background-color: color-mix(
      in srgb,
      var(--color-accent-primary-start) 10%,
      transparent
    );
    box-shadow: inset 0 0 0 2px var(--color-accent-primary-start);
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
