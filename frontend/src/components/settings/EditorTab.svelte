<script lang="ts">
  import { untrack } from 'svelte'
  import {
    settings,
    saveConfig,
    reloadFromBackend
  } from '../../settings/store.svelte'
  import type { SystemConfig } from '../../settings/store.svelte'
  import type * as config from '../../../bindings/silt/backend/config/models.js'
  import { displayFamilyName } from '../../theme/fonts'
  import { themeState } from '../../theme/store.svelte'
  import FontSelect from './FontSelect.svelte'
  import SpellcheckPackManager from './spellcheck/SpellcheckPackManager.svelte'
  import { DATE_FORMATS } from '../../lib/dateFormat'
  import { openDeletedPageHistory } from '../editor/openDeletedPageHistory'

  interface Props {
    ringAnchor?: string | null
  }
  let { ringAnchor = null }: Props = $props()

  let draft = $state<SystemConfig | null>(null)
  let lastSaved = $state<SystemConfig | null>(null)

  function deepClone<T>(value: T): T {
    return JSON.parse(JSON.stringify(value))
  }

  $effect(() => {
    const cfg = settings.config
    if (!cfg) return
    const hasDraft = untrack(() => draft)
    const dirty = untrack(() => settings.dirty)
    if (hasDraft && dirty) return
    draft = deepClone(cfg)
    lastSaved = deepClone(cfg)
  })

  function touch() {
    settings.dirty = true
  }

  function draftUI(): config.UIConfig {
    if (!draft!.ui) draft!.ui = {} as config.UIConfig
    return draft!.ui
  }
  function draftUIFormatting(): config.FormattingConfig {
    const ui = draftUI()
    if (!ui.formatting) ui.formatting = {} as config.FormattingConfig
    return ui.formatting
  }
  function draftEditor(): config.EditorConfig {
    if (!draft!.editor) draft!.editor = {} as config.EditorConfig
    return draft!.editor
  }

  function changed(): boolean {
    if (!draft || !lastSaved) return false
    return (
      JSON.stringify(draft.editor) !== JSON.stringify(lastSaved.editor) ||
      JSON.stringify(draft.ui) !== JSON.stringify(lastSaved.ui)
    )
  }

  let fontSizeError = $derived.by(() => {
    if (!draft?.editor) return null
    const v = draft.editor.font_size_px
    if (v == null || !Number.isFinite(v) || v <= 0)
      return 'Font size must be greater than 0.'
    if (v < 8 || v > 48) return 'Font size must be between 8 and 48 px.'
    return null
  })

  let lineHeightError = $derived.by(() => {
    if (!draft?.editor) return null
    const v = draft.editor.line_height
    if (v == null || !Number.isFinite(v) || v <= 0)
      return 'Line height must be greater than 0.'
    if (v < 1 || v > 3) return 'Line height must be between 1.0 and 3.0.'
    return null
  })

  let tabIndentError = $derived.by(() => {
    if (!draft?.editor) return null
    const v = draft.editor.tab_indent_spaces
    if (v == null || !Number.isFinite(v) || v <= 0)
      return 'Tab width must be greater than 0.'
    if (v < 1 || v > 8) return 'Tab width must be between 1 and 8 spaces.'
    return null
  })

  let autoSaveError = $derived.by(() => {
    if (!draft?.editor) return null
    const v = draft.editor.auto_save_delay_ms
    if (v == null || !Number.isFinite(v) || v < 0)
      return 'Auto-save delay cannot be negative.'
    return null
  })

  let maxVersionsError = $derived.by(() => {
    if (!draft?.editor) return null
    const v = draft.editor.max_versions_per_page
    if (v == null) return null
    if (!Number.isFinite(v) || v < 1 || v > 500)
      return 'Versions to keep must be between 1 and 500.'
    return null
  })

  let isValid = $derived(
    draft !== null &&
      !fontSizeError &&
      !lineHeightError &&
      !tabIndentError &&
      !autoSaveError &&
      !maxVersionsError
  )

  let themeBodyFont = $derived(themeState.darkTokens['--font-body'] ?? '')
  let themeMonoFont = $derived(themeState.darkTokens['--font-mono'] ?? '')

  function resetFont(field: 'font_family' | 'mono_font_family') {
    if (!draft) return
    draft.editor[field] = ''
    touch()
  }

  async function handleSave() {
    if (!draft) return
    settings.dirty = false
    const ok = await saveConfig(draft)
    if (ok) {
      lastSaved = deepClone(draft)
    } else {
      settings.dirty = true
    }
  }

  function handleRevert() {
    if (!lastSaved) return
    draft = deepClone(lastSaved)
    settings.dirty = false
  }

  // Settings-search anchor landing: briefly ring the targeted card so the user
  // can see where the jump landed.
  function ringClass(id: string): string {
    return ringAnchor === id
      ? 'ring-2 ring-accent-primary-start transition-shadow'
      : ''
  }

  // Cross-link to Appearance (typography overrides live there too). Kept cheap:
  // a one-line hint that only renders when the active theme sets fonts.
  let themeSetsFonts = $derived(Boolean(themeBodyFont || themeMonoFont))
