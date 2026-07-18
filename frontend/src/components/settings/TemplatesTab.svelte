<script lang="ts">
  import { onMount } from 'svelte'
  import {
    DeleteUserTemplate,
    FetchPageMarkdown,
    GetTemplate,
    SaveUserTemplate
  } from '../../../bindings/silt/app.js'
  import type * as tpl from '../../../bindings/silt/backend/templates/models.js'
  import {
    loadTemplates,
    setTemplateStatus,
    templateStatus,
    templatesState
  } from '../../templates/store.svelte'
  import ConfirmDialog from '../ConfirmDialog.svelte'

  interface Props {
    activeNotebook: string
    activeSection: string
    activePage: string
  }
  let { activeNotebook, activeSection, activePage }: Props = $props()

  type Draft = {
    schema_version: string
    id: string
    title: string
    description: string
    category: string
    icon: string
    placeholders: tpl.Placeholder[]
    body: string
    source: TemplateSource
    plugin_id: string
  }
  type TemplateSource = 'builtin' | 'disk' | 'plugin'

  let selectedId = $state('')
  let draft = $state<Draft | null>(null)
  let baseline = $state('')
  let loadingBody = $state(false)
  let saving = $state(false)
  let error = $state('')
  let filter = $state('')
  let confirmMode = $state<'discard' | 'delete' | ''>('')
  let pendingAction: (() => void | Promise<void>) | null = null

  let filtered = $derived(
    templatesState.items.filter((item) => {
      const query = filter.trim().toLocaleLowerCase()
      if (!query) return true
      return [item.title, item.id, item.category, item.source, item.plugin_id]
        .filter(Boolean)
        .join(' ')
        .toLocaleLowerCase()
        .includes(query)
    })
  )
  let selectedSummary = $derived(
    templatesState.items.find((item) => item.id === selectedId)
  )
  let readOnly = $derived(!!draft && draft.source !== 'disk')
  let dirty = $derived(!!draft && JSON.stringify(draft) !== baseline)
  let validation = $derived.by(() => {
    if (!draft) return ''
    if (!draft.id.trim()) return 'Template ID is required.'
    if (!/^[a-z0-9][a-z0-9_-]*$/.test(draft.id.trim()))
      return 'Use lowercase letters, numbers, hyphens, or underscores for the ID.'
    if (!draft.title.trim()) return 'Title is required.'
    if (!draft.category.trim()) return 'Category is required.'
    return ''
  })

  onMount(() => {
    if (!templatesState.loading && !templatesState.items.length) {
      void loadTemplates()
    }
  })

  function normalizeSource(source?: string): TemplateSource {
    if (source === 'disk' || source === 'plugin') return source
    return 'builtin'
  }

  function toDraft(template: tpl.Template): Draft {
    return {
      schema_version: template.schema_version || '1',
      id: template.id,
      title: template.title,
      description: template.description ?? '',
      category: template.category || 'General',
      icon: template.icon ?? '',
      placeholders: template.placeholders ?? [],
      body: template.body,
      source: normalizeSource(template.source),
      plugin_id: template.plugin_id ?? ''
    }
  }

  async function selectTemplate(id: string) {
    if (dirty) {
      pendingAction = () => selectTemplate(id)
      confirmMode = 'discard'
      return
    }
    selectedId = id
    draft = null
    baseline = ''
    loadingBody = true
    error = ''
    try {
      const template = await GetTemplate(id)
      draft = toDraft(template)
      baseline = JSON.stringify(draft)
    } catch (caught) {
      error = caught instanceof Error ? caught.message : String(caught)
    } finally {
      loadingBody = false
    }
  }

  function beginBlank() {
    const start = () => {
      selectedId = ''
      draft = {
        schema_version: '1',
        id: '',
        title: '',
        description: '',
        category: 'General',
        icon: 'description',
        placeholders: [],
        body: '',
        source: 'disk',
        plugin_id: ''
      }
      baseline = JSON.stringify(draft)
      error = ''
    }
    if (dirty) {
      pendingAction = start
      confirmMode = 'discard'
    } else start()
  }

  async function beginFromCurrentPage() {
    if (!activeNotebook || !activePage) return
    if (dirty) {
      pendingAction = beginFromCurrentPage
      confirmMode = 'discard'
      return
    }
    loadingBody = true
    error = ''
    try {
      const body = await FetchPageMarkdown(
        activeNotebook,
        activeSection,
        activePage
      )
      const id = activePage
        .toLocaleLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-|-$/g, '')
      draft = {
        schema_version: '1',
        id,
        title: activePage,
        description: '',
        category: 'General',
        icon: 'description',
        placeholders: [],
        body,
        source: 'disk',
        plugin_id: ''
      }
      selectedId = ''
      baseline = JSON.stringify(draft)
    } catch (caught) {
      error = caught instanceof Error ? caught.message : String(caught)
    } finally {
      loadingBody = false
    }
  }

  async function saveDraft() {
    if (!draft || readOnly || validation) return
    saving = true
    error = ''
    const canonical: Draft = {
      ...draft,
      id: draft.id.trim(),
      title: draft.title.trim(),
      description: draft.description.trim(),
      category: draft.category.trim(),
      icon: draft.icon.trim(),
      source: 'disk',
      plugin_id: ''
    }
    try {
      await SaveUserTemplate({
        schema_version: canonical.schema_version,
        id: canonical.id,
        title: canonical.title,
        description: canonical.description || undefined,
        category: canonical.category,
        icon: canonical.icon || undefined,
        placeholders: canonical.placeholders,
        body: canonical.body,
        source: 'disk',
        plugin_id: undefined
      })
      draft = canonical
      selectedId = canonical.id
      baseline = JSON.stringify(canonical)
      setTemplateStatus({
        kind: 'success',
        message: `Saved “${canonical.title}”.`
      })
      await loadTemplates()
    } catch (caught) {
      error = caught instanceof Error ? caught.message : String(caught)
      setTemplateStatus({
        kind: 'error',
        message: 'Template could not be saved.'
      })
    } finally {
      saving = false
    }
  }

  async function duplicateSelected() {
    if (!draft) return
    if (dirty) {
      pendingAction = duplicateSelected
      confirmMode = 'discard'
      return
    }
    const leaf = draft.id.split('/').at(-1) ?? 'template'
    const id = `${leaf.replace(/[^a-z0-9_-]/gi, '-').toLocaleLowerCase()}-copy`
    draft = {
      ...draft,
      id,
      title: `${draft.title} Copy`,
      source: 'disk',
      plugin_id: ''
    }
    selectedId = ''
    baseline = JSON.stringify({ ...draft, id: '', title: '' })
    await saveDraft()
  }

  function requestDelete() {
    if (!draft || draft.source !== 'disk') return
    confirmMode = 'delete'
  }

  async function confirmDialog() {
    if (confirmMode === 'delete' && draft) {
      const id = draft.id
      confirmMode = ''
      try {
        await DeleteUserTemplate(id)
        draft = null
        selectedId = ''
        baseline = ''
        setTemplateStatus({ kind: 'success', message: 'Template deleted.' })
        await loadTemplates()
      } catch (caught) {
        error = caught instanceof Error ? caught.message : String(caught)
        setTemplateStatus({
          kind: 'error',
          message: 'Template could not be deleted.'
        })
      }
      return
    }
    const action = pendingAction
    pendingAction = null
    confirmMode = ''
    baseline = draft ? JSON.stringify(draft) : ''
    await action?.()
  }
