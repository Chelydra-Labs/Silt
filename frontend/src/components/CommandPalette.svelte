<script lang="ts">
  import { onMount } from 'svelte'
  import {
    getSlashCommands,
    type SlashCommand
  } from '../lib/editor/slash-registry'
  import { rankSlashCommands } from '../lib/editor/slashRanking'
  import { settings } from '../settings/store.svelte'
  import { resolveHotkeyDisplay } from '../settings/hotkeys'

  interface Props {
    onSelect: (commandId: string) => void
    onClose: () => void
    query?: string
    style?: string
    /** Command ids to hide (e.g. feature opt-outs like math when disabled). */
    exclude?: string[]
    /**
     * The editor textbox (ProseMirror `.ProseMirror`) that controls this
     * palette. The palette is a sibling of the editor, not a child, so the
     * `aria-activedescendant` / `aria-controls` / `aria-expanded` semantics
     * must be projected onto the focused textbox for screen readers to track
     * the active option (ARIA 1.2 §4.3.2 control-relationship exemption).
     */
    textboxEl?: HTMLElement | null
  }

  let {
    onSelect,
    onClose,
    query = '',
    style = '',
    exclude = [],
    textboxEl = null
  }: Props = $props()

  // Stable ids so the controlling textbox can point at the listbox + active
  // option via aria-controls / aria-activedescendant.
  const PALETTE_ID = 'silt-slash-palette'
  const optionId = (idx: number) => `${PALETTE_ID}-opt-${idx}`

  let selectedIdx = $state(0)
  let containerEl = $state<HTMLDivElement | null>(null)

  // The command list is the union of built-ins + plugin-registered commands,
  // sourced from the slash-command registry (#110).
  const allCommands = getSlashCommands()

  // Hotkey bindings live in config and may be remapped per-vault; read them
  // live so the right-aligned hint tracks the user's actual keymap. A `hotkey`
  // action that resolves to '' (absent or disabled) renders no hint.
  let hotkeys = $derived(settings.config?.hotkeys ?? {})

  function hintFor(cmd: SlashCommand): string {
    if (cmd.hotkey) return resolveHotkeyDisplay(cmd.hotkey, hotkeys)
    return ''
  }

  // Filter then rank by the query prop reactively. Ranking is a pure, tested
  // function (#585) so the ordering is deterministic and independent of
  // registry insertion order.
  let filteredCommands = $derived.by(() => {
    const pool = exclude.length
      ? allCommands.filter((c) => !exclude.includes(c.id))
      : allCommands
    return rankSlashCommands(pool, query)
  })

  // Reset to the top-ranked match whenever the query changes so the active
  // option tracks the new ranking (#585). This reads only `query`, so Arrow
  // Up/Down — which write selectedIdx in handleKeyDown — do NOT retrigger it;
  // an explicitly-moved selection survives until the user types again. Without
  // this reset, typing after arrowing down leaves the highlight on a stale
  // index and Enter runs whatever command now occupies it instead of the new
  // top-ranked match.
  $effect(() => {
    void query
    selectedIdx = 0
  })

  // Scroll active item into view.
  $effect(() => {
    if (containerEl && selectedIdx !== -1) {
      const activeEl = containerEl.querySelector('[data-active-cmd="true"]')
      if (activeEl && typeof activeEl.scrollIntoView === 'function') {
        activeEl.scrollIntoView({ block: 'nearest' })
      }
    }
  })

  // Project listbox semantics onto the controlling textbox. The textbox holds
  // DOM focus (it is the contenteditable the user types into); the active
  // option is announced only while these attributes point from textbox →
  // listbox → option. Safari/VoiceOver historically does not honour
  // aria-activedescendant on a textbox role — if that regresses, temporarily
  // applying role="combobox" to the textbox while open is the documented fix.
  $effect(() => {
    const tb = textboxEl
    if (!tb) return
    tb.setAttribute('aria-controls', PALETTE_ID)
    tb.setAttribute('aria-expanded', 'true')
    tb.setAttribute('aria-autocomplete', 'list')
    const active =
      filteredCommands.length > 0 && filteredCommands[selectedIdx]
        ? optionId(selectedIdx)
        : ''
    if (active) tb.setAttribute('aria-activedescendant', active)
    else tb.removeAttribute('aria-activedescendant')
  })

  function handleKeyDown(e: KeyboardEvent) {
    // With no matches, swallow Enter + arrows so the keystroke does not fall
    // through to the editor (Enter would insert a newline / the literal
    // query). Only Escape closes.
    if (filteredCommands.length === 0) {
      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        onClose()
      } else if (
        e.key === 'Enter' ||
        e.key === 'ArrowDown' ||
        e.key === 'ArrowUp'
      ) {
        e.preventDefault()
        e.stopPropagation()
      }
      return
    }

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      e.stopPropagation()
      selectedIdx = (selectedIdx + 1) % filteredCommands.length
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      e.stopPropagation()
      selectedIdx =
        (selectedIdx - 1 + filteredCommands.length) % filteredCommands.length
    } else if (e.key === 'Enter') {
      e.preventDefault()
      e.stopPropagation()
      if (filteredCommands[selectedIdx]) {
        onSelect(filteredCommands[selectedIdx].id)
      }
    } else if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
    }
  }

  onMount(() => {
    window.addEventListener('keydown', handleKeyDown, true)
    return () => {
      window.removeEventListener('keydown', handleKeyDown, true)
      const tb = textboxEl
      if (tb) {
        tb.removeAttribute('aria-controls')
        tb.removeAttribute('aria-expanded')
        tb.removeAttribute('aria-autocomplete')
        tb.removeAttribute('aria-activedescendant')
      }
    }
  })
