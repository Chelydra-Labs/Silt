<script lang="ts">
  // "Turn into" conversion preview. Shown when a TYPED page switches to a
  // different type (or is cleared) so the user can see how each existing value
  // fares under the new schema before committing. Blocking modal — unlike the
  // properties panel, this is `aria-modal="true"` with a Tab focus trap.
  //
  // The backend's TurnIntoPage keeps every value as-is and re-validates against
  // the new schema (keep-and-flag); this dialog is an advisory preview of that
  // outcome, plus an opt-in to clear orphaned properties. Type rewrite + orphan
  // clears land in one atomic write (TurnIntoPage) so a failed switch cannot
  // leave values deleted under the old type.
  import { tick } from 'svelte'
  import { GetType, GetPageProperties } from '../../bindings/silt/app.js'
  import { coerceIPCError } from '../lib/ipcError'
  import { classifyPair } from './typeCompat'
  import type {
    PageLocator,
    PagePropertyValue,
    PropertyDef,
    PropertyType,
    TypeDef
  } from './types'

  type RowKind = 'auto' | 'coerced' | 'flagged' | 'orphaned' | 'new'

  interface MappingRow {
    name: string
    label: string
    kind: RowKind
    oldType: PropertyType | ''
    newType: PropertyType | ''
    preview: string
  }

  interface Props {
    open: boolean
    locator: PageLocator
    oldTypeId: string
    /** '' = clearing the type. */
    newTypeId: string
    newTypeLabel: string
    onConfirm: (orphanNames: string[], clearOrphaned: boolean) => void
    onCancel: () => void
  }

  let {
    open,
    locator,
    oldTypeId,
    newTypeId,
    newTypeLabel,
    onConfirm,
    onCancel
  }: Props = $props()

  let rows = $state<MappingRow[]>([])
  let orphanNames = $state<string[]>([])
  let loading = $state(true)
  let loadError = $state('')
  let clearOrphaned = $state(false)
  let dialogRef = $state<HTMLDivElement | null>(null)

  const KIND_LABEL: Record<RowKind, string> = {
    auto: 'Carries over',
    coerced: 'Compatible',
    flagged: 'Will be flagged',
    orphaned: "Won't appear",
    new: 'New (empty)'
  }

  function valuePreview(v: unknown): string {
    if (Array.isArray(v)) return v.map(String).join(', ')
    if (v === null || v === undefined) return ''
    if (typeof v === 'boolean') return v ? 'Yes' : 'No'
    if (typeof v === 'string' || typeof v === 'number') return String(v)
    return JSON.stringify(v)
  }

  async function build(): Promise<void> {
    loading = true
    loadError = ''
    rows = []
    orphanNames = []
    try {
      const [oldTypeRes, newTypeRes, currentValues] = await Promise.all([
        GetType(oldTypeId).catch(() => null),
        newTypeId
          ? GetType(newTypeId).catch(() => null)
          : Promise.resolve(null),
        GetPageProperties(locator.notebook, locator.section, locator.page)
      ])
      const oldType = oldTypeRes as TypeDef | null
      const newType = newTypeRes as TypeDef | null
      const values = (currentValues as PagePropertyValue[] | null) ?? []

      // Both structures are local scratch built fresh per build() call and
      // never read reactively, so SvelteMap/SvelteSet would only add overhead.
      // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local scratch map
      const newProps = new Map<string, PropertyDef>()
      for (const p of newType?.properties ?? []) newProps.set(p.name, p)
      const oldHas = new Set<string>(
        (oldType?.properties ?? []).map((p) => p.name)
      )

      const out: MappingRow[] = []
      const orphans: string[] = []
      for (const v of values) {
        if (!v.isSet) continue
        const newPdef = newProps.get(v.name)
        const oldT = (v.type || '') as PropertyType
        if (!newPdef) {
          // Clearing the type, or the new schema lacks this property.
          out.push({
            name: v.name,
            label: v.label || v.name,
            kind: 'orphaned',
            oldType: oldT,
            newType: '',
            preview: valuePreview(v.value)
          })
          orphans.push(v.name)
          continue
        }
        const kind = classifyPair(oldT, newPdef.type)
        out.push({
          name: v.name,
          label: v.label || v.name,
          kind,
          oldType: oldT,
          newType: newPdef.type,
          preview: valuePreview(v.value)
        })
      }
      // New-only properties: in the new schema but not the old.
      for (const p of newType?.properties ?? []) {
        if (!oldHas.has(p.name) && !values.some((v) => v.name === p.name)) {
          out.push({
            name: p.name,
            label: p.label || p.name,
            kind: 'new',
            oldType: '',
            newType: p.type,
            preview: ''
          })
        }
      }
      rows = out
      orphanNames = orphans
    } catch (e) {
      loadError = coerceIPCError(e).message
    } finally {
      loading = false
    }
  }

  // Rebuild whenever the dialog opens (locators/types may change between opens).
  $effect(() => {
    if (!open) return
    void oldTypeId
    void newTypeId
    void locator.notebook
    void locator.section
    void locator.page
    void build()
  })

  // Reset the opt-in checkbox each open so a prior choice doesn't leak.
  $effect(() => {
    if (!open) clearOrphaned = false
  })

  // ---- Focus trap (mirrors ConfirmDialog) --------------------------------
  let previouslyFocused: HTMLElement | null = null
  const FOCUSABLE =
    'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])'

  function focusableEls(): HTMLElement[] {
    if (!dialogRef) return []
    return Array.from(dialogRef.querySelectorAll<HTMLElement>(FOCUSABLE))
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onCancel()
      return
    }
    if (e.key === 'Tab' && dialogRef) {
      const els = focusableEls()
      if (els.length === 0) return
      const first = els[0]
      const last = els[els.length - 1]
      const active = document.activeElement as HTMLElement | null
      if (e.shiftKey) {
        if (active === first || !dialogRef.contains(active)) {
          e.preventDefault()
          last.focus()
        }
      } else if (active === last || !dialogRef.contains(active)) {
        e.preventDefault()
        first.focus()
      }
    }
  }

  $effect(() => {
    if (!open) return
    previouslyFocused = document.activeElement as HTMLElement | null
    window.addEventListener('keydown', handleKeydown, true)
    void tick().then(() => dialogRef?.focus())
    return () => {
      window.removeEventListener('keydown', handleKeydown, true)
      if (previouslyFocused?.isConnected) previouslyFocused.focus?.()
      previouslyFocused = null
    }
  })

  let clearing = $derived(newTypeId === '')
  let title = $derived(clearing ? 'Remove type' : `Turn into ${newTypeLabel}`)
  let hasOrphans = $derived(orphanNames.length > 0)

  function confirm(): void {
    onConfirm(orphanNames, clearOrphaned)
  }
