<script lang="ts">
  import { untrack, onMount } from 'svelte'
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
  import {
    ListLanguagePacks,
    ListDomainPacks,
    EnsureLanguagePack,
    EnsureDomainPack
  } from '../../../bindings/silt/app.js'
  import { getDictionaryLoadError } from '../../lib/editor/spellcheck/dictionary'

  interface Props {
    ringAnchor?: string | null
  }
  let { ringAnchor = null }: Props = $props()

  let draft = $state<SystemConfig | null>(null)
  let lastSaved = $state<SystemConfig | null>(null)

  type LangPack = {
    id: string
    label: string
    license: string
    approx_bytes: number
    bundled: boolean
    downloadable: boolean
    installed: boolean
    version: string
  }
  type DomainPack = {
    id: string
    label: string
    license: string
    approx_bytes: number
    bundled: boolean
    downloadable: boolean
    installed: boolean
    default_on: boolean
    version: string
  }

  let languagePacks = $state<LangPack[]>([])
  let domainPacks = $state<DomainPack[]>([])
  let packBusy = $state<string | null>(null)
  let packError = $state<string | null>(null)
  let packStatus = $state<string | null>(null)

  async function refreshPacks() {
    try {
      languagePacks = (await ListLanguagePacks()) as LangPack[]
      domainPacks = (await ListDomainPacks()) as DomainPack[]
    } catch (e) {
      packError = String(e)
    }
  }

  onMount(() => {
    void refreshPacks()
  })

  function formatBytes(n: number): string {
    if (n >= 1_000_000) return `~${(n / 1_000_000).toFixed(1)} MB`
    if (n >= 1000) return `~${Math.round(n / 1000)} KB`
    return `~${n} B`
  }

  async function onLanguageChange(e: Event) {
    const id = (e.currentTarget as HTMLSelectElement).value
    draftEditor().spellcheck_language = id
    touch()
    packError = null
    packStatus = null
    const pack = languagePacks.find((p) => p.id === id)
    if (pack && !pack.bundled && !pack.installed) {
      packBusy = id
      packStatus = `Downloading ${pack.label}…`
      try {
        await EnsureLanguagePack(id)
        packStatus = `${pack.label} ready. Save settings to apply.`
        await refreshPacks()
      } catch (err) {
        packError = String(err)
        packStatus = null
      } finally {
        packBusy = null
      }
    }
  }

  function domainEnabled(id: string): boolean {
    const domains =
      draft?.editor?.spellcheck_domains ?? (['software-terms'] as string[])
    return domains.includes(id)
  }

  async function toggleDomain(id: string, on: boolean) {
    const ed = draftEditor() as config.EditorConfig & {
      spellcheck_domains?: string[]
    }
    const current = [...(ed.spellcheck_domains ?? ['software-terms'])]
    const next = on
      ? current.includes(id)
        ? current
        : [...current, id]
      : current.filter((d) => d !== id)
    ed.spellcheck_domains = next
    touch()
    packError = null
    if (on) {
      const pack = domainPacks.find((p) => p.id === id)
      if (pack && !pack.bundled && !pack.installed) {
        packBusy = id
        packStatus = `Downloading ${pack.label}…`
        try {
          await EnsureDomainPack(id)
          packStatus = `${pack.label} ready.`
          await refreshPacks()
        } catch (err) {
          packError = String(err)
          packStatus = null
        } finally {
          packBusy = null
        }
      }
    }
  }

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
    return draft!.ui as config.UIConfig
  }
  function draftUIFormatting(): config.FormattingConfig {
    const ui = draftUI()
    if (!ui.formatting) ui.formatting = {} as config.FormattingConfig
    return ui.formatting as config.FormattingConfig
  }
  function draftEditor(): config.EditorConfig {
    if (!draft!.editor) draft!.editor = {} as config.EditorConfig
    return draft!.editor as config.EditorConfig
  }

  function changed(): boolean {
    if (!draft || !lastSaved) return false
    return (
      JSON.stringify(draft.editor) !== JSON.stringify(lastSaved.editor) ||
      JSON.stringify(draft.ui) !== JSON.stringify(lastSaved.ui)
    )
  }

  let isValid = $derived(
    draft !== null &&
      draft.editor.font_size_px > 0 &&
      draft.editor.tab_indent_spaces > 0 &&
      draft.editor.line_height > 0 &&
      draft.editor.auto_save_delay_ms >= 0
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
        class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-5 {ringClass(
          'editor-typography'
        )}"
      >
        <h4
          class="font-label-sm-bold text-text-primary uppercase tracking-wider text-type-2xs"
        >
          Typography
        </h4>
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
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors"
            />
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
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors"
            />
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
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors"
            />
          </label>
        </div>
      </div>

      <!-- Preferences Card -->
      <div
        id="editor-preferences"
        class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-5 {ringClass(
          'editor-preferences'
        )}"
      >
        <h4
          class="font-label-sm-bold text-text-primary uppercase tracking-wider text-type-2xs"
        >
          Writing Preferences
        </h4>
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
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors"
            />
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
                checked={draft.editor?.spellcheck_enabled !== false}
                onchange={(e: Event) => {
                  draftEditor().spellcheck_enabled = (
                    e.currentTarget as HTMLInputElement
                  ).checked
                  touch()
                }}
                type="checkbox"
                class="w-4 h-4 accent-[var(--color-accent-primary-end)] cursor-pointer"
              />
              <span class="text-text-primary text-type-md font-body-md">
                Spellcheck (underline misspelled words)
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

          <!-- Spellcheck language + domain packs (#336 / #337) -->
          <div
            id="editor-spellcheck-packs"
            class="space-y-4 pt-2 border-t border-surface-panel-border/60"
            aria-labelledby="spellcheck-packs-heading"
          >
            <h5
              id="spellcheck-packs-heading"
              class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
            >
              Spellcheck dictionaries
            </h5>
            <p class="text-text-muted text-type-sm font-body-md">
              Additional languages download once and work offline. Domain packs
              reduce false positives on technical terms. Note text never leaves
              your machine.
            </p>

            <label class="flex flex-col gap-1.5 max-w-sm">
              <span
                class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
                >Language</span
              >
              <select
                value={draft.editor?.spellcheck_language || 'en-US'}
                onchange={onLanguageChange}
                disabled={packBusy !== null}
                class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors cursor-pointer disabled:opacity-50"
              >
                {#each languagePacks as pack (pack.id)}
                  <option value={pack.id}>
                    {pack.label}
                    {pack.bundled
                      ? '(bundled)'
                      : pack.installed
                        ? '(installed)'
                        : `(download ${formatBytes(pack.approx_bytes)})`}
                  </option>
                {:else}
                  <option value="en-US">English (US)</option>
                {/each}
              </select>
            </label>

            <fieldset class="space-y-2">
              <legend
                class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider mb-1"
              >
                Domain word lists
              </legend>
              {#each domainPacks as pack (pack.id)}
                <label
                  class="flex items-start gap-2.5 cursor-pointer select-none"
                >
                  <input
                    type="checkbox"
                    checked={domainEnabled(pack.id)}
                    disabled={packBusy !== null}
                    onchange={(e: Event) => {
                      void toggleDomain(
                        pack.id,
                        (e.currentTarget as HTMLInputElement).checked
                      )
                    }}
                    class="w-4 h-4 mt-0.5 accent-[var(--color-accent-primary-end)] cursor-pointer"
                  />
                  <span class="flex flex-col gap-0.5">
                    <span class="text-text-primary text-type-md font-body-md">
                      {pack.label}
                      {#if pack.bundled}
                        <span class="text-text-muted text-type-sm"
                          >(bundled)</span
                        >
                      {:else if !pack.installed}
                        <span class="text-text-muted text-type-sm"
                          >({formatBytes(pack.approx_bytes)})</span
                        >
                      {/if}
                    </span>
                    <span class="text-text-muted text-type-2xs"
                      >{pack.license}</span
                    >
                  </span>
                </label>
              {/each}
            </fieldset>

            {#if packBusy}
              <p
                class="text-text-muted text-type-sm font-body-md"
                aria-live="polite"
              >
                {packStatus ?? 'Working…'}
              </p>
            {:else if packStatus}
              <p
                class="text-text-muted text-type-sm font-body-md"
                aria-live="polite"
              >
                {packStatus}
              </p>
            {/if}
            {#if packError || getDictionaryLoadError()}
              <p class="text-error text-type-sm font-body-md" role="alert">
                {packError || getDictionaryLoadError()}
              </p>
            {/if}
          </div>
        </div>
      </div>

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
