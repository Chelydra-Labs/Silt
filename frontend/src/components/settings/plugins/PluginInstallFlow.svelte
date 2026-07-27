<script lang="ts">
  // Install-from-archive flow: pick → validate → preview → install → reload.
  // Extracted from PluginsTab (#765) as a self-contained sub-feature. Owns its
  // own preview/install state + IPC; the parent passes `reloadAll` (called after
  // a successful install so the card list refreshes) and an optional `actions`
  // snippet rendered beside the install button (used for the "Check for
  // updates" button, which mutates the parent's card list and so stays there).
  import type { Snippet } from 'svelte'
  import {
    PickPluginArchive,
    ValidatePluginArchive,
    InstallPlugin
  } from '../../../../bindings/silt/app.js'
  import { capabilityLabels, qualifierLabel } from './capabilityLabels'

  type InstallPreview = {
    manifest: {
      id: string
      name: string
      version?: string
      description?: string
      capabilities?: Record<string, true | string>
    }
    warnings?: string[]
  }

  interface Props {
    reloadAll: () => Promise<void>
    onError: (msg: string) => void
    actions?: Snippet
  }
  let { reloadAll, onError, actions }: Props = $props()

  let installing = $state(false)
  let validating = $state(false)
  let preview = $state<InstallPreview | null>(null)
  let previewError = $state('')
  let pendingPath = $state('')

  async function chooseArchive() {
    preview = null
    previewError = ''
    pendingPath = ''
    validating = true
    try {
      const selected = await PickPluginArchive()
      if (!selected) return
      pendingPath = selected
      const result = await ValidatePluginArchive(selected)
      // ValidatePluginArchive returns { manifest, warnings }.
      preview = {
        manifest: result.manifest,
        warnings: result.warnings ?? []
      }
    } catch (e) {
      previewError = e instanceof Error ? e.message : String(e)
    } finally {
      validating = false
    }
  }

  async function confirmInstall() {
    if (!pendingPath) return
    installing = true
    try {
      await InstallPlugin(pendingPath)
      pendingPath = ''
      preview = null
      await reloadAll()
    } catch (e) {
      // Install/reload failures are tab-wide (not validation) — route them to
      // the parent's error banner rather than the local "Validation failed:"
      // line, which is reserved for chooseArchive validation failures.
      onError(e instanceof Error ? e.message : String(e))
    } finally {
      installing = false
    }
  }
</script>

<div class="flex flex-wrap items-center gap-2">
  <button
    type="button"
    onclick={chooseArchive}
    disabled={validating || installing}
    class="bg-accent-primary-glow border border-accent-primary-start/30 text-accent-primary-start font-label-sm-bold px-3 py-2 rounded flex items-center gap-2 hover:brightness-110 hover:border-accent-primary-start transition-all cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
  >
    <span class="material-symbols-outlined text-icon-lg" aria-hidden="true"
      >file_download</span
    >
    Install from .silt-plugin…
  </button>
  {@render actions?.()}
</div>

{#if validating}
  <p class="text-text-muted text-type-sm font-body-md mt-3 animate-pulse">
    Validating…
  </p>
{/if}

{#if previewError}
  <p class="text-error text-type-sm font-body-md mt-3" role="alert">
    Validation failed: {previewError}
  </p>
{/if}

{#if preview}
  <div
    class="mt-3 p-3 rounded-lg bg-surface-panel border border-surface-panel-border"
  >
    <div class="flex items-center gap-2 mb-1">
      <span class="font-label-sm-bold text-text-primary"
        >{preview.manifest.name}</span
      >
      <span class="text-type-2xs text-text-muted"
        >v{preview.manifest.version || '0.0.0'}</span
      >
      <span class="text-type-2xs text-text-muted">· {preview.manifest.id}</span>
    </div>
    {#if preview.manifest.description}
      <p class="text-text-muted text-type-sm font-body-md mb-2">
        {preview.manifest.description}
      </p>
    {/if}
    {#if preview.warnings && preview.warnings.length > 0}
      <ul class="mb-2 space-y-0.5" aria-label="Warnings">
        {#each preview.warnings as w, wi (wi)}
          <li
            class="text-status-warn text-type-xs font-body-md flex items-start gap-1"
          >
            <span
              class="material-symbols-outlined text-type-md mt-0.5"
              aria-hidden="true">warning</span
            >
            {w}
          </li>
        {/each}
      </ul>
    {/if}
    {#if preview.manifest.capabilities && Object.keys(preview.manifest.capabilities).length > 0}
      <div class="mb-2">
        <h5
          class="text-text-muted text-type-2xs font-label-sm-bold uppercase tracking-widest mb-1"
          id="install-caps"
        >
          Requests capabilities
        </h5>
        <ul class="space-y-0.5" aria-labelledby="install-caps">
          {#each Object.keys(preview.manifest.capabilities) as cap (cap)}
            <li
              class="text-type-xs text-text-primary font-body-md flex items-center gap-1.5"
            >
              <span
                class="material-symbols-outlined text-type-md text-accent-primary-start/70"
                aria-hidden="true">key</span
              >
              {capabilityLabels[cap] ?? cap}{qualifierLabel(
                preview.manifest.capabilities[cap]
              )}
            </li>
          {/each}
        </ul>
        <p class="text-text-muted text-type-2xs mt-1 italic">
          You can grant or revoke each capability after install.
        </p>
      </div>
    {/if}
    <button
      type="button"
      onclick={confirmInstall}
      disabled={installing}
      class="bg-accent-primary-start/20 border border-accent-primary-start/40 text-accent-primary-start font-label-sm-bold px-3 py-1.5 rounded hover:brightness-110 transition-all cursor-pointer disabled:opacity-50"
    >
      {installing ? 'Installing…' : 'Install'}
    </button>
  </div>
{/if}
