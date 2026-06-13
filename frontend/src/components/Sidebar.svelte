<script lang="ts">
  import { onMount } from 'svelte'
  import {
    ListNotebooksAndSections,
    CreateNewSection
  } from '../../wailsjs/go/main/App.js'

  interface Props {
    activeNotebook: string
    activeSection: string
    activeView: string
    onSelectNotebook: (notebook: string) => void
    onSelectSection: (section: string) => void
    onSelectView: (view: string) => void
  }

  let {
    activeNotebook = $bindable(),
    activeSection = $bindable(),
    activeView = $bindable(),
    onSelectNotebook,
    onSelectSection,
    onSelectView
  }: Props = $props()

  let notebooksMap = $state<Record<string, string[]>>({})
  let showNotebookDropdown = $state(false)

  let notebooksList = $derived(Object.keys(notebooksMap))
  let sectionsList = $derived(notebooksMap[activeNotebook] || [])

  async function loadNotebooks() {
    try {
      const data = await ListNotebooksAndSections()
      if (data) {
        notebooksMap = data

        // Set default notebook/section if none active
        const list = Object.keys(data)
        if (list.length > 0) {
          if (!activeNotebook || !list.includes(activeNotebook)) {
            activeNotebook = list.includes('Work') ? 'Work' : list[0]
            onSelectNotebook(activeNotebook)
          }

          const sections = data[activeNotebook] || []
          if (
            sections.length > 0 &&
            (!activeSection || !sections.includes(activeSection))
          ) {
            activeSection = sections.includes('Journal')
              ? 'Journal'
              : sections[0]
            onSelectSection(activeSection)
          }
        }
      }
    } catch (e) {
      console.error('Failed to load notebooks and sections:', e)
    }
  }

  function handleSwitchNotebook(nb: string) {
    activeNotebook = nb
    showNotebookDropdown = false
    onSelectNotebook(nb)

    // Select first section of this notebook
    const sections = notebooksMap[nb] || []
    if (sections.length > 0) {
      activeSection = sections.includes('Journal') ? 'Journal' : sections[0]
      onSelectSection(activeSection)
    } else {
      activeSection = ''
      onSelectSection('')
    }
  }

  async function handleCreateSection() {
    const sectionName = prompt('Enter new section name:')
    if (!sectionName) return

    const trimmed = sectionName.trim()
    if (trimmed === '') return

    try {
      // Create new section file with scaffolded daily note
      await CreateNewSection(activeNotebook, trimmed, '')

      // Reload the navigation tree
      await loadNotebooks()

      // Set the newly created section as active
      activeSection = trimmed
      onSelectSection(trimmed)
      activeView = 'notes'
      onSelectView('notes')
    } catch (e) {
      alert('Failed to create new section: ' + e)
    }
  }

  onMount(() => {
    loadNotebooks()

    // Listen for external refresh requests
    const handleRefresh = () => loadNotebooks()
    window.addEventListener('refresh-navigation', handleRefresh)
    return () => {
      window.removeEventListener('refresh-navigation', handleRefresh)
    }
  })
</script>

<aside
  class="bg-bg-surface border-r border-border-muted w-64 flex flex-col py-[4px] fixed left-0 top-14 h-[calc(100vh-56px)] select-none z-40"
