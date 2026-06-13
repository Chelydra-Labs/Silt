<script lang="ts">
  import logo from '../assets/logo.svg'

  interface Props {
    activeView: string
    sidebarCollapsed: boolean
    onSearchClick: () => void
    onToggleSidebar: () => void
  }

  let {
    activeView = $bindable(),
    sidebarCollapsed = $bindable(),
    onSearchClick,
    onToggleSidebar
  }: Props = $props()

  const views: { id: string; label: string; icon: string }[] = [
    { id: 'notes', label: 'Notes', icon: 'description' },
    { id: 'agenda', label: 'Agenda', icon: 'event_repeat' },
    { id: 'tags', label: 'Tags', icon: 'label' },
    { id: 'calendar', label: 'Calendar', icon: 'calendar_month' },
    { id: 'kanban', label: 'Kanban', icon: 'view_kanban' }
  ]

  function selectView(id: string) {
    activeView = id
  }
</script>

<header
  class="bg-void flex justify-between items-center px-3 h-14 w-full z-50 fixed top-0 border-b border-border-muted select-none gap-4"
>
  <!-- Left: brand + sidebar toggle + view switcher -->
  <div class="flex items-center gap-3 min-w-0">
    <button
      onclick={onToggleSidebar}
      aria-label={sidebarCollapsed ? 'Show sidebar' : 'Hide sidebar'}
      title="Toggle sidebar (Ctrl+B)"
      class="text-text-muted hover:text-accent-teal-start transition-colors border-none bg-transparent cursor-pointer p-1.5 rounded focus:outline-none flex-shrink-0"
    >
      <span class="material-symbols-outlined text-[20px]"
        >{sidebarCollapsed ? 'left_panel_open' : 'left_panel_close'}</span
      >
    </button>

    <div
      class="flex items-center gap-2 pr-2 mr-1 border-r border-border-muted flex-shrink-0"
    >
      <img src={logo} alt="Silt" class="w-6 h-6" />
      <span
        class="font-headline-md text-headline-md text-accent-teal-start font-bold tracking-tight"
        >Silt</span
      >
    </div>

    <!-- View switcher (segmented control) -->
    <nav class="flex items-center gap-0.5 min-w-0">
      {#each views as v (v.id)}
        <button
          onclick={() => selectView(v.id)}
          class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-md font-label-sm text-label-sm transition-all border-none cursor-pointer focus:outline-none whitespace-nowrap"
          class:bg-bg-hover={activeView === v.id}
          class:text-accent-teal-start={activeView === v.id}
          class:text-text-muted={activeView !== v.id}
          aria-pressed={activeView === v.id}
        >
          <span class="material-symbols-outlined text-[18px]">{v.icon}</span>
          <span class="hidden lg:inline">{v.label}</span>
        </button>
      {/each}
    </nav>
  </div>

  <!-- Right: search -->
  <div class="flex items-center gap-2 flex-shrink-0">
    <button
      onclick={onSearchClick}
      class="bg-bg-surface border border-border-muted rounded-lg pl-3 pr-8 py-1.5 flex items-center gap-2 cursor-pointer text-text-muted hover:border-accent-teal-start transition-all duration-200"
    >
      <span class="material-symbols-outlined text-[18px]">search</span>
      <span class="text-[12px] font-label-sm hidden md:inline"
        >Search... (Ctrl+P)</span
      >
    </button>
  </div>
</header>