</script>

<!-- Command Palette Container (Frosted glass). role=listbox + a stable id so
     the editor textbox can aria-controls / aria-activedescendant into it.
     data-slash-palette is the click-outside guard marker (decoupled from the
     .glass-palette visual class so restyling cannot break dismissal). -->
<div
  bind:this={containerEl}
  id={PALETTE_ID}
  role="listbox"
  aria-label="Slash commands"
  data-slash-palette
  class="w-64 glass-palette border border-surface-popover-border rounded shadow-2xl z-[100] overflow-hidden py-2 scale-100 origin-top-left transition-transform custom-scrollbar"
  style="backdrop-filter: blur(12px) saturate(140%); background: color-mix(in srgb, var(--color-surface-popover) 85%, transparent); max-height: 280px; overflow-y: auto; {style}"
>
  {#if filteredCommands.length === 0}
    <div class="px-4 py-3 text-xs text-text-muted text-center select-none">
      No matching commands
    </div>
  {:else}
    {#each filteredCommands as cmd, idx (cmd.id)}
      {#if cmd.pluginID && (idx === 0 || !filteredCommands[idx - 1].pluginID)}
        <div
          class="px-3 py-1.5 text-type-2xs text-text-muted font-label-sm-bold uppercase tracking-widest border-t border-surface-popover-border mt-1 pt-2 select-none"
        >
          Plugins
        </div>
      {:else if !cmd.pluginID && idx === 0}
        <div
          class="px-3 py-1.5 text-type-2xs text-text-muted font-label-sm-bold uppercase tracking-widest border-b border-surface-popover-border mb-1 select-none"
        >
          Commands
        </div>
      {/if}
      <button
        type="button"
        role="option"
        id={optionId(idx)}
        aria-selected={idx === selectedIdx}
        onclick={() => onSelect(cmd.id)}
        class="slash-palette-option flex items-center gap-3 px-4 py-2 w-full text-left transition-colors font-body-md border-none focus:outline-none cursor-pointer"
        class:bg-accent-primary-glow={idx === selectedIdx}
        class:text-accent-primary-start={idx === selectedIdx}
        class:text-text-primary={idx !== selectedIdx}
        data-active-cmd={idx === selectedIdx}
        onmouseenter={() => (selectedIdx = idx)}
      >
        <span class="material-symbols-outlined text-icon-lg select-none"
          >{cmd.icon ?? 'extension'}</span
        >
        <div class="flex-1 flex flex-col min-w-0">
          <span class="font-label-sm-bold text-label-sm">{cmd.label}</span>
          {#if cmd.description}
            <span class="text-type-2xs text-text-muted truncate"
              >{cmd.description}</span
            >
          {/if}
        </div>
        {#if hintFor(cmd)}
          <span class="text-type-2xs text-text-muted select-none"
            >{hintFor(cmd)}</span
          >
        {:else if cmd.pluginID}
          <span class="text-type-3xs text-text-muted select-none uppercase"
            >{cmd.pluginID}</span
          >
        {/if}
      </button>
    {/each}
  {/if}
</div>

<!-- Visually-hidden live region: announces the match count + empty state so a
     screen reader hears how many options are available without reading every
     row. role=status implies aria-live=polite + aria-atomic=true. -->
<div role="status" aria-live="polite" class="slash-status">
  {#if filteredCommands.length === 0}
    No matching commands
  {:else}
    {filteredCommands.length} matching command{filteredCommands.length === 1
      ? ''
      : 's'}
  {/if}
</div>

<style>
  /* Keyboard-focus indicator on the active option, distinct from the hover
     tint and theme-aware via the documented --color-border-focus token
     (ARCHITECTURE §4.4). */
  .slash-palette-option[aria-selected='true'] {
    box-shadow: inset 0 0 0 2px var(--color-border-focus, currentColor);
  }

  /* Visually hidden but available to assistive tech. */
  .slash-status {
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
</style>
