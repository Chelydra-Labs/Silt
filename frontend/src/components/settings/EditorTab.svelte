<script lang="ts">
  import { untrack, onMount } from 'svelte'
  import { Events } from '@wailsio/runtime'
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
    EnsureDomainPack,
    CancelSpellcheckDownload
  } from '../../../bindings/silt/app.js'
  import {
    dictionaryStatus,
    friendlyPackError
  } from '../../lib/editor/spellcheck/dictionaryStatus.svelte'

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
  let packsLoading = $state(true)
  let packBusy = $state<string | null>(null)
  let packError = $state<string | null>(null)
  let packStatus = $state<string | null>(null)
  let packProgress = $state<number | null>(null)
  let packStage = $state<string | null>(null)
  let failedLangId = $state<string | null>(null)
  let failedDomainId = $state<string | null>(null)
  let progressUnsub: (() => void) | null = null

  function setDomainList(ids: string[]) {
    const ed = draftEditor() as config.EditorConfig & {
      spellcheck_domains?: string[]
    }
    ed.spellcheck_domains = ids
    touch()
  }

  function domainList(): string[] {
    return draft?.editor?.spellcheck_domains ?? (['software-terms'] as string[])
  }

  async function refreshPacks() {
    try {
      languagePacks = await ListLanguagePacks()
      domainPacks = await ListDomainPacks()
    } catch (e) {
      packError = friendlyPackError(e)
    } finally {
      packsLoading = false
    }
  }

  function subscribeProgress() {
    unsubscribeProgress()
    progressUnsub = Events.On('spellcheck:download:progress', (ev) => {
      const p = ev?.data as
        | {
            received?: number
            total?: number
            id?: string
            file?: string
          }
        | undefined
      if (!p) return
      if (p.file) {
        packStage = stageLabel(p.file)
      }
      if (p.total && p.total > 0 && typeof p.received === 'number') {
        packProgress = Math.min(100, Math.round((p.received / p.total) * 100))
      }
    })
  }

  function stageLabel(file: string): string {
    if (file === 'index.aff' || file === 'aff') return 'rules'
    if (file === 'index.dic' || file === 'dic') return 'word list'
    if (file === 'license') return 'license'
    if (file === 'words') return 'word list'
    return file
  }

  function unsubscribeProgress() {
    if (progressUnsub) {
      progressUnsub()
      progressUnsub = null
    }
  }

  onMount(() => {
    void refreshPacks()
    return () => unsubscribeProgress()
  })

  function formatBytes(n: number): string {
    if (n >= 1_000_000) return `~${(n / 1_000_000).toFixed(1)} MB`
    if (n >= 1000) return `~${Math.round(n / 1000)} KB`
    return `~${n} B`
  }

  function statusLabel(pack: {
    bundled: boolean
    installed: boolean
    approx_bytes: number
  }): string {
    if (pack.bundled) return 'Included'
    if (pack.installed) return 'Downloaded'
    return `Download · ${formatBytes(pack.approx_bytes)}`
  }

  async function downloadLanguage(id: string): Promise<boolean> {
    const pack = languagePacks.find((p) => p.id === id)
    if (!pack) return false
    if (pack.bundled || pack.installed) return true
    packBusy = id
    packProgress = null
    packStage = null
    packStatus = `Downloading ${pack.label}…`
    packError = null
    failedLangId = null
    subscribeProgress()
    try {
      await EnsureLanguagePack(id)
      packStatus = `${pack.label} downloaded. Save settings to apply.`
      await refreshPacks()
      return true
    } catch (err) {
      const msg = String(err)
      if (msg.toLowerCase().includes('cancel')) {
        packStatus = 'Download cancelled.'
        packError = null
        failedLangId = null
      } else {
        packError = friendlyPackError(err)
        packStatus = null
        failedLangId = id
      }
      return false
    } finally {
      packBusy = null
      packProgress = null
      packStage = null
      unsubscribeProgress()
    }
  }

  async function onLanguageChange(e: Event) {
    const id = (e.currentTarget as HTMLSelectElement).value
    const prev = draft?.editor?.spellcheck_language || 'en-US'
    if (id === prev) return
    packError = null
    packStatus = null
    failedLangId = null
    // Optimistic select so the control stays in sync with the user gesture;
    // revert draft (and thus the select) if download fails.
    draftEditor().spellcheck_language = id
    touch()
    const pack = languagePacks.find((p) => p.id === id)
    if (pack && !pack.bundled && !pack.installed) {
      const ok = await downloadLanguage(id)
      if (!ok) {
        draftEditor().spellcheck_language = prev
        return
      }
    }
    if (pack && (pack.bundled || pack.installed)) {
      packStatus = `${pack.label} selected. Save settings to apply.`
    }
  }

  async function retryFailedLanguage() {
    if (!failedLangId) return
    const id = failedLangId
    const prev = draft?.editor?.spellcheck_language || 'en-US'
    draftEditor().spellcheck_language = id
    touch()
    const ok = await downloadLanguage(id)
    if (ok) {
      failedLangId = null
    } else {
      draftEditor().spellcheck_language = prev
    }
  }

  function domainEnabled(id: string): boolean {
    return domainList().includes(id)
  }

  async function downloadDomain(id: string): Promise<boolean> {
    const pack = domainPacks.find((p) => p.id === id)
    if (!pack) return false
    if (pack.bundled || pack.installed) return true
    packBusy = id
    packProgress = null
    packStage = null
    packStatus = `Downloading ${pack.label}…`
    packError = null
    failedDomainId = null
    subscribeProgress()
    try {
      await EnsureDomainPack(id)
      packStatus = `${pack.label} downloaded. Save settings to apply.`
      await refreshPacks()
      return true
    } catch (err) {
      const msg = String(err)
      if (msg.toLowerCase().includes('cancel')) {
        packStatus = 'Download cancelled.'
        packError = null
        failedDomainId = null
      } else {
        packError = friendlyPackError(err)
        packStatus = null
        failedDomainId = id
      }
      return false
    } finally {
      packBusy = null
      packProgress = null
      packStage = null
      unsubscribeProgress()
    }
  }

  async function toggleDomain(id: string, on: boolean) {
    const current = domainList()
    packError = null
    if (!on) {
      setDomainList(current.filter((d) => d !== id))
      return
    }
    if (current.includes(id)) return
    const pack = domainPacks.find((p) => p.id === id)
    if (pack && !pack.bundled && !pack.installed) {
      const ok = await downloadDomain(id)
      if (!ok) return
    }
    setDomainList([...current, id])
    if (pack && (pack.bundled || pack.installed) && !packBusy) {
      packStatus = `${pack.label} enabled. Save settings to apply.`
    }
  }

  async function retryFailedDomain() {
    if (!failedDomainId) return
    const ok = await downloadDomain(failedDomainId)
    if (ok) {
      const current = domainList()
      if (!current.includes(failedDomainId)) {
        setDomainList([...current, failedDomainId])
      }
      failedDomainId = null
    }
  }

  function cancelDownload() {
    try {
      void CancelSpellcheckDownload()
    } catch {
      /* ignore */
    }
    packStatus = 'Download cancelled.'
    packBusy = null
    packProgress = null
    packStage = null
    unsubscribeProgress()
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
        </div>
      </div>

      <!-- Spellcheck dictionaries card (#336 / #337 / #537) -->
      <div
        id="editor-spellcheck-packs"
        class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-5 {ringClass(
          'editor-spellcheck-packs'
        )}"
        aria-labelledby="spellcheck-packs-heading"
        aria-busy={packBusy !== null || packsLoading}
      >
        <div
          class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between"
        >
          <div class="space-y-1">
            <h4
              id="spellcheck-packs-heading"
              class="font-label-sm-bold text-text-primary uppercase tracking-wider text-type-2xs"
            >
              Spellcheck dictionaries
            </h4>
            <p class="text-text-muted text-type-sm font-body-md max-w-xl">
              Additional languages download once and work offline. Technical
              word lists cut underlines on terms like TypeScript or Docker. Note
              text never leaves your machine — only dictionary files download
              when you choose them. Language and word-list changes apply after
              you save settings. Manage personal words under General → Custom
              dictionary.
            </p>
          </div>
          <label
            class="flex items-center gap-2.5 cursor-pointer select-none shrink-0"
          >
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
              Enable spellcheck
            </span>
          </label>
        </div>

        {#if draft.editor?.spellcheck_enabled === false}
          <p class="text-text-muted text-type-sm font-body-md">
            Language and word lists apply when spellcheck is on.
          </p>
        {/if}

        {#if packsLoading}
          <p class="text-text-muted text-type-sm font-body-md">
            Loading dictionaries…
          </p>
        {:else}
          <label class="flex flex-col gap-1.5 max-w-sm">
            <span
              class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
              >Language</span
            >
            <select
              value={draft.editor?.spellcheck_language || 'en-US'}
              onchange={onLanguageChange}
              disabled={packBusy !== null}
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {#each languagePacks as pack (pack.id)}
                <option value={pack.id}>
                  {pack.label} ({statusLabel(pack)})
                </option>
              {:else}
                <option value="en-US">English (US) (Included)</option>
              {/each}
            </select>
          </label>
          {#if languagePacks.some((p) => p.license.includes('GPL') || p.license.includes('MPL'))}
            <p class="text-text-muted text-type-2xs font-body-md max-w-lg">
              Some languages use open-source licenses (e.g. GPL or MPL). The
              license file is saved with the download.
            </p>
          {/if}

          <fieldset class="space-y-2">
            <legend
              class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider mb-1"
            >
              Technical word lists
            </legend>
            {#each domainPacks as pack (pack.id)}
              <label
                class="flex items-start gap-2.5 select-none {packBusy
                  ? 'cursor-not-allowed opacity-70'
                  : 'cursor-pointer'}"
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
                  class="w-4 h-4 mt-0.5 accent-[var(--color-accent-primary-end)] cursor-pointer disabled:cursor-not-allowed"
                />
                <span class="flex flex-col gap-0.5">
                  <span class="text-text-primary text-type-md font-body-md">
                    {pack.label}
                    <span class="text-text-muted text-type-sm">
                      ({statusLabel(pack)})</span
                    >
                  </span>
                </span>
              </label>
            {:else}
              <p class="text-text-muted text-type-sm font-body-md">
                Couldn't load word lists.
              </p>
            {/each}
          </fieldset>
        {/if}

        <div
          class="flex flex-wrap items-center gap-3 min-h-[1.5rem]"
          aria-live="polite"
        >
          {#if packBusy}
            <p class="text-text-muted text-type-sm font-body-md">
              {packStatus ?? `Downloading…`}
              {#if packStage}
                — {packStage}
              {/if}
              {#if packProgress != null}
                ({packProgress}%)
              {/if}
            </p>
            <button
              type="button"
              onclick={cancelDownload}
              class="px-3 py-1 rounded-lg bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm font-label-sm-bold cursor-pointer hover:brightness-110"
            >
              Cancel
            </button>
          {:else if packStatus}
            <p class="text-text-muted text-type-sm font-body-md">
              {packStatus}
            </p>
          {/if}
        </div>
        {#if packError || dictionaryStatus.loadError || dictionaryStatus.domainError}
          <div
            class="flex items-start gap-2 p-3 rounded-lg bg-error-bg border border-error-border text-error text-type-sm font-body-md"
            role="alert"
          >
            <span class="material-symbols-outlined text-icon-lg">error</span>
            <div class="flex-1 space-y-2">
              <p>
                {packError ||
                  dictionaryStatus.loadError ||
                  dictionaryStatus.domainError}
              </p>
              {#if failedLangId}
                <button
                  type="button"
                  onclick={() => void retryFailedLanguage()}
                  disabled={packBusy !== null}
                  class="px-3 py-1 rounded-lg bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm font-label-sm-bold cursor-pointer disabled:opacity-50"
                >
                  Retry download
                </button>
              {:else if failedDomainId}
                <button
                  type="button"
                  onclick={() => void retryFailedDomain()}
                  disabled={packBusy !== null}
                  class="px-3 py-1 rounded-lg bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm font-label-sm-bold cursor-pointer disabled:opacity-50"
                >
                  Retry download
                </button>
              {/if}
            </div>
          </div>
        {/if}
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