</script>

{#if !draft}
  <div class="p-8 text-text-muted font-body-md">No configuration loaded.</div>
{:else}
  <div class="flex-1 flex flex-col min-h-0 overflow-hidden h-full">
    <!-- Scrollable content -->
    <div class="flex-1 overflow-y-auto p-6 space-y-6 custom-scrollbar">
      <!-- External update notice -->
      {#if settings.pendingExternal}
        <div
          class="flex items-start gap-2 p-3 rounded-lg bg-accent-primary-start/10 border border-accent-primary-start/30 text-accent-primary-start text-type-sm font-body-md"
        >
          <span class="material-symbols-outlined text-icon-lg">sync</span>
          <span class="flex-1">
            Settings were updated externally. Your unsaved edits are preserved.
          </span>
          <button
            onclick={async () => {
              settings.dirty = false
              await reloadFromBackend()
            }}
            class="font-label-sm-bold underline hover:brightness-110 bg-transparent border-none cursor-pointer text-accent-primary-start"
          >
            Reload
          </button>
        </div>
      {/if}

      <!-- Typography Card -->
      <div
        id="editor-typography"
        class="bg-surface-panel border border-surface-panel-border rounded-lg p-4 space-y-4 {ringClass(
          'editor-typography'
        )}"
      >
        <h3
          class="font-label-sm-bold text-text-muted uppercase tracking-wider text-type-xs"
        >
          Typography
        </h3>
        {#if themeSetsFonts}
          <!-- Cross-link to Appearance: when the active theme sets fonts, the
               "reset to theme default" buttons above point here; this hint
               sends users the other way to change the theme-level fonts. -->
          <p class="text-text-muted text-type-xs font-label-sm -mt-3">
            The active theme sets its own fonts. Change the theme in
            <button
              type="button"
              aria-label="Go to Appearance settings"
              onclick={() =>
                window.dispatchEvent(
                  new CustomEvent('silt:settings-jump', {
                    detail: { section: 'appearance' }
                  })
                )}
              class="text-accent-primary-start underline hover:brightness-110 bg-transparent border-none cursor-pointer p-0 font-label-sm"
            >
              Appearance
            </button>.
          </p>
        {/if}
        <div class="grid grid-cols-2 gap-4">
          <label class="flex flex-col gap-1.5">
            <span
              class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
              >Font family</span
            >
            <div class="flex items-center gap-2">
              <FontSelect
                bind:value={draft.editor.font_family}
                category="body"
                themeFont={themeBodyFont}
                label="Font family"
                onchange={touch}
              />
              {#if themeBodyFont}
                <button
                  type="button"
                  onclick={() => resetFont('font_family')}
                  title="Reset to theme default ({displayFamilyName(
                    themeBodyFont
                  )})"
                  aria-label="Reset body font to theme default"
                  class="flex-shrink-0 px-2.5 py-2 rounded-lg bg-surface-panel border border-surface-panel-border text-text-muted hover:text-text-primary hover:border-accent-primary-start transition-colors cursor-pointer"
                >
                  <span class="material-symbols-outlined text-icon-lg"
                    >restart_alt</span
                  >
                </button>
              {/if}
            </div>
          </label>

          <label class="flex flex-col gap-1.5">
            <span
              class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
              >Monospace font</span
            >
            <div class="flex items-center gap-2">
              <FontSelect
                bind:value={draft.editor.mono_font_family}
                category="mono"
                themeFont={themeMonoFont}
                label="Monospace font"
                onchange={touch}
              />
              {#if themeMonoFont}
                <button
                  type="button"
                  onclick={() => resetFont('mono_font_family')}
                  title="Reset to theme default ({displayFamilyName(
                    themeMonoFont
                  )})"
                  aria-label="Reset monospace font to theme default"
                  class="flex-shrink-0 px-2.5 py-2 rounded-lg bg-surface-panel border border-surface-panel-border text-text-muted hover:text-text-primary hover:border-accent-primary-start transition-colors cursor-pointer"
                >
                  <span class="material-symbols-outlined text-icon-lg"
                    >restart_alt</span
                  >
                </button>
              {/if}
            </div>
          </label>

          <label class="flex flex-col gap-1.5">
            <span
              class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
              >Font size (px)</span
            >
            <input
              bind:value={draft.editor.font_size_px}
              oninput={touch}
              type="number"
              min="8"
              max="48"
              aria-invalid={fontSizeError ? 'true' : undefined}
              aria-describedby={fontSizeError
                ? 'editor-font-size-error'
                : undefined}
              class="bg-surface-panel border {fontSizeError
                ? 'border-status-danger'
                : 'border-surface-panel-border'} rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors"
            />
            {#if fontSizeError}
              <p
                id="editor-font-size-error"
                class="text-type-xs text-status-danger"
                role="alert"
              >
                {fontSizeError}
              </p>
            {/if}
          </label>

          <label class="flex flex-col gap-1.5">
            <span
              class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
              >Line height</span
            >
            <input
              bind:value={draft.editor.line_height}
              oninput={touch}
              type="number"
              step="0.1"
              min="1"
              max="3"
              aria-invalid={lineHeightError ? 'true' : undefined}
              aria-describedby={lineHeightError
                ? 'editor-line-height-error'
                : undefined}
              class="bg-surface-panel border {lineHeightError
                ? 'border-status-danger'
                : 'border-surface-panel-border'} rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors"
            />
            {#if lineHeightError}
              <p
                id="editor-line-height-error"
                class="text-type-xs text-status-danger"
                role="alert"
              >
                {lineHeightError}
              </p>
            {/if}
          </label>

          <label class="flex flex-col gap-1.5">
            <span
              class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
              >Tab width (spaces)</span
            >
            <input
              bind:value={draft.editor.tab_indent_spaces}
              oninput={touch}
              type="number"
              min="1"
              max="8"
              aria-invalid={tabIndentError ? 'true' : undefined}
              aria-describedby={tabIndentError
                ? 'editor-tab-indent-error'
                : undefined}
              class="bg-surface-panel border {tabIndentError
                ? 'border-status-danger'
                : 'border-surface-panel-border'} rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors"
            />
            {#if tabIndentError}
              <p
                id="editor-tab-indent-error"
                class="text-type-xs text-status-danger"
                role="alert"
              >
                {tabIndentError}
              </p>
            {/if}
          </label>
        </div>
      </div>

      <!-- Preferences Card -->
      <div
        id="editor-preferences"
        class="bg-surface-panel border border-surface-panel-border rounded-lg p-4 space-y-4 {ringClass(
          'editor-preferences'
        )}"
      >
        <h3
          class="font-label-sm-bold text-text-muted uppercase tracking-wider text-type-xs"
        >
          Writing Preferences
        </h3>
        <div class="space-y-4">
          <label class="flex flex-col gap-1.5 max-w-xs">
            <span
              class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
              >Auto-save delay (ms)</span
            >
            <input
              bind:value={draft.editor.auto_save_delay_ms}
              oninput={touch}
              type="number"
              min="0"
              step="100"
              aria-invalid={autoSaveError ? 'true' : undefined}
              aria-describedby={autoSaveError
                ? 'editor-autosave-error'
                : undefined}
              class="bg-surface-panel border {autoSaveError
                ? 'border-status-danger'
                : 'border-surface-panel-border'} rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors"
            />
            {#if autoSaveError}
              <p
                id="editor-autosave-error"
                class="text-type-xs text-status-danger"
                role="alert"
              >
                {autoSaveError}
              </p>
            {/if}
          </label>

          <div class="grid grid-cols-2 gap-3 pt-2">
            <label class="flex items-center gap-2.5 cursor-pointer select-none">
              <input
                bind:checked={draft.editor.focus_highlight_ancestors}
                onchange={touch}
                type="checkbox"
                class="w-4 h-4 accent-[var(--color-accent-primary-end)] cursor-pointer"
              />
              <span class="text-text-primary text-type-md font-body-md">
                Highlight ancestor blocks on focus
              </span>
            </label>

            <label class="flex items-center gap-2.5 cursor-pointer select-none">
              <input
                checked={draft.ui?.show_format_toolbar !== false}
                onchange={(e: Event) => {
                  draftUI().show_format_toolbar = (
                    e.currentTarget as HTMLInputElement
                  ).checked
                  touch()
                }}
                type="checkbox"
                class="w-4 h-4 accent-[var(--color-accent-primary-end)] cursor-pointer"
              />
              <span class="text-text-primary text-type-md font-body-md">
                Show format toolbar
              </span>
            </label>

            <label class="flex items-center gap-2.5 cursor-pointer select-none">
              <input
                checked={draft.ui?.formatting?.typography_enabled !== false}
                onchange={(e: Event) => {
                  draftUIFormatting().typography_enabled = (
                    e.currentTarget as HTMLInputElement
                  ).checked
                  touch()
                }}
                type="checkbox"
                class="w-4 h-4 accent-[var(--color-accent-primary-end)] cursor-pointer"
              />
              <span class="text-text-primary text-type-md font-body-md">
                Smart typography (em-dash, smart quotes)
              </span>
            </label>

            <label class="flex items-center gap-2.5 cursor-pointer select-none">
              <input
                checked={draft.ui?.formatting?.color_enabled !== false}
                onchange={(e: Event) => {
                  draftUIFormatting().color_enabled = (
                    e.currentTarget as HTMLInputElement
                  ).checked
                  touch()
                }}
                type="checkbox"
                class="w-4 h-4 accent-[var(--color-accent-primary-end)] cursor-pointer"
              />
              <span class="text-text-primary text-type-md font-body-md">
                Text and background color pickers
              </span>
            </label>

            <label class="flex items-center gap-2.5 cursor-pointer select-none">
              <input
                checked={draft.editor?.show_word_count === true}
                onchange={(e: Event) => {
                  draftEditor().show_word_count = (
                    e.currentTarget as HTMLInputElement
                  ).checked
                  touch()
                }}
                type="checkbox"
                class="w-4 h-4 accent-[var(--color-accent-primary-end)] cursor-pointer"
              />
              <span class="text-text-primary text-type-md font-body-md">
                Show word count
              </span>
            </label>

            <label class="flex items-center gap-2.5 cursor-pointer select-none">
              <input
                checked={draft.editor?.focus_mode === true}
                onchange={(e: Event) => {
                  draftEditor().focus_mode = (
                    e.currentTarget as HTMLInputElement
                  ).checked
                  touch()
                }}
                type="checkbox"
                class="w-4 h-4 accent-[var(--color-accent-primary-end)] cursor-pointer"
              />
              <span class="text-text-primary text-type-md font-body-md">
                Focus mode (dim inactive paragraphs)
              </span>
            </label>

            <label class="flex items-center gap-2.5 cursor-pointer select-none">
              <input
                checked={draft.editor?.typewriter_mode === true}
                onchange={(e: Event) => {
                  draftEditor().typewriter_mode = (
                    e.currentTarget as HTMLInputElement
                  ).checked
                  touch()
                }}
                type="checkbox"
                class="w-4 h-4 accent-[var(--color-accent-primary-end)] cursor-pointer"
              />
              <span class="text-text-primary text-type-md font-body-md">
                Typewriter mode (keep active line centered)
              </span>
            </label>
          </div>

          <div class="space-y-2.5 pt-1">
            <label class="flex items-center gap-2.5 cursor-pointer select-none">
              <input
                checked={draft.editor?.auto_versioning_enabled === true}
                onchange={(e: Event) => {
                  const enabled = (e.currentTarget as HTMLInputElement).checked
                  const editor = draftEditor()
                  editor.auto_versioning_enabled = enabled
                  if (enabled) {
                    const cur = editor.max_versions_per_page
                    if (cur == null || cur < 1 || cur > 500) {
                      editor.max_versions_per_page = 50
                    }
                  }
                  touch()
                }}
                type="checkbox"
                aria-describedby="page-history-settings-help"
                class="w-4 h-4 accent-[var(--color-accent-primary-end)] cursor-pointer"
              />
              <span class="text-text-primary text-type-md font-body-md">
                Capture page history
              </span>
            </label>
            <p
              id="page-history-settings-help"
              class="text-text-muted text-type-sm font-body-md max-w-xl pl-6"
            >
              Snapshots stay in this vault under
              <span class="font-mono">.system/history</span>. They sync with the
              vault, are not encrypted, and are not a substitute for Settings →
              General → Export vault…. Pages larger than 1 MB are not
              snapshotted. Deleting a page leaves its snapshots; browse them
              below.
            </p>
            <button
              type="button"
              data-testid="browse-deleted-pages"
              onclick={() => openDeletedPageHistory()}
              class="ml-6 mt-1 cursor-pointer rounded-lg border border-surface-panel-border bg-surface-panel px-3 py-1.5 text-type-sm font-label-sm-bold text-text-primary transition-colors hover:border-accent-primary-start hover:text-text-primary"
            >
              Browse deleted pages
            </button>
            {#if draft.editor?.auto_versioning_enabled === true}
              <label class="flex flex-col gap-1.5 max-w-xs pl-6">
                <span
                  class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
                  >Versions to keep per page</span
                >
                <input
                  value={draft.editor?.max_versions_per_page ?? 50}
                  oninput={(e: Event) => {
                    const raw = (e.currentTarget as HTMLInputElement).value
                    const n = Number(raw)
                    draftEditor().max_versions_per_page = Number.isFinite(n)
                      ? n
                      : 0
                    touch()
                  }}
                  onblur={(e: Event) => {
                    const el = e.currentTarget as HTMLInputElement
                    const n = Number(el.value)
                    const clamped = !Number.isFinite(n)
                      ? 50
                      : Math.min(500, Math.max(1, Math.round(n)))
                    draftEditor().max_versions_per_page = clamped
                    el.value = String(clamped)
                    touch()
                  }}
                  type="number"
                  min="1"
                  max="500"
                  step="1"
                  aria-invalid={maxVersionsError ? 'true' : undefined}
                  aria-describedby={maxVersionsError
                    ? 'editor-max-versions-error'
                    : undefined}
                  class="bg-surface-panel border {maxVersionsError
                    ? 'border-status-danger'
                    : 'border-surface-panel-border'} rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors"
                />
                {#if maxVersionsError}
                  <p
                    id="editor-max-versions-error"
                    class="text-type-xs text-status-danger"
                    role="alert"
                  >
                    {maxVersionsError}
                  </p>
                {/if}
              </label>
            {/if}
          </div>
        </div>
      </div>

      <!-- Date format (#730) -->
      <div
        class="bg-surface-panel border border-surface-panel-border rounded-lg p-4 space-y-4"
      >
        <div class="space-y-1">
          <h3
            class="font-label-sm-bold text-text-muted uppercase tracking-wider text-type-xs"
          >
            Date format
          </h3>
          <p class="text-text-muted text-type-sm font-body-md max-w-xl">
            Controls how dates are inserted by the Date Glance popover and the
            /today command.
          </p>
        </div>
        <label class="flex flex-col gap-1.5 max-w-sm">
          <span
            class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
            >Format</span
          >
          <select
            value={draft.editor?.date_format || 'YYYY-MM-DD'}
            onchange={(e: Event) => {
              draftEditor().date_format = (
                e.currentTarget as HTMLSelectElement
              ).value
              touch()
            }}
            class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors cursor-pointer"
          >
            {#each DATE_FORMATS as fmt (fmt.id)}
              <option value={fmt.id}>{fmt.example}</option>
            {/each}
          </select>
        </label>
      </div>

      <!-- Spellcheck dictionaries card — delegates to SpellcheckPackManager.
           The three spellcheck draft fields bind two-way ($bindable) so the
           child edits the parent's draft in place; touch() propagates dirty
           state. (#764) -->
      <SpellcheckPackManager
        bind:spellcheckEnabled={draft.editor.spellcheck_enabled}
        bind:spellcheckLanguage={draft.editor.spellcheck_language}
        bind:spellcheckDomains={draft.editor.spellcheck_domains}
        {touch}
        ringed={ringAnchor === 'editor-spellcheck-packs'}
      />

      <!-- Error banner -->
      {#if settings.error}
        <div
          class="flex items-start gap-2 p-3 rounded-lg bg-error-bg border border-error-border text-error text-type-sm font-body-md"
        >
          <span class="material-symbols-outlined text-icon-lg">error</span>
          <span class="flex-1">{settings.error}</span>
        </div>
      {/if}
    </div>

    <!-- Fixed Footer Actions -->
    <div
      class="flex items-center justify-end gap-2 px-6 py-4 border-t border-surface-panel-border bg-surface-panel/10 flex-shrink-0"
    >
      <button
        onclick={handleRevert}
        disabled={!changed()}
        class="px-4 py-2 rounded-lg text-text-muted hover:text-text-primary font-label-sm-bold transition-colors border-none bg-transparent cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed"
      >
        Revert
      </button>
      <button
        onclick={handleSave}
        disabled={!changed() || !isValid || settings.saving}
        class="px-4 py-2 rounded-lg bg-accent-primary-start/20 border border-accent-primary-start/40 text-accent-primary-start font-label-sm-bold hover:brightness-110 transition-all cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
      >
        {settings.saving ? 'Saving…' : 'Save changes'}
      </button>
    </div>
  </div>
{/if}