>
  <div class="px-4 py-4 flex flex-col gap-1 relative">
    <!-- Workspace selector header -->
    <div class="flex items-center justify-between mb-4">
      <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
      <div
        onclick={() => (showNotebookDropdown = !showNotebookDropdown)}
        class="flex flex-col cursor-pointer group"
      >
        <div class="flex items-center gap-1.5">
          <span class="text-accent-teal-start font-headline-md text-headline-md"
            >{activeNotebook || 'Workspace'}</span
          >
          <span
            class="material-symbols-outlined text-text-muted text-[16px] group-hover:text-accent-teal-start transition-colors"
          >
            keyboard_arrow_down
          </span>
        </div>
        <span
          class="text-text-muted text-[9px] uppercase tracking-widest font-label-sm-bold"
        >
          Active Notebook
        </span>
      </div>

      <!-- Mock Profile Avatar -->
      <div
        class="w-8 h-8 rounded-lg bg-surface-container-high border border-border-muted flex items-center justify-center overflow-hidden"
      >
        <img
          alt="Profile Avatar"
          class="w-full h-full object-cover"
          src="https://lh3.googleusercontent.com/aida-public/AB6AXuCFZ_T6sRDXwaM-r-XY_HTbuuXenNX5lsepep3Km4BDTL5_x0rTmY2UCbFMEWdmXoikDvMPQwYrtW3TdZUs12Jiu7qU4Aih8bOEGyXgXrWDCqUcQU9ICVZ9_65a5d-R72RqkxhV8H-e7KWBojNtHl9QvvicTkAtAFpA-ZVcPIdpKZgFjdOayGDObK7jcR_Mp7p9JIkh9gvVFjPnctfDKkVJAQgua867sFyY4qm7yD_k5dUpwpyEW0unwwW6Sx1_1tOF_fCZ4wiIE04"
        />
      </div>
    </div>

    <!-- Notebooks Dropdown Panel -->
    {#if showNotebookDropdown}
      <div
        class="absolute left-4 right-4 top-16 glass-palette border border-accent-teal-start/20 rounded-lg shadow-2xl z-[70] py-2"
        style="backdrop-filter: blur(16px); background: rgba(22, 22, 25, 0.9);"
      >
        {#each notebooksList as nb}
          <button
            onclick={() => handleSwitchNotebook(nb)}
            class="flex items-center gap-3 px-4 py-2 w-full text-left cursor-pointer hover:bg-bg-hover transition-colors font-body-md border-none bg-transparent"
          >
            <span
              class="material-symbols-outlined text-accent-teal-start text-[18px]"
              >folder_shared</span
            >
            <span class="font-label-sm text-label-sm text-text-primary"
              >{nb}</span
            >
            {#if nb === activeNotebook}
              <span
                class="material-symbols-outlined text-accent-teal-start text-[16px] ml-auto"
                >check</span
              >
            {/if}
          </button>
        {/each}
      </div>
    {/if}

    <!-- New Section Button -->
    <button
      onclick={handleCreateSection}
      class="w-full bg-accent-teal-glow border border-accent-teal-start/30 text-accent-teal-start font-label-sm-bold text-label-sm-bold py-2.5 rounded mb-4 flex items-center justify-center gap-2 hover:brightness-110 hover:border-accent-teal-start transition-all cursor-pointer focus:outline-none"
    >
      <span class="material-symbols-outlined text-[18px]">add</span>
      New Section
    </button>

    <!-- Side Menu Categories -->
    <div class="space-y-1">
      <!-- Notes Section collapsible header -->
      <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
      <div
        onclick={() => {
          activeView = 'notes'
          onSelectView('notes')
        }}
        class="flex items-center gap-3 px-4 py-2 cursor-pointer transition-all rounded"
        class:bg-bg-hover={activeView === 'notes'}
        class:text-accent-teal-start={activeView === 'notes'}
        class:text-text-muted={activeView !== 'notes'}
      >
        <span class="material-symbols-outlined text-[20px]">description</span>
        <span class="font-label-sm text-label-sm font-bold flex-1">Notes</span>
      </div>

      <!-- Collapsible Sections List under Notes -->
      {#if activeView === 'notes' && sectionsList.length > 0}
        <div
          class="pl-6 pr-2 py-1 space-y-1 border-l border-border-muted ml-6 mb-2"
        >
          {#each sectionsList as sec}
            <button
              onclick={() => {
                activeSection = sec
                onSelectSection(sec)
              }}
              class="w-full text-left px-3 py-1.5 rounded text-[13px] font-body-md transition-colors border-none bg-transparent cursor-pointer"
              class:bg-bg-active={activeSection === sec}
              class:text-accent-teal-start={activeSection === sec}
              class:text-text-muted={activeSection !== sec}
              class:hover:text-text-primary={activeSection !== sec}
            >
              {sec}
            </button>
          {/each}
        </div>
      {/if}

      <!-- Agenda View -->
      <button
        onclick={() => {
          activeView = 'agenda'
          onSelectView('agenda')
        }}
        class="w-full border-none bg-transparent flex items-center gap-3 px-4 py-2 cursor-pointer transition-all rounded text-left"
        class:bg-bg-hover={activeView === 'agenda'}
        class:text-accent-teal-start={activeView === 'agenda'}
        class:text-text-muted={activeView !== 'agenda'}
      >
        <span class="material-symbols-outlined text-[20px]">event_repeat</span>
        <span class="font-label-sm text-label-sm">Agenda</span>
      </button>

      <!-- Tags View -->
      <button
        onclick={() => {
          activeView = 'tags'
          onSelectView('tags')
        }}
        class="w-full border-none bg-transparent flex items-center gap-3 px-4 py-2 cursor-pointer transition-all rounded text-left"
        class:bg-bg-hover={activeView === 'tags'}
        class:text-accent-teal-start={activeView === 'tags'}
        class:text-text-muted={activeView !== 'tags'}
      >
        <span class="material-symbols-outlined text-[20px]">label</span>
        <span class="font-label-sm text-label-sm">Tags</span>
      </button>

      <!-- Calendar View -->
      <button
        onclick={() => {
          activeView = 'calendar'
          onSelectView('calendar')
        }}
        class="w-full border-none bg-transparent flex items-center gap-3 px-4 py-2 cursor-pointer transition-all rounded text-left"
        class:bg-bg-hover={activeView === 'calendar'}
        class:text-accent-teal-start={activeView === 'calendar'}
        class:text-text-muted={activeView !== 'calendar'}
      >
        <span class="material-symbols-outlined text-[20px]">calendar_month</span
        >
        <span class="font-label-sm text-label-sm">Calendar</span>
      </button>

      <!-- Kanban View -->
      <button
        onclick={() => {
          activeView = 'kanban'
          onSelectView('kanban')
        }}
        class="w-full border-none bg-transparent flex items-center gap-3 px-4 py-2 cursor-pointer transition-all rounded text-left"
        class:bg-bg-hover={activeView === 'kanban'}
        class:text-accent-teal-start={activeView === 'kanban'}
        class:text-text-muted={activeView !== 'kanban'}
      >
        <span class="material-symbols-outlined text-[20px]">view_kanban</span>
        <span class="font-label-sm text-label-sm">Kanban</span>
      </button>
    </div>
  </div>

  <!-- Bottom navigation items -->
  <div class="mt-auto pb-4 space-y-1">
    <button
      class="w-full border-none bg-transparent flex items-center gap-3 text-text-muted px-4 py-2 hover:bg-bg-hover hover:text-text-primary cursor-pointer transition-all text-left"
    >
      <span class="material-symbols-outlined text-[20px]">archive</span>
      <span class="font-label-sm text-label-sm">Archive</span>
    </button>
    <button
      class="w-full border-none bg-transparent flex items-center gap-3 text-text-muted px-4 py-2 hover:bg-bg-hover hover:text-text-primary cursor-pointer transition-all text-left"
    >
      <span class="material-symbols-outlined text-[20px]">delete</span>
      <span class="font-label-sm text-label-sm">Trash</span>
    </button>
    <button
      class="w-full border-none bg-transparent flex items-center gap-3 text-text-muted px-4 py-2 hover:bg-bg-hover hover:text-text-primary cursor-pointer transition-all text-left"
    >
      <span class="material-symbols-outlined text-[20px]">settings</span>
      <span class="font-label-sm text-label-sm">Settings</span>
    </button>
  </div>
</aside>
