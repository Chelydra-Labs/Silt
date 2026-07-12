<script lang="ts">
  // Full-width Theme Editor shell (docs/theme-v2-ux.md). Progressive
  // disclosure: Simple (~5 controls) by default; Advanced left rail by intent.
  // Working copy + FE flatten → injectTokens; contrast warns never block save.
  import { onMount } from 'svelte'
  import {
    clearEditorStaging,
    getThemeJSON,
    pickImageFile,
    prepareBackgroundAsset,
    refreshActiveTheme,
    saveCustomTheme,
    setStatus
  } from '../store.svelte'
  import {
    autoFixLightness,
    classifyContrast,
    contrastRatioWCAG,
    coreContrastPairs,
    effectiveBackgroundWithScrim,
    type ContrastPair
  } from '../contrast'
  import { flattenTheme } from '../flatten'
  import { FONT_REGISTRY, displayFamilyName, type FontEntry } from '../fonts'
  import {
    SURFACE_PARENT,
    SURFACE_ZONES,
    type AdvancedGroup,
    type Background,
    type Surface,
    type SurfaceZone
  } from '../types'
  import { concreteEditorDefaults } from './concreteEditorDefaults'
  import { createWorkingCopy } from './workingCopy.svelte'
  import OklchColorField from './OklchColorField.svelte'
  import ContrastBadge from './ContrastBadge.svelte'

  type Props = {
    themeId: string
    /** When false (built-in/bundled seed), Save always forks as a new theme. */
    sourceIsDisk: boolean
    onClose: () => void
    onSaved?: (id: string) => void
    /** Test seam: inject seed JSON instead of GetThemeJSON. */
    injectJson?: string | null
    getThemeJSONFn?: (id: string) => Promise<string>
    saveCustomThemeFn?: typeof saveCustomTheme
  }

  let {
    themeId,
    sourceIsDisk,
    onClose,
    onSaved,
    injectJson = null,
    getThemeJSONFn = getThemeJSON,
    saveCustomThemeFn = saveCustomTheme
  }: Props = $props()

  const wc = createWorkingCopy()
  let saving = $state(false)
  let saveError = $state<string | null>(null)
  let surfaceZone = $state<SurfaceZone>('app')
  let bgZone = $state<SurfaceZone>('app')
  let pickingBg = $state(false)
  // Generation counter: ignore stale bootstrap/save results after unmount/remount.
  let gen = 0

  // Debounced contrast summary for aria-live (not per slider tick).
  let liveContrastMsg = $state('')
  let contrastTimer: ReturnType<typeof setTimeout> | null = null

  const activeTabId = $derived(
    !wc.showAdvanced
      ? 'theme-editor-tab-simple'
      : `theme-editor-tab-${wc.advancedGroup}`
  )

  const ADVANCED: { id: AdvancedGroup; label: string; icon: string }[] = [
    { id: 'surfaces', label: 'Surfaces', icon: 'layers' },
    { id: 'color', label: 'Color & accent', icon: 'palette' },
    { id: 'typography', label: 'Typography', icon: 'text_fields' },
    { id: 'geometry', label: 'Geometry', icon: 'rounded_corner' },
    { id: 'editor', label: 'Editor', icon: 'edit_note' },
    { id: 'background', label: 'Background', icon: 'image' }
  ]

  const bodyFonts = $derived(
    FONT_REGISTRY.filter(
      (f) =>
        f.category === 'sans' ||
        f.category === 'serif' ||
        f.category === 'display' ||
        f.source === 'system'
    )
  )

  const themeName = $derived(wc.draft?.name ?? themeId)

  const previewTokens = $derived.by(() => {
    if (!wc.draft) return {} as Record<string, string>
    return flattenTheme(wc.draft, wc.editMode)
  })

  const contrastPairs = $derived(coreContrastPairs(previewTokens))
  const belowAA = $derived(
    contrastPairs.filter((p) => p.level === 'fail' || p.level === 'warn')
  )

  $effect(() => {
    const n = belowAA.length
    const msg =
      n === 0
        ? 'All core contrast pairs meet AA'
        : `${n} pair${n === 1 ? '' : 's'} below AA — review`
    if (contrastTimer !== null) clearTimeout(contrastTimer)
    contrastTimer = setTimeout(() => {
      liveContrastMsg = msg
      contrastTimer = null
    }, 280)
  })

  onMount(() => {
    const my = ++gen
    void bootstrap(my)
    return () => {
      gen++
      if (contrastTimer !== null) clearTimeout(contrastTimer)
      wc.discard()
    }
  })

  async function bootstrap(my: number) {
    wc.loading = true
    saveError = null
    // Best-effort once per mount; openEditor also clears before mount.
    void clearEditorStaging().catch(() => {})
    try {
      if (injectJson != null) {
        wc.loadFromJson(injectJson)
      } else {
        const json = await getThemeJSONFn(themeId)
        if (my !== gen) return
        wc.loadFromJson(json)
      }
      if (my !== gen) return
      if (wc.draft) wc.previewNow()
    } catch (err) {
      if (my !== gen) return
      // IPC/network failures must not be rewritten as a parse error.
      const msg = err instanceof Error ? err.message : String(err)
      wc.setLoadError(msg)
      setStatus({
        kind: 'error',
        message: `Failed to load theme: ${msg}`,
        fields: []
      })
    } finally {
      if (my === gen) wc.loading = false
    }
  }

  function requestClose() {
    if (saving) return
    if (wc.dirty) {
      const ok = window.confirm(
        'Leave without saving? Unsaved changes will be lost.'
      )
      if (!ok) return
    }
    wc.discard()
    onClose()
  }

  function onRevert() {
    if (!wc.dirty) return
    const ok = window.confirm('Discard all unsaved changes?')
    if (!ok) return
    wc.resetAll()
    wc.previewNow()
  }

  async function onSave(asNew: boolean) {
    if (!wc.draft) return
    const my = gen
    const mustFork = !sourceIsDisk || asNew
    let name = wc.draft.name
    if (mustFork) {
      const entered = window.prompt(
        'Name for the new theme',
        `${wc.draft.name} (custom)`
      )
      if (entered === null) return
      name = entered.trim() || `${wc.draft.name} (custom)`
    }
    saving = true
    saveError = null
    try {
      const res = await saveCustomThemeFn({
        json: JSON.stringify(wc.draft),
        overwrite: !mustFork,
        apply: true,
        name
      })
      if (my !== gen) return
      if (!res) throw new Error('Save returned no result')
      const id = res.info?.id ?? themeId
      // Save succeeded — reseed + refresh store tokens so unmount restore
      // does not inject pre-save maps. Reseed failures are non-fatal.
      try {
        const fresh = await getThemeJSONFn(id)
        if (my !== gen) return
        wc.loadFromJson(fresh)
        wc.previewNow()
        await refreshActiveTheme()
        if (my !== gen) return
        setStatus({
          kind: 'success',
          message: `Saved theme "${res.info?.name ?? name}".`,
          fields: []
        })
      } catch {
        if (my !== gen) return
        setStatus({
          kind: 'info',
          message: 'Saved, but could not refresh editor — re-open to continue.',
          fields: []
        })
      }
      if (my !== gen) return
      onSaved?.(id)
    } catch (err) {
      if (my !== gen) return
      const msg = err instanceof Error ? err.message : String(err)
      saveError = msg
      setStatus({
        kind: 'error',
        message: `Failed to save theme: ${msg}`,
        fields: []
      })
    } finally {
      if (my === gen) saving = false
    }
  }

  function mode(): 'dark' | 'light' {
    return wc.editMode
  }

  function appBg(): string {
    return wc.draft?.modes[mode()].surfaces.app.bg ?? '#000'
  }

  function setAppBg(v: string) {
    wc.setColor(wc.modePath('surfaces.app.bg'), v)
  }
  function setAppText(v: string) {
    wc.setColor(wc.modePath('surfaces.app.text'), v)
  }
  function setAccentStart(v: string) {
    // Capture before start write — after setColor, draft is replaced and
    // end === start would always be true if we compared post-write values.
    const m = wc.draft?.modes[mode()]
    const prevStart = m?.accent.primary.start
    const prevEnd = m?.accent.primary.end
    wc.setColor(wc.modePath('accent.primary.start'), v)
    if (m && prevEnd === prevStart) {
      wc.setColor(wc.modePath('accent.primary.end'), v)
    }
  }

  function setBodyFont(v: string) {
    if (!wc.draft) return
    const trimmed = v.trim()
    // Empty → delete key so unset matches advanced font selects.
    wc.setAt('typography.font_family', trimmed || undefined)
  }

  function setRadiusMd(v: string) {
    if (!wc.draft) return
    const m = wc.draft.modes[mode()]
    const radius = {
      sm: m.radius?.sm ?? '4px',
      md: v,
      lg: m.radius?.lg ?? '12px',
      xl: m.radius?.xl ?? '16px',
      full: m.radius?.full ?? '9999px'
    }
    wc.setAt(wc.modePath('radius'), radius)
  }

  function ensureSurface(zone: SurfaceZone): Surface {
    if (!wc.draft) {
      return { bg: '#000', border: '#000', text: '#fff' }
    }
    const m = wc.draft.modes[mode()]
    if (zone === 'app') return m.surfaces.app
    const existing = m.surfaces[zone]
    if (existing) return existing
    // Seed from parent so the zone becomes concrete without a visual jump.
    const parent = SURFACE_PARENT[zone] || 'app'
    const pSurf =
      parent === 'app'
        ? m.surfaces.app
        : (m.surfaces[parent as SurfaceZone] ?? m.surfaces.app)
    return {
      bg: pSurf.bg,
      border: pSurf.border,
      text: pSurf.text
    }
  }

  function updateSurface(zone: SurfaceZone, patch: Partial<Surface>) {
    if (!wc.draft) return
    const base = ensureSurface(zone)
    const next = { ...base, ...patch }
    if (zone === 'app') {
      wc.setAt(wc.modePath('surfaces.app'), next)
    } else {
      wc.setAt(wc.modePath(`surfaces.${zone}`), next)
    }
  }

  function surfaceField(zone: SurfaceZone): Surface {
    return ensureSurface(zone)
  }

  async function pickBackground() {
    if (!wc.draft) return
    pickingBg = true
    try {
      const path = await pickImageFile()
      if (!path) return
      const res = await prepareBackgroundAsset(path)
      if (!res?.reference) return
      const zone = bgZone
      const base = ensureSurface(zone)
      const bg: Background = {
        ...(base.background ?? {}),
        image: res.reference,
        size: base.background?.size ?? 'cover',
        opacity: base.background?.opacity ?? 0.35,
        blend: base.background?.blend ?? 'normal',
        position: base.background?.position ?? 'center',
        scrim: base.background?.scrim ?? base.bg
      }
      updateSurface(zone, { background: bg })
    } catch (err) {
      setStatus({
        kind: 'error',
        message: `Background image failed: ${err instanceof Error ? err.message : String(err)}`,
        fields: []
      })
    } finally {
      pickingBg = false
    }
  }

  function updateBackground(patch: Partial<Background>) {
    const base = ensureSurface(bgZone)
    const bg: Background = { ...(base.background ?? {}), ...patch }
    updateSurface(bgZone, { background: bg })
  }

  function clearBackground() {
    if (!wc.draft) return
    const zone = bgZone
    const surf = ensureSurface(zone)
    // Single write that omits the background key entirely.
    const next: Surface = {
      bg: surf.bg,
      border: surf.border,
      text: surf.text,
      ...(surf.text_muted ? { text_muted: surf.text_muted } : {}),
      ...(surf.text_disabled ? { text_disabled: surf.text_disabled } : {})
    }
    if (zone === 'app') wc.setAt(wc.modePath('surfaces.app'), next)
    else wc.setAt(wc.modePath(`surfaces.${zone}`), next)
  }

  function fixPair(pair: ContrastPair) {
    if (!pair.fg || !pair.bg) return
    const fixed = autoFixLightness(pair.fg, pair.bg, 4.5)
    if (!fixed) return
    // Editor zone may be inherited — materialize then set text.
    if (pair.id === 'editor-text') {
      updateSurface('editor', { text: fixed })
      return
    }
    const paths: Record<string, string> = {
      'app-text': wc.modePath('surfaces.app.text'),
      'muted-text': wc.modePath('text_muted'),
      accent: wc.modePath('accent.primary.start'),
      error: wc.modePath('error.fg')
    }
    const path = paths[pair.id]
    if (path) wc.setColor(path, fixed)
  }

  function onWindowKey(e: KeyboardEvent) {
    if (e.key !== 'Escape') return
    // Escape in a form field blurs/cancels the field — not the whole editor.
    const t = e.target
    if (t instanceof HTMLElement) {
      const tag = t.tagName
      if (
        tag === 'INPUT' ||
        tag === 'TEXTAREA' ||
        tag === 'SELECT' ||
        t.isContentEditable
      ) {
        return
      }
      if (t.closest('[data-focus-trap], [role="dialog"], [role="listbox"]')) {
        return
      }
    }
    requestClose()
  }

  function selectAdvanced(id: AdvancedGroup) {
    wc.showAdvanced = true
    wc.advancedGroup = id
  }

  function selectSimple() {
    wc.showAdvanced = false
  }

  function typographyValue(key: string): string {
    const t = wc.draft?.typography
    if (!t) return ''
    if (key === 'font_family') return t.font_family ?? ''
    if (key === 'mono_font_family') return t.mono_font_family ?? ''
    if (key === 'headline_font') return t.headline_font ?? ''
    return ''
  }

  function scaleValue(
    kind: 'size' | 'line_height' | 'weight',
    step: string
  ): string {
    return wc.draft?.typography?.scale?.[kind]?.[step] ?? ''
  }

  function setScaleValue(
    kind: 'size' | 'line_height' | 'weight',
    step: string,
    value: string
  ) {
    if (!wc.draft) return
    const scale = { ...(wc.draft.typography?.scale ?? {}) }
    const bucket = { ...(scale[kind] ?? {}) }
    const trimmed = value.trim()
    if (trimmed) bucket[step] = trimmed
    else delete bucket[step]
    scale[kind] = bucket
    wc.setAt('typography.scale', scale)
  }

  function resetGroupPaths(paths: string[]) {
    wc.resetGroup(paths)
  }

  function colorGroupPaths(): string[] {
    const p = (rel: string) => wc.modePath(rel)
    return [
      p('accent.primary.start'),
      p('accent.primary.end'),
      p('accent.primary.glow'),
      p('accent.secondary.start'),
      p('accent.secondary.end'),
      p('accent.secondary.glow'),
      p('hover'),
      p('active'),
      p('border_active'),
      p('border_focus'),
      p('text_muted'),
      p('text_disabled'),
      p('status.warn'),
      p('status.danger'),
      p('status.success'),
      p('error.fg'),
      p('error.bg'),
      p('error.border')
    ]
  }

  function surfacesGroupPaths(): string[] {
    return [wc.modePath(`surfaces.${surfaceZone}`)]
  }

  function geometryGroupPaths(): string[] {
    return [
      wc.modePath('radius'),
      wc.modePath('spacing'),
      wc.modePath('shadow')
    ]
  }

  function typographyGroupPaths(): string[] {
    return [
      'typography.font_family',
      'typography.mono_font_family',
      'typography.headline_font',
      'typography.scale'
    ]
  }

  function editorGroupPaths(): string[] {
    return [wc.modePath('editor')]
  }

  function backgroundGroupPaths(): string[] {
    return [wc.modePath(`surfaces.${bgZone}.background`)]
  }

  const surfaceEdit = $derived(surfaceField(surfaceZone))
  const bgEdit = $derived(ensureSurface(bgZone).background)
  const bgZoneSurf = $derived(ensureSurface(bgZone))
  const bgEffective = $derived(
    effectiveBackgroundWithScrim(
      bgEdit?.image,
      bgEdit?.scrim,
      bgEdit?.opacity,
      bgZoneSurf.bg
    )
  )
  const bgTextContrastRatio = $derived(
    contrastRatioWCAG(bgZoneSurf.text, bgEffective)
  )
  const bgTextContrastLevel = $derived(
    classifyContrast(bgTextContrastRatio, true)
  )
