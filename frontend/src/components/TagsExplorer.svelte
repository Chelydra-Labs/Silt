<script lang="ts">
  import { QueryBlocksByTag } from '../../bindings/silt/app.js'
  import ViewHeader from './ui/ViewHeader.svelte'
  import ViewState from './ui/ViewState.svelte'
  import ContextMenu from './ContextMenu.svelte'
  import {
    isContextMenuKey,
    openContextMenuFromKeyboard
  } from '../lib/contextMenuKey'
  import { usePageLocatorMenu } from '../lib/pageLocatorMenu.svelte'
  import { copyText } from '../lib/pageActions'

  interface Props {
    selectedTag?: string
  }

  let { selectedTag = '' }: Props = $props()

  let activeTag = $state('')
  type TagHit = {
    id: string
    notebook?: string
    section?: string
    page?: string
    file_date?: string
    clean_content?: string
    clean_text?: string
  }
  let results = $state<TagHit[]>([])
  let loadingResults = $state(false)
  let loadError = $state<string | null>(null)

  // --- block context menu (#1120) -----------------------------------------
  //
  // Tag rows are block-scoped, so none of the standard page-locator items
  // apply verbatim; like Search's "Replace in vault", the items are
  // surface-owned. The shared helper still owns the menu state, anchor
  // resolution, dismissal, and focus return — the identical chrome every
  // #1120 surface shares.
  interface TagBlockLocator {
    notebook: string
    section: string
    page: string
    blockId: string
    date: string
  }

  function blockLocatorOf(res: TagHit): TagBlockLocator {
    return {
      notebook: res.notebook ?? '',
      section: res.section ?? '',
      page: res.page ?? '',
      blockId: res.id,
      date: res.file_date ?? ''
    }
  }

  // Hits without a page triple can't be navigated or referenced; those rows
  // keep the native menu (the Quick Access unavailable-row rule).
  function locatorMenuApplies(res: TagHit): boolean {
    return !!res.notebook && !!res.page
  }

  // Hosted for chrome/state only — the items below are surface-specific,
  // so nothing from the standard vocabulary is injected and
  // locatorMenu.items stays unused.
  const locatorMenu = usePageLocatorMenu<TagBlockLocator>({})

  function openBlockAt(loc: TagBlockLocator) {
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: loc.notebook,
          section: loc.section,
          page: loc.page,
          date: loc.date,
          blockId: loc.blockId
        }
      })
    )
  }

  function openContainingPage(loc: TagBlockLocator) {
    window.dispatchEvent(
      new CustomEvent('navigate-to-page', {
        detail: {
          notebook: loc.notebook,
          section: loc.section,
          page: loc.page
        }
      })
    )
  }

  function runItem(action: (loc: TagBlockLocator) => void): void {
    const loc = locatorMenu.menu?.locator
    locatorMenu.close()
    if (loc) action(loc)
  }

  function onRowKeydown(e: KeyboardEvent, res: TagHit): void {
    if (!isContextMenuKey(e)) return
    if (!locatorMenuApplies(res)) return
    e.preventDefault()
    openContextMenuFromKeyboard(e.currentTarget as HTMLElement)
  }

  async function selectTag(path: string) {
    activeTag = path
    loadingResults = true
    loadError = null
    try {
      results = await QueryBlocksByTag(path)
    } catch (e) {
      console.error('QueryBlocksByTag failed:', e)
      loadError =
        e instanceof Error ? e.message : 'Failed to load tagged blocks'
      results = []
    } finally {
      loadingResults = false
    }
  }

  function openBlock(res: TagHit) {
    openBlockAt(blockLocatorOf(res))
  }

  // When selectedTag updates from props, load the blocks
  $effect(() => {
    const tag = selectedTag
    if (tag) {
      void selectTag(tag)
    }
  })
</script>

<div class="flex-1 flex flex-col min-h-0 bg-surface-panel">
  <ViewHeader
    title="Tags"
    headingLevel="h1"
    icon="label"
    dataTestId="tags-explorer-header"
  >
    {#snippet meta()}
      {#if activeTag}
        <span
          class="text-text-muted text-type-sm font-body-md normal-case font-normal"
          data-testid="tags-count"
        >
          #{activeTag} · {results.length} block{results.length === 1 ? '' : 's'}
        </span>
      {/if}
    {/snippet}
  </ViewHeader>
  <div class="flex-1 overflow-y-auto custom-scrollbar">
    {#if !activeTag}
      <ViewState
        kind="empty"
        icon="label"
        title="No tag selected"
        description="Select a tag in the sidebar to see its blocks."
      />
    {:else if loadingResults}
      <ViewState kind="loading" title="Loading tagged blocks…" />
    {:else if loadError}
      <ViewState
        kind="error"
        title="Failed to load tagged blocks"
        description={loadError}
        onRetry={() => selectTag(activeTag)}
      />
    {:else if results.length === 0}
      <ViewState
        kind="empty"
        icon="label_off"
        title="No blocks tagged"
        description="No blocks have #{activeTag}."
      />
    {:else}
      {#each results as res (res.id)}
        <button
          onclick={() => openBlock(res)}
          aria-keyshortcuts={locatorMenuApplies(res) ? 'Shift+F10' : undefined}
          onkeydown={(e) => onRowKeydown(e, res)}
          oncontextmenu={(e) => {
            if (locatorMenuApplies(res)) locatorMenu.openFromEvent(e, blockLocatorOf(res))
          }}
          class="w-full text-left px-6 py-3 border-b border-surface-panel-border/50 hover:bg-hover transition-colors border-none bg-transparent cursor-pointer flex flex-col gap-1"
        >
          <div
            class="flex items-center gap-1.5 text-type-2xs text-text-muted uppercase tracking-widest font-label-sm-bold"
          >
            <span>{res.notebook}</span>
            <span class="material-symbols-outlined text-type-2xs"
              >chevron_right</span
            >
            <span>{res.section}</span>
            <span class="material-symbols-outlined text-type-2xs"
              >chevron_right</span
            >
            <span>{res.page}</span>
          </div>
          <div class="font-body-md text-sm text-text-primary">
            {res.clean_content}
          </div>
        </button>
      {/each}
    {/if}
  </div>

  {#if locatorMenu.menu}
    <ContextMenu
      open={true}
      anchor={{ x: locatorMenu.menu.x, y: locatorMenu.menu.y }}
      anchorEl={locatorMenu.menu.anchorEl}
      onClose={locatorMenu.close}
      ariaLabel={`Actions for ${locatorMenu.menu.locator.page}`}
      menuTestId="tags-locator-menu"
    >
      <button
        type="button"
        role="menuitem"
        data-testid="tags-menu-open-block"
        onclick={() => runItem(openBlockAt)}
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >my_location</span
        >
        <span>Open block</span>
      </button>
      <button
        type="button"
        role="menuitem"
        data-testid="tags-menu-open-page"
        onclick={() => runItem(openContainingPage)}
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >description</span
        >
        <span>Open containing page</span>
      </button>
      <button
        type="button"
        role="menuitem"
        data-testid="tags-menu-copy-block-reference"
        onclick={() => runItem((loc) => void copyText(`((${loc.blockId}))`))}
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >link</span
        >
        <span>Copy block reference</span>
      </button>
    </ContextMenu>
  {/if}
</div>