</script>

{#if open}
  <div
    class="fixed inset-0 z-[200] flex items-center justify-center bg-black/40 backdrop-blur-[2px]"
    data-focus-trap
  >
    <button
      type="button"
      tabindex="-1"
      aria-label="Cancel"
      class="backdrop-click"
      onclick={onCancel}
    ></button>
    <div
      bind:this={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-labelledby="turninto-title"
      tabindex="-1"
      class="surface"
    >
      <header class="head">
        <h2 id="turninto-title" class="title">{title}</h2>
        <p class="subtitle">
          {#if clearing}
            All property values will be orphaned (kept in frontmatter but
            hidden).
          {:else}
            Review how existing values carry over to <strong
              >{newTypeLabel}</strong
            >.
          {/if}
        </p>
      </header>

      {#if loading}
        <div class="state" role="status" aria-live="polite">
          Building preview…
        </div>
      {:else if loadError}
        <div class="state error" role="alert">{loadError}</div>
      {:else}
        <ul class="matrix" role="list">
          {#each rows as row (row.name)}
            <li class="row kind-{row.kind}">
              <span class="badge" aria-label={KIND_LABEL[row.kind]}>
                {KIND_LABEL[row.kind]}
              </span>
              <div class="row-main">
                <span class="row-label">{row.label}</span>
                <span class="row-types">
                  {row.oldType || '—'} → {row.newType || '—'}
                </span>
                {#if row.preview}
                  <span class="row-value">{row.preview}</span>
                {/if}
              </div>
            </li>
          {/each}
          {#if rows.length === 0}
            <li class="row-empty">No properties to map.</li>
          {/if}
        </ul>

        <label class="opt" class:disabled={!hasOrphans}>
          <input
            type="checkbox"
            checked={clearOrphaned}
            disabled={!hasOrphans}
            onchange={(e) => (clearOrphaned = e.currentTarget.checked)}
          />
          <span>
            Clear {orphanNames.length} orphaned
            {orphanNames.length === 1 ? 'value' : 'values'}
            {#if !hasOrphans}(none){/if}
          </span>
        </label>
      {/if}

      <footer class="foot">
        <button type="button" class="btn ghost" onclick={onCancel}
          >Cancel</button
        >
        <button
          type="button"
          class="btn primary"
          onclick={confirm}
          disabled={loading}
        >
          {clearing ? 'Remove type' : `Turn into ${newTypeLabel}`}
        </button>
      </footer>
    </div>
  </div>
{/if}

<style>
  .backdrop-click {
    position: absolute;
    inset: 0;
    border: 0;
    background: transparent;
    cursor: default;
    padding: 0;
  }
  .surface {
    position: relative;
    width: 32rem;
    max-width: calc(100vw - 2rem);
    max-height: calc(100vh - 4rem);
    display: flex;
    flex-direction: column;
    background: var(--color-surface-modal);
    border: 1px solid var(--color-surface-modal-border);
    border-radius: 0.75rem;
    box-shadow: var(--shadow-lg);
    color: var(--color-surface-modal-text);
    overflow: hidden;
    outline: none;
  }
  .surface:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 2px;
  }
  .head {
    padding: 1rem 1.25rem 0.75rem;
    border-bottom: 1px solid var(--color-surface-modal-border);
  }
  .title {
    margin: 0;
    font-family: var(--font-headline, sans-serif);
    font-size: var(--text-type-lg);
    font-weight: 600;
    color: var(--color-text-primary);
  }
  .subtitle {
    margin: 0.35rem 0 0;
    color: var(--color-text-muted);
    font-size: var(--text-type-sm);
  }
  .state {
    padding: 2rem;
    text-align: center;
    color: var(--color-text-muted);
  }
  .state.error {
    color: var(--color-error-fg);
  }
  .matrix {
    list-style: none;
    margin: 0;
    padding: 0.5rem;
    overflow-y: auto;
    min-height: 0;
    flex: 1 1 auto;
  }
  .row {
    display: flex;
    align-items: flex-start;
    gap: 0.5rem;
    padding: 0.4rem 0.5rem;
    border-radius: 0.375rem;
    border-left: 3px solid transparent;
  }
  .row-main {
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    min-width: 0;
  }
  .row-label {
    color: var(--color-text-primary);
    font-size: var(--text-type-sm);
    font-weight: 600;
  }
  .row-types {
    color: var(--color-text-muted);
    font-family: var(--font-mono, monospace);
    font-size: var(--text-type-2xs);
  }
  .row-value {
    color: var(--color-text-muted);
    font-size: var(--text-type-2xs);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .row-empty {
    padding: 1rem;
    text-align: center;
    color: var(--color-text-muted);
    font-size: var(--text-type-sm);
  }
  .badge {
    flex: 0 0 auto;
    padding: 0.1rem 0.45rem;
    border-radius: 9999px;
    font-size: var(--text-type-2xs);
    font-weight: 600;
    border: 1px solid currentColor;
    white-space: nowrap;
    margin-top: 0.1rem;
  }
  .kind-auto {
    border-left-color: var(--color-status-success);
  }
  .kind-auto .badge {
    color: var(--color-status-success);
  }
  .kind-coerced {
    border-left-color: var(--color-status-warn);
  }
  .kind-coerced .badge {
    color: var(--color-status-warn);
  }
  .kind-flagged {
    border-left-color: var(--color-status-danger);
  }
  .kind-flagged .badge {
    color: var(--color-status-danger);
  }
  .kind-orphaned {
    border-left-color: var(--color-text-muted);
    opacity: 0.75;
  }
  .kind-orphaned .badge {
    color: var(--color-text-muted);
  }
  .kind-new .badge {
    color: var(--color-text-muted);
    border-style: dashed;
  }
  .opt {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.6rem 1.25rem;
    border-top: 1px solid var(--color-surface-modal-border);
    font-size: var(--text-type-sm);
    color: var(--color-text-primary);
    cursor: pointer;
  }
  .opt.disabled {
    color: var(--color-text-muted);
    cursor: default;
  }
  .foot {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    padding: 0.75rem 1.25rem;
    border-top: 1px solid var(--color-surface-modal-border);
  }
  .btn {
    padding: 0.4rem 1rem;
    border-radius: 0.5rem;
    font-size: var(--text-type-sm);
    font-weight: 600;
    font-family: var(--font-mono, monospace);
    cursor: pointer;
    border: 1px solid transparent;
  }
  .btn.ghost {
    background: transparent;
    color: var(--color-text-muted);
    border-color: var(--color-surface-panel-border);
  }
  .btn.ghost:hover {
    color: var(--color-text-primary);
    background: var(--color-hover);
  }
  .btn.primary {
    background: var(--color-accent-primary-glow);
    color: var(--color-accent-primary-start);
    border-color: var(--color-accent-primary-start);
  }
  .btn.primary:hover {
    background: var(--color-hover);
  }
  .btn:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
</style>
