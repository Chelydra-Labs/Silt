<script lang="ts">
  // Recursive section renderer for the sidebar tree (#88). Renders one
  // NavigationSection plus its nested Children. Each level tracks its own
  // expanded state via the parent Sidebar's `expandedSections` set (keyed
  // by the section's single-segment display name — the immediate folder
  // name, not the full path).
  //
  // Drag-to-reorder (#68), right-click context menu (#62), and HTML5 DnD
  // handlers are threaded in from the parent Sidebar so every section and
  // page — top-level or deeply nested — retains those capabilities.
  import SidebarSection from './SidebarSection.svelte'
  import type { NavSection } from '../lib/sidebar/types'
  import { sortByName } from '../lib/sidebar/navOrder'

  interface DropTarget {
    level: string
    name: string
    before: boolean
  }

  interface Props {
    section: NavSection
    depth: number
    activeNotebook: string
    activeSection: string
    activePage: string
    expandedSections: Set<string>
    navOrder: {
      pages: Record<string, string[]>
    }
    dropTarget?: DropTarget | null
    dragItem?: { level: string; name: string; section?: string } | null
    onToggleSection: (name: string) => void
    onSelectPage: (section: string, page: string) => void
    onPinPage: (section: string, page: string) => void
    onSelectSection: (section: string) => void
    onCreatePageInline: (section: string) => void
    onDragStart: (
      e: DragEvent,
      level: string,
      name: string,
      section?: string
    ) => void
    onDragOver: (e: DragEvent, level: string, name: string) => void
    onDragLeave: () => void
    onDrop: (
      e: DragEvent,
      level: string,
      targetName: string,
      notebook?: string,
      section?: string
    ) => void
    onDragEnd: () => void
    onContextMenu: (
      e: MouseEvent,
      level: 'section' | 'page',
      notebook: string,
      section?: string,
      page?: string
    ) => void
  }

  let {
    section,
    depth,
    activeNotebook,
    activeSection,
    activePage,
    expandedSections,
    navOrder,
    dropTarget = null,
    dragItem = null,
    onToggleSection,
    onSelectPage,
    onPinPage,
    onSelectSection,
    onCreatePageInline,
    onDragStart,
    onDragOver,
    onDragLeave,
    onDrop,
    onDragEnd,
    onContextMenu
  }: Props = $props()

  let sectionKey = $derived(section.path || section.name)
  let isExpanded = $derived(expandedSections.has(sectionKey))

  let sortedPages = $derived(
    sortByName(
      section.pages,
      navOrder.pages[`${activeNotebook}/${sectionKey}`] ?? []
    )
  )

  function recursivePageCount(sec: NavSection): number {
    let count = sec.pages.length
    if (sec.children) {
      for (const child of sec.children) {
        count += recursivePageCount(child)
      }
    }
    return count
  }

  let totalCount = $derived(recursivePageCount(section))
</script>

