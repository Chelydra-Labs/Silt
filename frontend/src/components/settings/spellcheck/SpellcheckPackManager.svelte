<script lang="ts">
  // Self-contained spellcheck pack manager for the Editor tab. Owns the entire
  // language/domain pack lifecycle: listing, download, progress, cancel, retry.
  //
  // The three spellcheck draft fields (enabled / language / domains) are
  // `$bindable` because they live in the parent's editor-config draft and are
  // read + written here (including the optimistic-select-then-revert-on-failure
  // flow in onLanguageChange). `$bindable` is the sanctioned Svelte-5 path for
  // a child that edits a shared object — direct mutation of a passed proxy
  // would emit an ownership_invalid_mutation warning. Dirty tracking is
  // delegated back to the parent via `touch()`.
  import { onMount } from 'svelte'
  import { Events } from '@wailsio/runtime'
  import { EventName } from '../../../generated/enums'
  import {
    ListLanguagePacks,
    ListDomainPacks,
    EnsureLanguagePack,
    EnsureDomainPack,
    CancelSpellcheckDownload
  } from '../../../../bindings/silt/app.js'
  import {
    dictionaryStatus,
    friendlyPackError
  } from '../../../lib/editor/spellcheck/dictionaryStatus.svelte'

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

  interface Props {
    // Nullable because the wails-generated EditorConfig types these fields as
    // `T | null` (absent vs. explicitly unset). `!== false` / `|| 'en-US'`
    // treat null like undefined (default-on / fallback).
    spellcheckEnabled: boolean | null | undefined
    spellcheckLanguage: string | null | undefined
    spellcheckDomains: string[] | undefined
    touch: () => void
    ringed?: boolean
  }
  let {
    spellcheckEnabled = $bindable(),
    spellcheckLanguage = $bindable(),
    spellcheckDomains = $bindable(),
    touch,
    ringed = false
  }: Props = $props()

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
    spellcheckDomains = ids
    touch()
  }

  function domainList(): string[] {
    return spellcheckDomains ?? (['software-terms'] as string[])
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
    progressUnsub = Events.On(
      EventName.EventSpellcheckDownloadProgress,
      (ev) => {
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
      }
    )
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
    const prev = spellcheckLanguage || 'en-US'
    if (id === prev) return
    packError = null
    packStatus = null
    failedLangId = null
    // Optimistic select so the control stays in sync with the user gesture;
    // revert draft (and thus the select) if download fails.
    spellcheckLanguage = id
    touch()
    const pack = languagePacks.find((p) => p.id === id)
    if (pack && !pack.bundled && !pack.installed) {
      const ok = await downloadLanguage(id)
      if (!ok) {
        spellcheckLanguage = prev
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
    const prev = spellcheckLanguage || 'en-US'
    spellcheckLanguage = id
    touch()
    const ok = await downloadLanguage(id)
    if (ok) {
      failedLangId = null
    } else {
      spellcheckLanguage = prev
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
</script>

<!-- Spellcheck dictionaries card (#336 / #337 / #537) -->
<div
  id="editor-spellcheck-packs"
  class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-5 {ringed
    ? 'ring-2 ring-accent-primary-start transition-shadow'
    : ''}"
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
        Additional languages download once and work offline. Technical word
        lists cut underlines on terms like TypeScript or Docker. Note text never
        leaves your machine — only dictionary files download when you choose
        them. Language and word-list changes apply after you save settings.
        Manage personal words under General → Custom dictionary.
      </p>
    </div>
    <label
      class="flex items-center gap-2.5 cursor-pointer select-none shrink-0"
    >
      <input
        checked={spellcheckEnabled !== false}
        onchange={(e: Event) => {
          spellcheckEnabled = (e.currentTarget as HTMLInputElement).checked
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

  {#if spellcheckEnabled === false}
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
        value={spellcheckLanguage || 'en-US'}
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
        Some languages use open-source licenses (e.g. GPL or MPL). The license
        file is saved with the download.
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
