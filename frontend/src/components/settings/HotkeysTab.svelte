<script lang="ts">
  import { untrack } from 'svelte'
  import {
    settings,
    saveConfig,
    reloadFromBackend,
    appendDismissedTip
  } from '../../settings/store.svelte'
  import type { SystemConfig } from '../../settings/store.svelte'
  import { parseHotkey } from '../../settings/hotkeys'
  import HotkeyCaptureInput from './HotkeyCaptureInput.svelte'
  import HotkeysDefaultsNotice from './HotkeysDefaultsNotice.svelte'
  import {
    SHORTCUT_ACTIONS,
    shortcutBinding
  } from '../../settings/shortcutActions'

  interface Props {
    ringAnchor?: string | null
  }
  let { ringAnchor = null }: Props = $props()

  // v1 default-keymap migration notice (#868). The Go normalizer stamps
  // V1_NOTICE into ui.dismissed_tips when it rewrites a legacy chord; the banner
  // stays visible until the user acknowledges (appends V1_ACK via the existing
  // add-only appendDismissedTip helper, so no removal IPC is needed).
  const V1_NOTICE = 'hotkeys_defaults_v1_notice'
  const V1_ACK = 'hotkeys_defaults_v1_ack'
  let v1NoticeDismissed = $derived.by(() => {
    const tips = settings.config?.ui?.dismissed_tips ?? []
    return !tips.includes(V1_NOTICE) || tips.includes(V1_ACK)
  })
  function dismissV1Notice() {
    void appendDismissedTip(V1_ACK)
  }

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

  function changed(): boolean {
    if (!draft || !lastSaved) return false
    return JSON.stringify(draft.hotkeys) !== JSON.stringify(lastSaved.hotkeys)
  }

  let isValid = $derived(
    draft !== null &&
      Object.values(draft.hotkeys).every(
        (h) => h === undefined || h.trim() === '' || parseHotkey(h) !== null
      )
  )

  let hotkeyEntries = $derived(
    draft
      ? Array.from(
          new Set([
            ...Object.keys(draft.hotkeys),
            ...SHORTCUT_ACTIONS.map((action) => action.id)
          ])
        )
          .map(
            (key) =>
              [key, shortcutBinding(key, draft!.hotkeys)] as [string, string]
          )
          .sort((a, b) => a[0].localeCompare(b[0]))
      : []
  )
  let conflicts = $derived.by(() => {
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- non-reactive local/helper
    const byBinding = new Map<string, string[]>()
    for (const [key, value] of hotkeyEntries) {
      const normalized = value.trim().toLocaleLowerCase()
      if (!normalized) continue
      byBinding.set(normalized, [...(byBinding.get(normalized) ?? []), key])
    }
    return new Map(
      [...byBinding.values()]
        .filter((keys) => keys.length > 1)
        .flatMap((keys) =>
          keys.map(
            (key) => [key, keys.filter((other) => other !== key)] as const
          )
        )
    )
  })

  function prettyLabel(key: string): string {
    return (
      SHORTCUT_ACTIONS.find((action) => action.id === key)?.label ??
      key.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
    )
  }

  async function handleSave() {
    if (!draft || conflicts.size) return
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

      <!-- v1 default-keymap migration notice (#868). Surfaces the relocated
           chords to a user reviewing their shortcuts after upgrade. The
           tab-chord text is derived from the resolved hotkey map so the notice
           matches the ShortcutHelp table and the HotkeysTab grid on every
           platform (#863: Linux keeps Ctrl+Tab, Windows uses Ctrl+Alt+Arrow). -->
      <HotkeysDefaultsNotice
        dismissed={v1NoticeDismissed}
        onDismiss={dismissV1Notice}
        hotkeys={draft?.hotkeys ?? {}}
      />

      <!-- Hotkeys Group Card -->
      <div
        id="hotkeys-shortcuts"
        class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-4 {ringAnchor ===
        'hotkeys-shortcuts'
          ? 'ring-2 ring-accent-primary-start transition-shadow'
          : ''}"
      >
        <div class="flex items-center justify-between gap-3 flex-wrap">
          <h4
            class="font-label-sm-bold text-text-primary uppercase tracking-wider text-type-2xs"
          >
            Keyboard Shortcuts
          </h4>
          <span class="text-text-muted text-type-2xs">
            Click a field and press a shortcut. Clear or Backspace to disable.
          </span>
        </div>
        <div class="grid grid-cols-2 gap-x-6 gap-y-3">
          {#each hotkeyEntries as [key, value] (key)}
            <div class="flex flex-col gap-1">
              <span
                id="hotkey-label-{key}"
                class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider truncate"
                title={prettyLabel(key)}
              >
                {prettyLabel(key)}
              </span>
              <HotkeyCaptureInput
                value={value ?? ''}
                label={prettyLabel(key)}
                labelId="hotkey-label-{key}"
                error={conflicts.has(key)
                  ? `Conflicts with ${conflicts
                      .get(key)!
                      .map(prettyLabel)
                      .join(', ')}.`
                  : ''}
                onchange={(next) => {
                  draft!.hotkeys[key] = next
                  touch()
                }}
              />
            </div>
          {/each}
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
        disabled={!changed() ||
          !isValid ||
          conflicts.size > 0 ||
          settings.saving}
        class="px-4 py-2 rounded-lg bg-accent-primary-start/20 border border-accent-primary-start/40 text-accent-primary-start font-label-sm-bold hover:brightness-110 transition-all cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
      >
        {settings.saving ? 'Saving…' : 'Save changes'}
      </button>
    </div>
  </div>
{/if}