<div class="mb-0.5">
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div
    class="group flex items-center gap-1 px-2 py-1.5 cursor-pointer rounded hover:bg-hover transition-colors"
    class:drag-over-top={dropTarget?.level === 'section' &&
      dragItem?.level !== 'page' &&
      dropTarget.name === section.name &&
      dropTarget.before}
    class:drag-over-bottom={dropTarget?.level === 'section' &&
      dragItem?.level !== 'page' &&
      dropTarget.name === section.name &&
      !dropTarget.before}
    class:drag-over-into={dropTarget?.level === 'section' &&
      dragItem?.level === 'page' &&
      dropTarget.name === section.name}
    draggable="true"
    ondragstart={(e) => onDragStart(e, 'section', section.name)}
    ondragover={(e) => onDragOver(e, 'section', section.name)}
    ondragleave={onDragLeave}
    ondrop={(e) =>
      onDrop(e, 'section', section.name, activeNotebook, sectionKey)}
    ondragend={onDragEnd}
    onclick={() => onToggleSection(sectionKey)}
    onkeydown={(e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        onToggleSection(sectionKey)
      }
    }}
    oncontextmenu={(e) =>
      onContextMenu(e, 'section', activeNotebook, sectionKey)}
    role="treeitem"
    tabindex="0"
    aria-level={depth + 1}
    aria-expanded={isExpanded}
    aria-selected={activeSection === sectionKey}
  >
    <span
      class="material-symbols-outlined text-icon-md transition-transform"
      class:rotate-90={isExpanded}
      class:text-accent-primary-start={activeSection === sectionKey}
      class:text-surface-sidebar-text-muted={activeSection !== sectionKey}
    >
      chevron_right
    </span>
    <span
      class="font-semibold text-type-md text-surface-sidebar-text truncate flex-1"
    >
      {section.name ? section.name : 'Pages (no section)'}
    </span>
    {#if totalCount > 0}
      <span
        class="text-type-3xs font-label-sm text-surface-sidebar-text-muted bg-surface-card border border-surface-sidebar-border rounded-full px-1.5 py-0.5"
      >
        {totalCount}
      </span>
    {/if}
    <button
      onclick={(e) => {
        e.stopPropagation()
        onSelectSection(sectionKey)
        onCreatePageInline(sectionKey)
      }}
      title="New page in this section"
      class="opacity-30 group-hover:opacity-100 text-surface-sidebar-text-muted hover:text-accent-primary-start border-none bg-transparent cursor-pointer p-0.5 rounded transition-all"
    >
      <span class="material-symbols-outlined text-icon-md">add</span>
    </button>
  </div>

  {#if isExpanded}
    <div
      class="ml-4 border-l pl-1 mt-0.5 mb-1.5 transition-colors duration-200 {activeSection ===
      sectionKey
        ? 'border-accent-primary-start/30'
        : 'border-surface-sidebar-border'}"
    >
      {#if section.pages.length === 0 && (!section.children || section.children.length === 0)}
        <div
          class="text-surface-sidebar-text-muted text-type-2xs font-body-md py-1.5 px-2.5 flex items-center justify-between select-none"
        >
          <span class="italic">No pages</span>
          <button
            type="button"
            onclick={() => {
              // Match header +: select section first so create lands in the
              // active-section path (title focus / nav highlight).
              onSelectSection(sectionKey)
              onCreatePageInline(sectionKey)
            }}
            class="text-type-2xs text-accent-primary-start hover:underline border-none bg-transparent cursor-pointer p-0 font-medium focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-primary-start rounded-sm"
            title="Create a new page in this section"
          >
            + Add Page
          </button>
        </div>
      {:else}
        {#each sortedPages as pg (pg.name)}
          {@const isActive =
            activeSection === sectionKey && activePage === pg.name}
          <button
            onclick={() => onSelectPage(sectionKey, pg.name)}
            ondblclick={() => onPinPage(sectionKey, pg.name)}
            onauxclick={(e) => {
              // Middle-click (button 1) pins the page — industry-standard parity (#142).
              if (e.button === 1) {
                e.preventDefault()
                onPinPage(sectionKey, pg.name)
              }
            }}
            oncontextmenu={(e) =>
              onContextMenu(e, 'page', activeNotebook, sectionKey, pg.name)}
            draggable="true"
            ondragstart={(e) => onDragStart(e, 'page', pg.name, sectionKey)}
            ondragover={(e) => onDragOver(e, 'page', pg.name)}
            ondragleave={onDragLeave}
            ondrop={(e) =>
              onDrop(e, 'page', pg.name, activeNotebook, sectionKey)}
            ondragend={onDragEnd}
            class="relative w-full text-left pl-4 pr-2 py-1.5 rounded text-type-md font-body-md transition-colors border-none bg-transparent cursor-pointer flex items-center gap-2"
            class:bg-hover={isActive}
            class:text-surface-sidebar-text={isActive}
            class:font-medium={isActive}
            class:text-surface-sidebar-text-muted={!isActive}
            class:hover:text-surface-sidebar-text={!isActive}
            class:drag-over-top={dropTarget?.level === 'page' &&
              dropTarget.name === pg.name &&
              dropTarget.before}
            class:drag-over-bottom={dropTarget?.level === 'page' &&
              dropTarget.name === pg.name &&
              !dropTarget.before}
            role="treeitem"
            aria-level={depth + 2}
            aria-selected={isActive}
          >
            {#if isActive}
              <span
                class="absolute left-0 top-1 bottom-1 w-0.5 bg-accent-primary-start rounded-full"
              ></span>
            {/if}
            <span class="truncate flex-1" title={pg.name}>{pg.name}</span>
          </button>
        {/each}
      {/if}
    </div>

    {#if section.children && section.children.length > 0}
      {#each section.children as child (child.name)}
        <SidebarSection
          section={child}
          depth={depth + 1}
          {activeNotebook}
          {activeSection}
          {activePage}
          {expandedSections}
          {navOrder}
          {dropTarget}
          {dragItem}
          {onToggleSection}
          {onSelectPage}
          {onPinPage}
          {onSelectSection}
          {onCreatePageInline}
          {onDragStart}
          {onDragOver}
          {onDragLeave}
          {onDrop}
          {onDragEnd}
          {onContextMenu}
        />
      {/each}
    {/if}
  {/if}
</div>