</script>

<div class="h-full min-h-0 grid grid-cols-3 max-lg:grid-cols-1">
  <section
    class="col-span-1 min-h-0 flex flex-col border-r max-lg:border-r-0 max-lg:border-b border-surface-panel-border"
    aria-label="Templates"
  >
    <div class="p-4 border-b border-surface-panel-border space-y-3">
      <div class="flex gap-2 flex-wrap">
        <button type="button" class="action primary" onclick={beginBlank}
          >New blank</button
        >
        <button
          type="button"
          class="action"
          disabled={!activeNotebook || !activePage}
          title={!activePage
            ? 'Open a page first'
            : 'Start with the current page markdown'}
          onclick={() => void beginFromCurrentPage()}>From current page</button
        >
      </div>
      <label class="block">
        <span class="sr-only">Filter templates</span>
        <input
          bind:value={filter}
          type="search"
          placeholder="Filter templates…"
          class="field w-full"
        />
      </label>
    </div>
    <div
      class="flex-1 min-h-0 overflow-y-auto custom-scrollbar p-2"
      aria-live="polite"
    >
      {#if templatesState.loading && !templatesState.items.length}
        <p class="empty" role="status">Loading templates…</p>
      {:else if templatesState.loadError}
        <div class="empty" role="alert">
          <p>Templates could not be loaded.</p>
          <button
            class="link"
            type="button"
            onclick={() => void loadTemplates()}>Try again</button
          >
        </div>
      {:else if !filtered.length}
        <p class="empty">No templates match.</p>
      {:else}
        {#each filtered as item (item.id)}
          {@const source = normalizeSource(item.source)}
          <button
            type="button"
            class="template-row"
            class:selected={selectedId === item.id}
            aria-pressed={selectedId === item.id}
            onclick={() => void selectTemplate(item.id)}
          >
            <span
              class="material-symbols-outlined text-icon-lg"
              aria-hidden="true">{item.icon || 'article'}</span
            >
            <span class="min-w-0 flex-1">
              <span class="block truncate font-label-sm-bold text-text-primary"
                >{item.title}</span
              >
              <span class="block truncate text-type-xs text-text-muted"
                >{item.category}</span
              >
            </span>
            <span class="source {source}"
              >{source === 'builtin'
                ? 'Built-in'
                : source === 'plugin'
                  ? item.plugin_id || 'Plugin'
                  : 'User'}</span
            >
          </button>
        {/each}
      {/if}
    </div>
  </section>

  <section
    class="col-span-2 max-lg:col-span-1 min-h-0 flex flex-col"
    aria-label="Template editor"
  >
    {#if loadingBody}
      <p class="empty" role="status">Loading template…</p>
    {:else if !draft}
      <div class="empty flex-1 grid place-content-center text-center">
        <span
          class="material-symbols-outlined text-type-4xl text-accent-primary-start/70"
          aria-hidden="true">article</span
        >
        <p>Select a template, or create a new one.</p>
        {#if error}<p class="message error" role="alert">{error}</p>{/if}
      </div>
    {:else}
      <div
        class="p-4 border-b border-surface-panel-border flex items-center justify-between gap-3"
      >
        <div class="min-w-0">
          <h3 class="m-0 truncate text-text-primary font-headline-md">
            {draft.title || 'Untitled template'}
          </h3>
          <p class="m-0 text-type-xs text-text-muted">
            {readOnly
              ? 'Read-only source — duplicate to edit.'
              : dirty
                ? 'Unsaved changes'
                : 'Saved'}
          </p>
        </div>
        <div class="flex gap-2">
          <button
            type="button"
            class="action"
            onclick={() => void duplicateSelected()}>Duplicate</button
          >
          {#if !readOnly}
            <button
              type="button"
              class="action danger"
              onclick={requestDelete}
              disabled={!selectedId}>Delete</button
            >
            <button
              type="button"
              class="action primary"
              onclick={() => void saveDraft()}
              disabled={!dirty || !!validation || saving}
              >{saving ? 'Saving…' : 'Save'}</button
            >
          {/if}
        </div>
      </div>
      <div
        class="flex-1 min-h-0 overflow-y-auto custom-scrollbar p-5 space-y-4"
      >
        <div class="grid sm:grid-cols-2 gap-4">
          <label class="label"
            >Template ID<input
              class="field"
              bind:value={draft.id}
              disabled={readOnly || !!selectedId}
            /></label
          >
          <label class="label"
            >Title<input
              class="field"
              bind:value={draft.title}
              disabled={readOnly}
            /></label
          >
          <label class="label"
            >Category<input
              class="field"
              bind:value={draft.category}
              disabled={readOnly}
            /></label
          >
          <label class="label"
            >Icon<input
              class="field"
              bind:value={draft.icon}
              disabled={readOnly}
              placeholder="description"
            /></label
          >
        </div>
        <label class="label"
          >Description<input
            class="field"
            bind:value={draft.description}
            disabled={readOnly}
          /></label
        >
        <label class="label"
          >Markdown body<textarea
            class="field body"
            bind:value={draft.body}
            disabled={readOnly}
            spellcheck="true"></textarea></label
        >
        {#if validation && !readOnly}<p class="message error" role="alert">
            {validation}
          </p>{/if}
        {#if error}<p class="message error" role="alert">{error}</p>{/if}
        {#if templateStatus.message}<p
            class="message {templateStatus.kind}"
            role="status"
            aria-live="polite"
          >
            {templateStatus.message}
          </p>{/if}
      </div>
    {/if}
  </section>
</div>

{#if confirmMode}
  <ConfirmDialog
    title={confirmMode === 'delete'
      ? 'Delete template?'
      : 'Discard unsaved changes?'}
    message={confirmMode === 'delete'
      ? `Delete “${draft?.title ?? 'this template'}”? This cannot be undone.`
      : 'Your unsaved template changes will be lost.'}
    confirmLabel={confirmMode === 'delete' ? 'Delete' : 'Discard'}
    destructive={confirmMode === 'delete'}
    onConfirm={() => void confirmDialog()}
    onCancel={() => {
      confirmMode = ''
      pendingAction = null
    }}
    dataTestId="template-confirm"
  />
{/if}

<style>
  .action {
    border: 1px solid var(--color-surface-panel-border);
    background: transparent;
    color: var(--color-text-primary);
    border-radius: 0.5rem;
    padding: 0.45rem 0.75rem;
    cursor: pointer;
    font-size: var(--text-type-xs);
    font-weight: 650;
  }
  .action:hover:not(:disabled),
  .action:focus-visible {
    background: var(--color-hover);
    outline: none;
  }
  .action.primary {
    color: var(--color-accent-primary-start);
    border-color: color-mix(
      in srgb,
      var(--color-accent-primary-start) 45%,
      transparent
    );
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 10%,
      transparent
    );
  }
  .action.danger {
    color: var(--color-status-danger);
  }
  .action:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .field {
    display: block;
    margin-top: 0.35rem;
    width: 100%;
    border: 1px solid var(--color-surface-panel-border);
    border-radius: 0.55rem;
    background: var(--color-surface-panel);
    color: var(--color-text-primary);
    padding: 0.55rem 0.7rem;
    outline: none;
    font: inherit;
  }
  .field:focus {
    border-color: var(--color-accent-primary-start);
    box-shadow: 0 0 0 2px
      color-mix(in srgb, var(--color-accent-primary-start) 25%, transparent);
  }
  .field:disabled {
    opacity: 0.7;
    cursor: not-allowed;
  }
  .label {
    display: block;
    color: var(--color-text-muted);
    font-size: var(--text-type-xs);
    font-weight: 650;
  }
  .body {
    min-height: 20rem;
    resize: vertical;
    font-family: var(--font-mono);
    line-height: 1.6;
  }
  .template-row {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 0.65rem;
    border: 1px solid transparent;
    background: transparent;
    border-radius: 0.65rem;
    padding: 0.65rem 0.7rem;
    text-align: left;
    cursor: pointer;
  }
  .template-row:hover,
  .template-row:focus-visible {
    background: var(--color-hover);
    outline: none;
  }
  .template-row.selected {
    border-color: color-mix(
      in srgb,
      var(--color-accent-primary-start) 40%,
      transparent
    );
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 10%,
      transparent
    );
  }
  .source {
    border: 1px solid var(--color-surface-panel-border);
    border-radius: 999px;
    padding: 0.15rem 0.4rem;
    color: var(--color-text-muted);
    font-size: var(--text-type-3xs);
    white-space: nowrap;
  }
  .source.plugin {
    color: var(--color-accent-primary-start);
  }
  .source.disk {
    color: var(--color-status-success);
  }
  .empty {
    margin: 0;
    padding: 2rem;
    color: var(--color-text-muted);
    font-size: var(--text-type-sm);
  }
  .link {
    border: 0;
    background: transparent;
    color: var(--color-accent-primary-start);
    text-decoration: underline;
    cursor: pointer;
  }
  .message {
    margin: 0;
    border-radius: 0.55rem;
    padding: 0.65rem 0.75rem;
    font-size: var(--text-type-sm);
  }
  .message.error {
    color: var(--color-status-danger);
    background: color-mix(in srgb, var(--color-status-danger) 10%, transparent);
  }
  .message.success {
    color: var(--color-status-success);
    background: color-mix(
      in srgb,
      var(--color-status-success) 10%,
      transparent
    );
  }
</style>
