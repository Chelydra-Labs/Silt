<script lang="ts">
  import { onMount } from 'svelte'
  import {
    IsVaultInitialized,
    InitializeVault
  } from '../wailsjs/go/main/App.js'
  import TopBar from './components/TopBar.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import VirtualScrollContainer from './components/VirtualScrollContainer.svelte'
  import SearchModal from './components/SearchModal.svelte'
  import logo from './assets/logo.svg'

  let isInitialized = $state(false)
  let loading = $state(true)

  // Navigation state
  let activeNotebook = $state('Work')
  let activeSection = $state('Journal')
  let activeView = $state('notes')

  // Search overlay state
  let showSearch = $state(false)

  // Focused block ancestry path highlighting
  let activeFocusedBlockAncestors = $state<string[]>([])
  let searchTargetDate = $state('')
  let searchTargetBlockId = $state('')
  let searchTargetKey = $state('')

  onMount(() => {
    async function checkInit() {
      try {
        isInitialized = await IsVaultInitialized()
      } catch (e) {
        console.error('Startup check failed:', e)
      } finally {
        loading = false
      }
    }
    checkInit()

    // Bind global keyboard shortcut (Ctrl+P) for Fuzzy Search
    function handleGlobalKeyDown(e: KeyboardEvent) {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'p') {
        e.preventDefault()
        showSearch = !showSearch
      }
    }

    // Listen for switch-view custom events (e.g. from Slash Command Palette)
    function handleSwitchView(e: Event) {
      const customEvent = e as CustomEvent
      if (customEvent.detail) {
        activeView = customEvent.detail
      }
    }

    window.addEventListener('keydown', handleGlobalKeyDown)
    window.addEventListener('switch-view', handleSwitchView)
    return () => {
      window.removeEventListener('keydown', handleGlobalKeyDown)
      window.removeEventListener('switch-view', handleSwitchView)
    }
  })

  async function handleSelectFolder() {
    try {
      const success = await InitializeVault()
      if (success) {
        isInitialized = true
        // Fire refresh to load notebooks list in Sidebar
        window.dispatchEvent(new CustomEvent('refresh-navigation'))
      }
    } catch (e) {
      alert('Failed to initialize vault: ' + e)
    }
  }

  // Handle fuzzy search selection jump
  function handleSearchJump(
    notebook: string,
    section: string,
    date: string,
    blockId: string
  ) {
    activeNotebook = notebook
    activeSection = section
    activeView = 'notes'
    searchTargetDate = date
    searchTargetBlockId = blockId
    searchTargetKey = `${date}:${blockId}:${Date.now()}`
  }

  function handleBlockFocus(blockId: string, ancestors: string[]) {
    activeFocusedBlockAncestors = ancestors
  }

  function handleBlockBlur() {
    activeFocusedBlockAncestors = []
  }
</script>

<main
  class="w-full h-full flex flex-col bg-bg-void text-text-primary overflow-hidden font-body-md"
>
  {#if loading}
    <div class="onboarding-container">
      <div class="text-text-muted animate-pulse text-lg font-headline-md">
        Initializing Silt Core...
      </div>
    </div>
  {:else if !isInitialized}
    <!-- First run Onboarding folder setup screen -->
    <div class="onboarding-container select-none">
      <div class="onboarding-card">
        <img
          src={logo}
          alt="Silt Logo"
          class="onboarding-logo animate-spin-slow"
        />
        <h1 class="onboarding-title font-headline-lg">Silt</h1>
        <p class="onboarding-description font-body-md">
          A local-first hybrid journal and task manager. Plain-text Markdown on
          your drive, real-time index in memory.
        </p>
        <button
          class="onboarding-btn font-label-sm-bold"
          onclick={handleSelectFolder}
        >
          Initialize Workspace Folder
        </button>
      </div>
    </div>
  {:else}
    <!-- Main Shell Layout -->
    <TopBar onSearchClick={() => (showSearch = true)} />

    <div class="flex flex-1 pt-14 h-full w-full relative">
      <Sidebar
        bind:activeNotebook
        bind:activeSection
        bind:activeView
        onSelectNotebook={(nb) => (activeNotebook = nb)}
        onSelectSection={(sec) => (activeSection = sec)}
        onSelectView={(v) => (activeView = v)}
      />

      <!-- Content viewport router -->
      <div
        class="ml-64 flex-1 h-[calc(100vh-56px)] flex flex-col overflow-hidden bg-bg-void"
      >
        {#if activeView === 'notes'}
          <VirtualScrollContainer
            notebook={activeNotebook}
            section={activeSection}
            targetDate={searchTargetDate}
            targetBlockId={searchTargetBlockId}
            targetKey={searchTargetKey}
            {activeFocusedBlockAncestors}
            onBlockFocus={handleBlockFocus}
            onBlockBlur={handleBlockBlur}
          />
        {:else if activeView === 'kanban'}
          <div class="flex-1 p-8 flex flex-col select-none">
            <h1
              class="font-headline-lg text-headline-lg text-text-primary mb-4"
            >
              Kanban Board
            </h1>
            <p class="text-text-muted font-body-md">
              Lane board plugin loading soon in Sprint 3 / 4. Check out your
              note tasks in {activeSection} Timeline!
            </p>
          </div>
        {:else if activeView === 'agenda'}
          <div class="flex-1 p-8 flex flex-col select-none">
            <h1
              class="font-headline-lg text-headline-lg text-text-primary mb-4"
            >
              Agenda
            </h1>
            <p class="text-text-muted font-body-md">
              Chronological schedule list loading soon in Sprint 3. Check out
              your task lists in {activeSection} Timeline!
            </p>
          </div>
        {:else if activeView === 'tags'}
          <div class="flex-1 p-8 flex flex-col select-none">
            <h1
              class="font-headline-lg text-headline-lg text-text-primary mb-4"
            >
              Tags Explorer
            </h1>
            <p class="text-text-muted font-body-md">
              Tag taxonomy search index loading soon in Sprint 3. Check out your
              tags in {activeSection} Timeline!
            </p>
          </div>
        {:else if activeView === 'calendar'}
          <div class="flex-1 p-8 flex flex-col select-none">
            <h1
              class="font-headline-lg text-headline-lg text-text-primary mb-4"
            >
              Calendar
            </h1>
            <p class="text-text-muted font-body-md">
              Macro planner scheduler loading soon in Sprint 3. Check out your
              tasks in {activeSection} Timeline!
            </p>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  <!-- Global Search Overlay Modal -->
  {#if showSearch}
    <SearchModal
      onClose={() => (showSearch = false)}
      onJump={handleSearchJump}
    />
  {/if}
</main>

<style>
  .animate-spin-slow {
    animation: spin 8s linear infinite;
  }
  @keyframes spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }
</style>
