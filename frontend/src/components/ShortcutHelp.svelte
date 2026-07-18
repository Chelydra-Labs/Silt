<script lang="ts">
  import { onMount } from 'svelte'
  import { settings } from '../settings/store.svelte'
  import {
    SHORTCUT_GROUPS,
    shortcutActionDefinitions,
    shortcutBinding
  } from '../settings/shortcutActions'

  interface Props {
    onClose: () => void
  }
  let { onClose }: Props = $props()

  let dialog = $state<HTMLDivElement | null>(null)
  let closeButton = $state<HTMLButtonElement | null>(null)
  let previousFocus: HTMLElement | null = null
  let hotkeys = $derived(settings.config?.hotkeys ?? {})
  let actions = $derived(shortcutActionDefinitions(hotkeys))

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault()
      onClose()
      return
    }
    if (event.key !== 'Tab' || !dialog) return
    const focusable = Array.from(
      dialog.querySelectorAll<HTMLElement>(
        'button:not([disabled]), [tabindex="0"]'
      )
    )
    if (!focusable.length) return
    const current = focusable.indexOf(document.activeElement as HTMLElement)
    const next = event.shiftKey
      ? (current - 1 + focusable.length) % focusable.length
      : (current + 1) % focusable.length
    event.preventDefault()
    focusable[next]?.focus()
  }

  onMount(() => {
    previousFocus = document.activeElement as HTMLElement | null
    closeButton?.focus()
    return () => previousFocus?.focus()
  })
</script>

<div
  class="fixed inset-0 z-[195] flex items-center justify-center p-4 bg-black/45 backdrop-blur-[3px]"
>
  <button
    type="button"
    tabindex="-1"
    aria-label="Close keyboard shortcuts"
    class="absolute inset-0 border-none bg-transparent cursor-default"
    onclick={onClose}
  ></button>
  <div
    bind:this={dialog}
    role="dialog"
    tabindex="-1"
    aria-modal="true"
    aria-labelledby="shortcut-help-title"
    onkeydown={handleKeydown}
    class="relative w-full max-w-3xl max-h-[82vh] overflow-hidden rounded-2xl border border-surface-modal-border glass-palette glass-palette-strong shadow-2xl flex flex-col"
  >
    <header
      class="flex items-center justify-between gap-4 px-5 py-4 border-b border-surface-modal-border"
    >
      <div>
        <h2
          id="shortcut-help-title"
          class="m-0 font-headline-md text-headline-md text-text-primary"
        >
          Keyboard shortcuts
        </h2>
        <p class="m-0 mt-1 text-type-xs text-text-muted">
          Current bindings from this vault. Remap them in Settings.
        </p>
      </div>
      <button
        bind:this={closeButton}
        type="button"
        aria-label="Close keyboard shortcuts"
        title="Close"
        onclick={onClose}
        class="w-9 h-9 rounded-lg border-none bg-transparent text-text-muted hover:text-text-primary hover:bg-hover cursor-pointer focus-visible:ring-2 focus-visible:ring-accent-primary-start"
        ><span class="material-symbols-outlined" aria-hidden="true">close</span
        ></button
      >
    </header>
    <div class="overflow-y-auto custom-scrollbar p-5 grid gap-5 sm:grid-cols-2">
      {#each SHORTCUT_GROUPS as group (group)}
        {@const groupActions = actions.filter(
          (action) => action.group === group
        )}
        {#if groupActions.length}
          <section aria-labelledby={`shortcut-group-${group}`}>
            <h3
              id={`shortcut-group-${group}`}
              class="m-0 mb-2 text-type-2xs uppercase tracking-[0.16em] text-accent-primary-start font-label-sm-bold"
            >
              {group}
            </h3>
            <dl
              class="m-0 rounded-xl border border-surface-panel-border overflow-hidden"
            >
              {#each groupActions as action (action.id)}
                {@const binding = shortcutBinding(action.id, hotkeys)}
                <div
                  class="flex gap-3 items-center px-3 py-2 border-b last:border-b-0 border-surface-panel-border/70"
                >
                  <dt class="flex-1 text-type-sm text-text-primary min-w-0">
                    {action.label}
                  </dt>
                  <dd class="m-0 shrink-0">
                    {#if binding}
                      <span class="flex items-center justify-end gap-1.5">
                        {#if action.defaultBinding && binding !== action.defaultBinding}
                          <span class="text-type-3xs text-accent-primary-start"
                            >Remapped</span
                          >
                        {/if}
                        <kbd
                          class="inline-flex px-2 py-1 rounded-md border border-surface-panel-border bg-surface-panel text-type-2xs text-text-muted font-mono whitespace-nowrap"
                          >{binding}</kbd
                        >
                      </span>
                    {:else}
                      <span
                        class="text-type-2xs text-text-muted italic"
                        aria-label={`${action.label} disabled`}>Disabled</span
                      >
                    {/if}
                  </dd>
                </div>
              {/each}
            </dl>
          </section>
        {/if}
      {/each}
    </div>
  </div>
</div>
