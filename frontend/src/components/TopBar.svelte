<script lang="ts">
  import logo from '../assets/logo.svg'

  interface Props {
    onSearchClick: () => void
  }

  let { onSearchClick }: Props = $props()
</script>

<header
  class="bg-void flex justify-between items-center px-[24px] h-14 w-full z-50 fixed top-0 border-b border-border-muted select-none"
>
  <!-- Brand logo and topnav -->
  <div class="flex items-center gap-8">
    <div class="flex items-center gap-2">
      <img
        src={logo}
        alt="notes# logo"
        class="w-6 h-6 filter drop-shadow-[0_0_4px_rgba(6,182,212,0.3)]"
      />
      <span
        class="font-headline-md text-headline-md font-bold text-accent-teal-start"
        >notes#</span
      >
    </div>
    <nav class="hidden md:flex items-center gap-6">
      <a
        class="text-accent-teal-start font-bold border-b-2 border-accent-teal-start pb-1 font-body-md"
        href="#/">Home</a
      >
      <a
        class="text-text-muted font-body-md hover:text-accent-teal-start transition-colors duration-200"
        href="#/recent">Recent</a
      >
      <a
        class="text-text-muted font-body-md hover:text-accent-teal-start transition-colors duration-200"
        href="#/starred">Starred</a
      >
    </nav>
  </div>

  <!-- Search and settings controls -->
  <div class="flex items-center gap-4">
    <!-- Clickable search bar trigger -->
    <button
      onclick={onSearchClick}
      class="bg-bg-surface border border-border-muted rounded-lg pl-3 pr-10 py-1.5 flex items-center gap-2 cursor-pointer text-text-muted hover:border-accent-teal-start transition-all duration-200"
    >
      <span class="material-symbols-outlined text-[18px]">search</span>
      <span class="text-[12px] font-label-sm">Search Notebook... (Ctrl+P)</span>
    </button>

    <button
      class="text-text-muted hover:text-accent-teal-start transition-colors border-none bg-transparent cursor-pointer"
    >
      <span class="material-symbols-outlined">help</span>
    </button>
    <button
      class="text-text-muted hover:text-accent-teal-start transition-colors border-none bg-transparent cursor-pointer"
    >
      <span class="material-symbols-outlined">settings</span>
    </button>
  </div>
</header>