</script>

<svelte:window onkeydown={onWindowKey} />

<div
  class="theme-editor flex flex-col h-full min-h-0 bg-surface-app text-surface-app-text"
  role="region"
  aria-label="Theme editor"
>
  <!-- Sticky header -->
  <header
    class="sticky top-0 z-20 flex flex-wrap items-center gap-3 px-4 py-3 border-b border-surface-panel-border bg-surface-panel/95 backdrop-blur-sm"
  >
    <button
      type="button"
      onclick={requestClose}
      disabled={saving}
      class="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-text-muted hover:text-text-primary hover:bg-hover border-none bg-transparent cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 font-label-sm disabled:opacity-40 disabled:cursor-not-allowed"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >arrow_back</span
      >
      Appearance
    </button>

    <h2
      class="text-type-md font-headline-md text-text-primary truncate min-w-0"
    >
      Theme Editor · {themeName}
    </h2>

    <div class="flex-1"></div>

    <!-- Dark / Light edit-mode toggle (which map is edited, not app mode) -->
    <div
      role="radiogroup"
      aria-label="Edit color mode"
      class="inline-flex bg-surface-app border border-surface-panel-border rounded-lg p-0.5 gap-0.5"
    >
      {#each [{ id: 'dark' as const, label: 'Dark', icon: 'dark_mode' }, { id: 'light' as const, label: 'Light', icon: 'light_mode' }] as m (m.id)}
        {@const active = wc.editMode === m.id}
        <button
          type="button"
          role="radio"
          aria-checked={active}
          onclick={() => {
            wc.setEditMode(m.id)
            wc.previewNow()
          }}
          class="inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-type-xs font-label-sm border-none cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
          class:bg-hover={active}
          class:text-accent-primary-start={active}
          class:text-text-muted={!active}
        >
          <span
            class="material-symbols-outlined text-icon-sm"
            aria-hidden="true">{m.icon}</span
          >
          {m.label}
        </button>
      {/each}
    </div>

    <button
      type="button"
      onclick={onRevert}
      disabled={!wc.dirty || saving}
      class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-surface-app border border-surface-panel-border text-text-primary font-label-sm-bold hover:border-border-active cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 disabled:opacity-40 disabled:cursor-not-allowed"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >undo</span
      >
      Revert
    </button>

    {#if sourceIsDisk}
      <button
        type="button"
        onclick={() => onSave(true)}
        disabled={!wc.draft || saving}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-surface-app border border-surface-panel-border text-text-primary font-label-sm-bold hover:border-border-active cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 disabled:opacity-40 disabled:cursor-not-allowed"
      >
        Save as new
      </button>
      <button
        type="button"
        onclick={() => onSave(false)}
        disabled={!wc.draft || saving}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-accent-primary-start/20 border border-accent-primary-start/50 text-accent-primary-start font-label-sm-bold hover:brightness-110 cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 disabled:opacity-40 disabled:cursor-not-allowed"
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >save</span
        >
        {saving ? 'Saving…' : 'Save'}
      </button>
    {:else}
      <button
        type="button"
        onclick={() => onSave(true)}
        disabled={!wc.draft || saving}
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-accent-primary-start/20 border border-accent-primary-start/50 text-accent-primary-start font-label-sm-bold hover:brightness-110 cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 disabled:opacity-40 disabled:cursor-not-allowed"
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >save</span
        >
        {saving ? 'Saving…' : 'Save as new theme'}
      </button>
    {/if}
  </header>

  {#if wc.loading}
    <div
      class="flex-1 flex items-center justify-center text-text-muted p-8"
      role="status"
      aria-live="polite"
    >
      Loading theme…
    </div>
  {:else if wc.loadError && !wc.draft}
    <div class="flex-1 p-6" role="alert">
      <div
        class="rounded-lg border border-error-border bg-error-bg text-error px-4 py-3"
      >
        Failed to load theme: {wc.loadError}
      </div>
      <button
        type="button"
        class="mt-4 text-accent-primary-start font-label-sm-bold underline bg-transparent border-none cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed"
        disabled={saving}
        onclick={requestClose}>Back to Appearance</button
      >
    </div>
  {:else if wc.draft}
    <div class="flex flex-col flex-1 min-h-0">
      <!-- Top section tabs (not a third left rail — immersive full-width). -->
      <div
        class="flex-shrink-0 border-b border-surface-panel-border bg-surface-panel/30 px-4 overflow-x-auto"
      >
        <div
          role="tablist"
          aria-label="Theme editor sections"
          class="flex items-center gap-1 min-w-min py-2"
        >
          <button
            type="button"
            id="theme-editor-tab-simple"
            role="tab"
            aria-selected={!wc.showAdvanced}
            aria-controls="theme-editor-panel"
            onclick={selectSimple}
            class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-type-sm font-label-sm border-none cursor-pointer whitespace-nowrap focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
            class:bg-hover={!wc.showAdvanced}
            class:text-accent-primary-start={!wc.showAdvanced}
            class:text-text-muted={wc.showAdvanced}
            class:bg-transparent={wc.showAdvanced}
          >
            <span
              class="material-symbols-outlined text-icon-md"
              aria-hidden="true">tune</span
            >
            Simple
          </button>
          {#each ADVANCED as g (g.id)}
            <button
              type="button"
              id={`theme-editor-tab-${g.id}`}
              role="tab"
              aria-selected={wc.showAdvanced && wc.advancedGroup === g.id}
              aria-controls="theme-editor-panel"
              onclick={() => selectAdvanced(g.id)}
              class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-type-sm font-label-sm border-none cursor-pointer whitespace-nowrap focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
              class:bg-hover={wc.showAdvanced && wc.advancedGroup === g.id}
              class:text-accent-primary-start={wc.showAdvanced &&
                wc.advancedGroup === g.id}
              class:text-text-muted={!(
                wc.showAdvanced && wc.advancedGroup === g.id
              )}
              class:bg-transparent={!(
                wc.showAdvanced && wc.advancedGroup === g.id
              )}
            >
              <span
                class="material-symbols-outlined text-icon-md"
                aria-hidden="true">{g.icon}</span
              >
              {g.label}
            </button>
          {/each}
        </div>
      </div>

      <!-- Main controls (div+tabpanel: main cannot host interactive roles). -->
      <div
        id="theme-editor-panel"
        role="tabpanel"
        aria-labelledby={activeTabId}
        class="flex-1 min-w-0 overflow-y-auto p-6 pb-28"
      >
        {#if !wc.showAdvanced}
          <!-- Simple: calm, sparse -->
          <div class="max-w-xl space-y-8">
            <div>
              <h3 class="text-type-lg font-headline-md text-text-primary mb-1">
                Essentials
              </h3>
              <p class="text-text-muted text-type-sm font-body-md">
                Five controls cover most looks. Open Advanced for full surfaces,
                status colors, and backgrounds.
              </p>
            </div>

            <OklchColorField
              label="App background"
              value={wc.draft.modes[mode()].surfaces.app.bg}
              onchange={setAppBg}
              onReset={() => wc.resetPath(wc.modePath('surfaces.app.bg'))}
            />

            <OklchColorField
              label="App text"
              value={wc.draft.modes[mode()].surfaces.app.text}
              bgForContrast={appBg()}
              onchange={setAppText}
              onReset={() => wc.resetPath(wc.modePath('surfaces.app.text'))}
            />

            <OklchColorField
              label="Accent"
              value={wc.draft.modes[mode()].accent.primary.start}
              bgForContrast={appBg()}
              onchange={setAccentStart}
              onReset={() => wc.resetPath(wc.modePath('accent.primary.start'))}
            />

            <div class="flex flex-col gap-1.5">
              <label
                class="text-type-sm font-label-sm text-text-primary"
                for="theme-body-font">Body font</label
              >
              <select
                id="theme-body-font"
                class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
                value={wc.draft.typography?.font_family ?? ''}
                onchange={(e) =>
                  setBodyFont((e.currentTarget as HTMLSelectElement).value)}
              >
                <option value="">— Theme default stack —</option>
                {#each bodyFonts as f (f.id)}
                  <option value={f.cssFamily}>{f.displayName}</option>
                {/each}
              </select>
              {#if wc.draft.typography?.font_family}
                <p class="text-type-xs text-text-muted font-label-sm">
                  {displayFamilyName(wc.draft.typography.font_family)}
                </p>
              {/if}
            </div>

            <div class="flex flex-col gap-1.5">
              <label
                class="text-type-sm font-label-sm text-text-primary"
                for="theme-radius-md">Corner radius</label
              >
              <div class="flex items-center gap-3">
                <input
                  id="theme-radius-md"
                  type="text"
                  class="h-10 w-28 px-3 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary font-mono text-type-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
                  value={wc.draft.modes[mode()].radius?.md ?? '8px'}
                  onchange={(e) =>
                    setRadiusMd((e.currentTarget as HTMLInputElement).value)}
                />
                <span
                  class="inline-block w-10 h-10 border-2 border-accent-primary-start bg-surface-panel"
                  style="border-radius: {wc.draft.modes[mode()].radius?.md ??
                    '8px'}"
                  aria-hidden="true"
                ></span>
              </div>
            </div>
          </div>
        {:else if wc.advancedGroup === 'surfaces'}
          <div class="max-w-2xl space-y-6">
            <div class="flex items-start justify-between gap-3">
              <div>
                <h3 class="text-type-lg font-headline-md mb-1">Surfaces</h3>
                <p class="text-text-muted text-type-sm">
                  Per-zone background, border, and text. Unset zones inherit
                  from their parent.
                </p>
              </div>
              <button
                type="button"
                class="flex-shrink-0 text-type-xs font-label-sm text-text-muted hover:text-text-primary underline-offset-2 hover:underline bg-transparent border-none cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 rounded-sm"
                onclick={() => resetGroupPaths(surfacesGroupPaths())}
              >
                Reset group
              </button>
            </div>
            <div class="flex flex-col gap-1.5">
              <label class="text-type-sm font-label-sm" for="surface-zone"
                >Zone</label
              >
              <select
                id="surface-zone"
                class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 max-w-xs"
                bind:value={surfaceZone}
              >
                {#each SURFACE_ZONES as z (z)}
                  <option value={z}>{z}</option>
                {/each}
              </select>
              {#if surfaceZone !== 'app'}
                <p class="text-type-xs text-text-muted">
                  Overrides inherited {SURFACE_PARENT[surfaceZone] || 'app'}
                </p>
              {/if}
            </div>
            <OklchColorField
              label="Background"
              value={surfaceEdit.bg}
              onchange={(v) => updateSurface(surfaceZone, { bg: v })}
              onReset={() =>
                wc.resetPath(wc.modePath(`surfaces.${surfaceZone}.bg`))}
            />
            <OklchColorField
              label="Border"
              value={surfaceEdit.border}
              onchange={(v) => updateSurface(surfaceZone, { border: v })}
              onReset={() =>
                wc.resetPath(wc.modePath(`surfaces.${surfaceZone}.border`))}
            />
            <OklchColorField
              label="Text"
              value={surfaceEdit.text}
              bgForContrast={surfaceEdit.bg}
              onchange={(v) => updateSurface(surfaceZone, { text: v })}
              onReset={() =>
                wc.resetPath(wc.modePath(`surfaces.${surfaceZone}.text`))}
            />
          </div>
        {:else if wc.advancedGroup === 'color'}
          <div class="max-w-2xl space-y-6">
            <div class="flex items-start justify-between gap-3">
              <div>
                <h3 class="text-type-lg font-headline-md mb-1">
                  Color & accent
                </h3>
                <p class="text-text-muted text-type-sm">
                  Accents, status, error, and interaction tokens.
                </p>
              </div>
              <button
                type="button"
                class="flex-shrink-0 text-type-xs font-label-sm text-text-muted hover:text-text-primary underline-offset-2 hover:underline bg-transparent border-none cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 rounded-sm"
                onclick={() => resetGroupPaths(colorGroupPaths())}
              >
                Reset group
              </button>
            </div>
            <OklchColorField
              label="Primary accent start"
              value={wc.draft.modes[mode()].accent.primary.start}
              bgForContrast={appBg()}
              onchange={(v) =>
                wc.setColor(wc.modePath('accent.primary.start'), v)}
              onReset={() => wc.resetPath(wc.modePath('accent.primary.start'))}
            />
            <OklchColorField
              label="Primary accent end"
              value={wc.draft.modes[mode()].accent.primary.end}
              onchange={(v) =>
                wc.setColor(wc.modePath('accent.primary.end'), v)}
              onReset={() => wc.resetPath(wc.modePath('accent.primary.end'))}
            />
            <OklchColorField
              label="Primary glow"
              value={wc.draft.modes[mode()].accent.primary.glow}
              onchange={(v) =>
                wc.setColor(wc.modePath('accent.primary.glow'), v)}
              onReset={() => wc.resetPath(wc.modePath('accent.primary.glow'))}
            />
            <OklchColorField
              label="Secondary start"
              value={wc.draft.modes[mode()].accent.secondary.start}
              onchange={(v) =>
                wc.setColor(wc.modePath('accent.secondary.start'), v)}
              onReset={() =>
                wc.resetPath(wc.modePath('accent.secondary.start'))}
            />
            <OklchColorField
              label="Secondary end"
              value={wc.draft.modes[mode()].accent.secondary.end}
              onchange={(v) =>
                wc.setColor(wc.modePath('accent.secondary.end'), v)}
              onReset={() => wc.resetPath(wc.modePath('accent.secondary.end'))}
            />
            <OklchColorField
              label="Secondary glow"
              value={wc.draft.modes[mode()].accent.secondary.glow}
              onchange={(v) =>
                wc.setColor(wc.modePath('accent.secondary.glow'), v)}
              onReset={() => wc.resetPath(wc.modePath('accent.secondary.glow'))}
            />
            <OklchColorField
              label="Hover"
              value={wc.draft.modes[mode()].hover}
              onchange={(v) => wc.setColor(wc.modePath('hover'), v)}
              onReset={() => wc.resetPath(wc.modePath('hover'))}
            />
            <OklchColorField
              label="Active"
              value={wc.draft.modes[mode()].active}
              onchange={(v) => wc.setColor(wc.modePath('active'), v)}
              onReset={() => wc.resetPath(wc.modePath('active'))}
            />
            <OklchColorField
              label="Border active"
              value={wc.draft.modes[mode()].border_active}
              onchange={(v) => wc.setColor(wc.modePath('border_active'), v)}
              onReset={() => wc.resetPath(wc.modePath('border_active'))}
            />
            <OklchColorField
              label="Border focus"
              value={wc.draft.modes[mode()].border_focus}
              onchange={(v) => wc.setColor(wc.modePath('border_focus'), v)}
              onReset={() => wc.resetPath(wc.modePath('border_focus'))}
            />
            <OklchColorField
              label="Text muted"
              value={wc.draft.modes[mode()].text_muted}
              bgForContrast={appBg()}
              onchange={(v) => wc.setColor(wc.modePath('text_muted'), v)}
              onReset={() => wc.resetPath(wc.modePath('text_muted'))}
            />
            <OklchColorField
              label="Text disabled"
              value={wc.draft.modes[mode()].text_disabled}
              bgForContrast={appBg()}
              onchange={(v) => wc.setColor(wc.modePath('text_disabled'), v)}
              onReset={() => wc.resetPath(wc.modePath('text_disabled'))}
            />
            <OklchColorField
              label="Status warn"
              value={wc.draft.modes[mode()].status.warn}
              onchange={(v) => wc.setColor(wc.modePath('status.warn'), v)}
              onReset={() => wc.resetPath(wc.modePath('status.warn'))}
            />
            <OklchColorField
              label="Status danger"
              value={wc.draft.modes[mode()].status.danger}
              onchange={(v) => wc.setColor(wc.modePath('status.danger'), v)}
              onReset={() => wc.resetPath(wc.modePath('status.danger'))}
            />
            <OklchColorField
              label="Status success"
              value={wc.draft.modes[mode()].status.success}
              onchange={(v) => wc.setColor(wc.modePath('status.success'), v)}
              onReset={() => wc.resetPath(wc.modePath('status.success'))}
            />
            <OklchColorField
              label="Error foreground"
              value={wc.draft.modes[mode()].error.fg}
              bgForContrast={wc.draft.modes[mode()].error.bg}
              onchange={(v) => wc.setColor(wc.modePath('error.fg'), v)}
              onReset={() => wc.resetPath(wc.modePath('error.fg'))}
            />
            <OklchColorField
              label="Error background"
              value={wc.draft.modes[mode()].error.bg}
              onchange={(v) => wc.setColor(wc.modePath('error.bg'), v)}
              onReset={() => wc.resetPath(wc.modePath('error.bg'))}
            />
            <OklchColorField
              label="Error border"
              value={wc.draft.modes[mode()].error.border}
              onchange={(v) => wc.setColor(wc.modePath('error.border'), v)}
              onReset={() => wc.resetPath(wc.modePath('error.border'))}
            />
          </div>
        {:else if wc.advancedGroup === 'typography'}
          <div class="max-w-xl space-y-6">
            <div class="flex items-start justify-between gap-3">
              <div>
                <h3 class="text-type-lg font-headline-md mb-1">Typography</h3>
                <p class="text-text-muted text-type-sm">
                  Theme-level font families and optional type scale (shared
                  across dark and light).
                </p>
              </div>
              <button
                type="button"
                class="flex-shrink-0 text-type-xs font-label-sm text-text-muted hover:text-text-primary underline-offset-2 hover:underline bg-transparent border-none cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 rounded-sm"
                onclick={() => resetGroupPaths(typographyGroupPaths())}
              >
                Reset group
              </button>
            </div>
            {#each [{ key: 'font_family', label: 'Body', cat: 'body' as const }, { key: 'mono_font_family', label: 'Mono', cat: 'mono' as const }, { key: 'headline_font', label: 'Headline', cat: 'display' as const }] as slot (slot.key)}
              <div class="flex flex-col gap-1.5">
                <label
                  class="text-type-sm font-label-sm"
                  for={`typo-${slot.key}`}>{slot.label}</label
                >
                <select
                  id={`typo-${slot.key}`}
                  class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
                  value={typographyValue(slot.key)}
                  onchange={(e) => {
                    const v = (e.currentTarget as HTMLSelectElement).value
                    wc.setAt(`typography.${slot.key}`, v || undefined)
                  }}
                >
                  <option value="">— Unset —</option>
                  {#each FONT_REGISTRY.filter( (f: FontEntry) => (slot.cat === 'mono' ? f.category === 'mono' : slot.cat === 'display' ? f.category === 'display' || f.category === 'sans' : f.category !== 'mono') ) as f (f.id)}
                    <option value={f.cssFamily}>{f.displayName}</option>
                  {/each}
                </select>
              </div>
            {/each}

            <div class="pt-2 border-t border-surface-panel-border space-y-4">
              <h4 class="text-type-sm font-label-sm-bold text-text-primary">
                Type scale (optional)
              </h4>
              <div class="space-y-3">
                <p class="text-type-xs text-text-muted font-label-sm">Size</p>
                {#each ['xs', 'sm', 'base', 'lg', 'xl', '2xl'] as step (step)}
                  <div class="flex flex-col gap-1.5">
                    <label
                      class="text-type-sm font-label-sm"
                      for={`scale-size-${step}`}>Size {step}</label
                    >
                    <input
                      id={`scale-size-${step}`}
                      type="text"
                      class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border font-mono text-type-sm text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 max-w-xs"
                      value={scaleValue('size', step)}
                      placeholder="e.g. 0.875rem"
                      onchange={(e) =>
                        setScaleValue(
                          'size',
                          step,
                          (e.currentTarget as HTMLInputElement).value
                        )}
                    />
                  </div>
                {/each}
              </div>
              <div class="space-y-3">
                <p class="text-type-xs text-text-muted font-label-sm">
                  Line height
                </p>
                {#each ['tight', 'normal', 'relaxed'] as step (step)}
                  <div class="flex flex-col gap-1.5">
                    <label
                      class="text-type-sm font-label-sm"
                      for={`scale-lh-${step}`}>Line height {step}</label
                    >
                    <input
                      id={`scale-lh-${step}`}
                      type="text"
                      class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border font-mono text-type-sm text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 max-w-xs"
                      value={scaleValue('line_height', step)}
                      placeholder="e.g. 1.5"
                      onchange={(e) =>
                        setScaleValue(
                          'line_height',
                          step,
                          (e.currentTarget as HTMLInputElement).value
                        )}
                    />
                  </div>
                {/each}
              </div>
              <div class="space-y-3">
                <p class="text-type-xs text-text-muted font-label-sm">Weight</p>
                {#each ['normal', 'medium', 'semibold'] as step (step)}
                  <div class="flex flex-col gap-1.5">
                    <label
                      class="text-type-sm font-label-sm"
                      for={`scale-weight-${step}`}>Weight {step}</label
                    >
                    <input
                      id={`scale-weight-${step}`}
                      type="text"
                      class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border font-mono text-type-sm text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 max-w-xs"
                      value={scaleValue('weight', step)}
                      placeholder="e.g. 500"
                      onchange={(e) =>
                        setScaleValue(
                          'weight',
                          step,
                          (e.currentTarget as HTMLInputElement).value
                        )}
                    />
                  </div>
                {/each}
              </div>
            </div>
          </div>
        {:else if wc.advancedGroup === 'geometry'}
          <div class="max-w-xl space-y-6">
            <div class="flex items-start justify-between gap-3">
              <div>
                <h3 class="text-type-lg font-headline-md mb-1">Geometry</h3>
                <p class="text-text-muted text-type-sm">
                  Radius, spacing, and shadow ramps for this mode.
                </p>
              </div>
              <button
                type="button"
                class="flex-shrink-0 text-type-xs font-label-sm text-text-muted hover:text-text-primary underline-offset-2 hover:underline bg-transparent border-none cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 rounded-sm"
                onclick={() => resetGroupPaths(geometryGroupPaths())}
              >
                Reset group
              </button>
            </div>
            {#each ['sm', 'md', 'lg', 'xl', 'full'] as step (step)}
              <div class="flex flex-col gap-1.5">
                <label class="text-type-sm font-label-sm" for={`radius-${step}`}
                  >Radius {step}</label
                >
                <input
                  id={`radius-${step}`}
                  type="text"
                  class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border font-mono text-type-sm text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 max-w-xs"
                  value={wc.draft.modes[mode()].radius?.[
                    step as keyof typeof wc.draft.modes.dark.radius
                  ] ?? ''}
                  onchange={(e) => {
                    const m = wc.draft!.modes[mode()]
                    const radius = {
                      sm: m.radius?.sm ?? '4px',
                      md: m.radius?.md ?? '8px',
                      lg: m.radius?.lg ?? '12px',
                      xl: m.radius?.xl ?? '16px',
                      full: m.radius?.full ?? '9999px',
                      [step]: (e.currentTarget as HTMLInputElement).value
                    }
                    wc.setAt(wc.modePath('radius'), radius)
                  }}
                />
              </div>
            {/each}
            <div class="pt-2 border-t border-surface-panel-border space-y-4">
              <h4 class="text-type-sm font-label-sm-bold text-text-primary">
                Spacing
              </h4>
              {#each ['sm', 'md', 'lg', 'xl'] as step (step)}
                <div class="flex flex-col gap-1.5">
                  <label
                    class="text-type-sm font-label-sm"
                    for={`spacing-${step}`}>Spacing {step}</label
                  >
                  <input
                    id={`spacing-${step}`}
                    type="text"
                    class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border font-mono text-type-sm text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 max-w-xs"
                    value={wc.draft.modes[mode()].spacing?.[
                      step as keyof NonNullable<
                        typeof wc.draft.modes.dark.spacing
                      >
                    ] ?? ''}
                    placeholder={step === 'sm'
                      ? '4px'
                      : step === 'md'
                        ? '8px'
                        : step === 'lg'
                          ? '16px'
                          : '24px'}
                    onchange={(e) => {
                      const m = wc.draft!.modes[mode()]
                      const spacing = {
                        sm: m.spacing?.sm ?? '4px',
                        md: m.spacing?.md ?? '8px',
                        lg: m.spacing?.lg ?? '16px',
                        xl: m.spacing?.xl ?? '24px',
                        [step]: (e.currentTarget as HTMLInputElement).value
                      }
                      wc.setAt(wc.modePath('spacing'), spacing)
                    }}
                  />
                </div>
              {/each}
            </div>
            <div class="pt-2 border-t border-surface-panel-border space-y-4">
              <h4 class="text-type-sm font-label-sm-bold text-text-primary">
                Shadow
              </h4>
              {#each ['sm', 'md', 'lg'] as step (step)}
                <div class="flex flex-col gap-1.5">
                  <label
                    class="text-type-sm font-label-sm"
                    for={`shadow-${step}`}>Shadow {step}</label
                  >
                  <input
                    id={`shadow-${step}`}
                    type="text"
                    class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border font-mono text-type-sm text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 w-full"
                    value={wc.draft.modes[mode()].shadow?.[
                      step as keyof NonNullable<
                        typeof wc.draft.modes.dark.shadow
                      >
                    ] ?? ''}
                    placeholder="CSS box-shadow"
                    onchange={(e) => {
                      const m = wc.draft!.modes[mode()]
                      const shadow = {
                        sm:
                          m.shadow?.sm ??
                          '0 1px 2px color-mix(in oklch, var(--color-surface-app) 40%, transparent)',
                        md:
                          m.shadow?.md ??
                          '0 4px 12px color-mix(in oklch, var(--color-surface-app) 35%, transparent)',
                        lg:
                          m.shadow?.lg ??
                          '0 12px 32px color-mix(in oklch, var(--color-surface-app) 30%, transparent)',
                        [step]: (e.currentTarget as HTMLInputElement).value
                      }
                      wc.setAt(wc.modePath('shadow'), shadow)
                    }}
                  />
                </div>
              {/each}
            </div>
          </div>
        {:else if wc.advancedGroup === 'editor'}
          <div class="max-w-xl space-y-6">
            <div class="flex items-start justify-between gap-3">
              <div>
                <h3 class="text-type-lg font-headline-md mb-1">
                  Editor tokens
                </h3>
                <p class="text-text-muted text-type-sm">
                  Caret, selection, links, and highlight on the writing canvas.
                </p>
              </div>
              <button
                type="button"
                class="flex-shrink-0 text-type-xs font-label-sm text-text-muted hover:text-text-primary underline-offset-2 hover:underline bg-transparent border-none cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 rounded-sm"
                onclick={() => resetGroupPaths(editorGroupPaths())}
              >
                Reset group
              </button>
            </div>
            {#each [{ key: 'caret', label: 'Caret' }, { key: 'selection', label: 'Selection' }, { key: 'selection_text', label: 'Selection text' }, { key: 'link', label: 'Link' }, { key: 'link_hover', label: 'Link hover' }, { key: 'highlight', label: 'Highlight' }] as field (field.key)}
              {@const ed = wc.draft.modes[mode()].editor}
              <OklchColorField
                label={field.label}
                value={ed?.[field.key as keyof NonNullable<typeof ed>] ??
                  previewTokens[
                    `--color-editor-${field.key.replace('_', '-')}`
                  ] ??
                  ''}
                onchange={(v) => {
                  const m = wc.draft!.modes[mode()]
                  const base = concreteEditorDefaults(m)
                  const editor = { ...base, [field.key]: v }
                  wc.setAt(wc.modePath('editor'), editor)
                }}
                onReset={() => {
                  // Reset whole editor block when seed has no per-key path.
                  const seedEd = wc.seed?.modes[mode()].editor
                  if (seedEd && field.key in seedEd) {
                    wc.resetPath(wc.modePath(`editor.${field.key}`))
                  } else {
                    wc.resetPath(wc.modePath('editor'))
                  }
                }}
              />
            {/each}
          </div>
        {:else if wc.advancedGroup === 'background'}
          <div class="max-w-xl space-y-6">
            <div class="flex items-start justify-between gap-3">
              <div>
                <h3 class="text-type-lg font-headline-md mb-1">Background</h3>
                <p class="text-text-muted text-type-sm">
                  Per-zone image overlay. Assets stage via PickImageFile +
                  PrepareBackgroundAsset; nothing writes until Save. Scrim is
                  the readability control (image luminance is not sampled yet).
                </p>
              </div>
              <button
                type="button"
                class="flex-shrink-0 text-type-xs font-label-sm text-text-muted hover:text-text-primary underline-offset-2 hover:underline bg-transparent border-none cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 rounded-sm"
                onclick={() => resetGroupPaths(backgroundGroupPaths())}
              >
                Reset group
              </button>
            </div>
            <div class="flex flex-col gap-1.5">
              <label class="text-type-sm font-label-sm" for="bg-zone"
                >Zone</label
              >
              <select
                id="bg-zone"
                class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 max-w-xs"
                bind:value={bgZone}
              >
                {#each SURFACE_ZONES as z (z)}
                  <option value={z}>{z}</option>
                {/each}
              </select>
            </div>
            <div
              class="flex flex-wrap items-center justify-between gap-2 rounded-md border border-surface-panel-border bg-surface-panel/40 px-3 py-2"
            >
              <span class="text-type-sm font-label-sm text-text-primary">
                Zone text on effective background
              </span>
              <ContrastBadge
                level={bgTextContrastLevel}
                ratio={bgTextContrastRatio}
                label="Zone text on effective background"
              />
            </div>
            <div class="flex flex-wrap gap-2">
              <button
                type="button"
                onclick={() => void pickBackground()}
                disabled={pickingBg}
                class="inline-flex items-center gap-2 px-3 py-2 rounded-md bg-accent-primary-start/15 border border-accent-primary-start/40 text-accent-primary-start font-label-sm-bold cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 disabled:opacity-40"
              >
                <span
                  class="material-symbols-outlined text-icon-md"
                  aria-hidden="true">add_photo_alternate</span
                >
                {pickingBg ? 'Preparing…' : 'Pick image'}
              </button>
              {#if bgEdit?.image}
                <button
                  type="button"
                  onclick={clearBackground}
                  class="inline-flex items-center gap-2 px-3 py-2 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary font-label-sm-bold cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
                >
                  Clear image
                </button>
              {/if}
            </div>
            {#if bgEdit?.image}
              <p class="text-type-xs font-mono text-text-muted break-all">
                {bgEdit.image.length > 80
                  ? bgEdit.image.slice(0, 80) + '…'
                  : bgEdit.image}
              </p>
            {/if}
            <div class="flex flex-col gap-1.5">
              <label class="text-type-sm font-label-sm" for="bg-size"
                >Size</label
              >
              <select
                id="bg-size"
                class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm max-w-xs focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
                value={bgEdit?.size ?? 'cover'}
                onchange={(e) =>
                  updateBackground({
                    size: (e.currentTarget as HTMLSelectElement).value
                  })}
              >
                <option value="cover">Cover</option>
                <option value="contain">Contain</option>
                <option value="tile">Tile</option>
              </select>
            </div>
            <div class="flex flex-col gap-1.5">
              <label class="text-type-sm font-label-sm" for="bg-opacity"
                >Opacity ({((bgEdit?.opacity ?? 0.35) * 100).toFixed(
                  0
                )}%)</label
              >
              <input
                id="bg-opacity"
                type="range"
                min="0"
                max="100"
                step="1"
                value={(bgEdit?.opacity ?? 0.35) * 100}
                class="max-w-xs"
                oninput={(e) =>
                  updateBackground({
                    opacity:
                      Number((e.currentTarget as HTMLInputElement).value) / 100
                  })}
              />
            </div>
            <div class="flex flex-col gap-1.5">
              <label class="text-type-sm font-label-sm" for="bg-blend"
                >Blend</label
              >
              <select
                id="bg-blend"
                class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm max-w-xs focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
                value={bgEdit?.blend ?? 'normal'}
                onchange={(e) =>
                  updateBackground({
                    blend: (e.currentTarget as HTMLSelectElement).value
                  })}
              >
                {#each ['normal', 'multiply', 'screen', 'overlay', 'soft-light', 'hard-light', 'darken', 'lighten'] as b (b)}
                  <option value={b}>{b}</option>
                {/each}
              </select>
            </div>
            <div class="flex flex-col gap-1.5">
              <label class="text-type-sm font-label-sm" for="bg-position"
                >Position</label
              >
              <input
                id="bg-position"
                type="text"
                class="h-10 px-3 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm max-w-xs focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
                value={bgEdit?.position ?? 'center'}
                onchange={(e) =>
                  updateBackground({
                    position: (e.currentTarget as HTMLInputElement).value
                  })}
              />
            </div>
            <OklchColorField
              label="Scrim"
              value={bgEdit?.scrim ?? ensureSurface(bgZone).bg}
              onchange={(v) => updateBackground({ scrim: v })}
              onReset={() =>
                wc.resetPath(
                  wc.modePath(`surfaces.${bgZone}.background.scrim`)
                )}
            />
          </div>
        {/if}

        {#if saveError}
          <div
            class="mt-8 rounded-lg border border-error-border bg-error-bg text-error px-3 py-2 text-type-sm"
            role="alert"
          >
            {saveError}
          </div>
        {/if}
      </div>
    </div>

    <!-- Sticky footer: dirty + contrast -->
    <footer
      class="sticky bottom-0 z-20 border-t border-surface-panel-border bg-surface-panel/95 backdrop-blur-sm px-4 py-2.5 flex flex-wrap items-center gap-3"
    >
      {#if wc.dirty}
        <div
          class="inline-flex items-center gap-1.5 text-type-sm font-label-sm text-accent-secondary-start"
          role="status"
        >
          <span
            class="material-symbols-outlined text-icon-md"
            aria-hidden="true">edit</span
          >
          Unsaved changes · Esc asks before leaving
        </div>
      {:else}
        <div class="text-type-sm font-label-sm text-text-muted">
          No unsaved changes
        </div>
      {/if}

      <div class="flex-1"></div>

      <div class="flex flex-wrap items-center gap-2">
        <span
          class="text-type-xs font-label-sm text-text-muted"
          aria-live="polite"
          aria-atomic="true">{liveContrastMsg}</span
        >
        {#each belowAA.slice(0, 3) as pair (pair.id)}
          <button
            type="button"
            class="inline-flex items-center gap-1.5 rounded-md border border-surface-panel-border bg-surface-app px-2 py-1 text-type-2xs font-label-sm cursor-pointer hover:border-border-active focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
            title={`Auto-fix ${pair.label}`}
            onclick={() => fixPair(pair)}
          >
            <ContrastBadge
              level={pair.level}
              ratio={pair.ratio}
              label={pair.label}
            />
            <span class="text-text-muted">Fix</span>
          </button>
        {/each}
      </div>
    </footer>
  {/if}
</div>
