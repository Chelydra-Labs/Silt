<script lang="ts">
  // Generic, schema-driven settings form renderer (#103). Renders a form from a
  // plugin's declarative SettingSchema[] and writes changes through
  // UpdatePluginSetting (vault-scoped atomic write). Replaces the bespoke
  // read-only pre/JSON dump that each first-party plugin hand-rolled.
  import type { SettingSchema } from '../../plugins/sdk'
  import { updatePluginSetting } from '../../settings/store.svelte'

  interface Props {
    pluginID: string
    schema: SettingSchema[]
    values: Record<string, any>
    /** Optional notebook scope label (vault vs this-notebook). */
    scopeLabel?: string
  }
  let { pluginID, schema, values, scopeLabel = 'vault' }: Props = $props()

  // Local draft so the user can edit multiple fields and save atomically.
  // svelte-ignore state_referenced_locally — intentional initial snapshot of
  // the upstream prop; the $effect below re-syncs on subsequent changes.
  let draft = $state<Record<string, any>>({ ...values })
  let dirty = $derived(JSON.stringify(draft) !== JSON.stringify(values))
  let saving = $state(false)
  let error = $state('')

  // Re-sync draft when the upstream values change (e.g. external config reload)
  // — but only if there are no unsaved edits, so we never clobber the user's
  // in-flight changes.
  $effect(() => {
    // Track `values` reactively.
    const upstream = values
    if (!dirty) {
      draft = { ...upstream }
    }
  })

  async function save() {
    saving = true
    error = ''
    try {
      for (const field of schema) {
        if (
          JSON.stringify(draft[field.key]) !== JSON.stringify(values[field.key])
        ) {
          await updatePluginSetting(pluginID, field.key, draft[field.key])
        }
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      saving = false
    }
  }

  function revert() {
    draft = { ...values }
    error = ''
  }

  function fieldLabel(t: string): string {
    const map: Record<string, string> = {
      string: 'text',
      number: 'number',
      bool: 'checkbox',
      select: 'list',
      color: 'color',
      keymap: 'shortcut',
      list: 'comma-separated list'
    }
    return map[t] ?? t
  }
</script>

{#if schema.length === 0}
  <p class="text-text-muted text-[11px] font-body-md italic">No settings.</p>
{:else}
  <div class="space-y-3">
    {#if scopeLabel}
      <p
        class="text-text-muted text-[10px] font-label-sm uppercase tracking-wider"
      >
        Editing {scopeLabel} settings
      </p>
    {/if}
    {#each schema as field (field.key)}
      <div class="flex flex-col gap-1">
        <label
          for="setting-{pluginID}-{field.key}"
          class="text-text-primary text-[11px] font-label-sm-bold"
        >
          {field.label}
        </label>
        {#if field.type === 'bool'}
          <button
            type="button"
            role="switch"
            aria-checked={!!draft[field.key]}
            aria-label={field.label}
            id="setting-{pluginID}-{field.key}"
            class="toggle-switch"
            onclick={() => (draft[field.key] = !draft[field.key])}
          >
            <span class="material-symbols-outlined text-[18px]">
              {draft[field.key] ? 'toggle_on' : 'toggle_off'}
            </span>
          </button>
        {:else if field.type === 'select'}
          <select
            id="setting-{pluginID}-{field.key}"
            class="bg-void border border-border-muted rounded px-2 py-1 text-text-primary text-[12px] font-body-md"
            bind:value={draft[field.key]}
          >
            {#each field.options ?? [] as opt}
              <option value={opt}>{opt}</option>
            {/each}
          </select>
        {:else if field.type === 'color'}
          <input
            type="color"
            id="setting-{pluginID}-{field.key}"
            class="w-12 h-8 rounded border border-border-muted bg-transparent cursor-pointer"
            bind:value={draft[field.key]}
          />
        {:else if field.type === 'list'}
          <input
            type="text"
            id="setting-{pluginID}-{field.key}"
            class="bg-void border border-border-muted rounded px-2 py-1 text-text-primary text-[12px] font-body-md w-full"
            placeholder={fieldLabel(field.type)}
            value={Array.isArray(draft[field.key])
              ? draft[field.key].join(', ')
              : ''}
            oninput={(e) => {
              draft[field.key] = (e.currentTarget as HTMLInputElement).value
                .split(',')
                .map((s) => s.trim())
                .filter(Boolean)
            }}
          />
        {:else if field.type === 'keymap'}
          <input
            type="text"
            id="setting-{pluginID}-{field.key}"
            class="bg-void border border-border-muted rounded px-2 py-1 text-text-primary text-[12px] font-body-md w-full font-mono"
            placeholder="e.g. Ctrl+Shift+A"
            bind:value={draft[field.key]}
            onkeydown={(e) => {
              // Capture the key combination and display it.
              e.preventDefault()
              const parts: string[] = []
              if (e.ctrlKey) parts.push('Ctrl')
              if (e.shiftKey) parts.push('Shift')
              if (e.altKey) parts.push('Alt')
              if (e.metaKey) parts.push('Meta')
              const key = e.key
              if (key && !['Control', 'Shift', 'Alt', 'Meta'].includes(key)) {
                parts.push(key.length === 1 ? key.toUpperCase() : key)
              }
              if (parts.length > 0) draft[field.key] = parts.join('+')
            }}
          />
        {:else if field.type === 'number'}
          <input
            type="number"
            id="setting-{pluginID}-{field.key}"
            class="bg-void border border-border-muted rounded px-2 py-1 text-text-primary text-[12px] font-body-md w-full"
            placeholder={fieldLabel(field.type)}
            min={field.min}
            max={field.max}
            bind:value={draft[field.key]}
          />
        {:else}
          <input
            type="text"
            id="setting-{pluginID}-{field.key}"
            class="bg-void border border-border-muted rounded px-2 py-1 text-text-primary text-[12px] font-body-md w-full"
            placeholder={fieldLabel(field.type)}
            minlength={field.minLength}
            maxlength={field.maxLength}
            bind:value={draft[field.key]}
          />
        {/if}
        {#if field.help}
          <span class="text-text-muted text-[10px] font-body-md"
            >{field.help}</span
          >
        {/if}
      </div>
    {/each}

    {#if error}
      <p class="text-error text-[11px] font-body-md">{error}</p>
    {/if}
    {#if dirty}
      <div class="flex gap-2 pt-1">
        <button
          onclick={save}
          disabled={saving}
          class="bg-accent-primary-start/20 border border-accent-primary-start/40 text-accent-primary-start font-label-sm-bold px-3 py-1 rounded hover:brightness-110 cursor-pointer disabled:opacity-50 text-[11px]"
        >
          {saving ? 'Saving…' : 'Save'}
        </button>
        <button
          onclick={revert}
          disabled={saving}
          class="text-text-muted hover:text-text-primary border border-border-muted font-label-sm-bold px-3 py-1 rounded cursor-pointer disabled:opacity-50 text-[11px]"
        >
          Revert
        </button>
      </div>
    {/if}
  </div>
{/if}
