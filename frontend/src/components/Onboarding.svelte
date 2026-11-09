<script lang="ts">
  import logo from '../assets/logo.svg'

  interface Props {
    /** True while the startup IPC sequence is in flight (show spinner). */
    loading: boolean
    /** True once startup finished and the vault is known to be initialized. */
    initialized: boolean
    /** True while folder initialization IPC is in progress. */
    initializing?: boolean
    /** Error message to display inline if folder initialization failed. */
    error?: string | null
    /** Retry handler when initialization fails. */
    onRetry?: () => void
    /** Click handler for the "Initialize Workspace" button. */
    onSelectFolder: () => void
  }

  let {
    loading,
    initialized,
    initializing = false,
    error = null,
    onRetry,
    onSelectFolder
  }: Props = $props()
</script>

{#if loading}
  <div class="onboarding-container">
    <div
      role="status"
      aria-live="polite"
      class="text-text-muted animate-pulse text-lg font-headline-md"
    >
      Initializing Silt Core…
    </div>
  </div>
{:else if !initialized}
  <!-- First run onboarding -->
  <div class="onboarding-container select-none">
    <div class="onboarding-card">
      <img src={logo} alt="Silt Logo" class="onboarding-logo" />
      <h1 class="onboarding-title font-headline-lg">Silt</h1>
      <p class="onboarding-description font-body-md">
        Capture ideas. Connect them. Get work done. A fast, private workspace
        for your notes and tasks.
      </p>

      {#if error}
        <div
          role="alert"
          aria-live="assertive"
          class="flex w-full items-start gap-2 rounded-lg border border-status-danger/40 bg-status-danger/10 px-3 py-2 text-left text-sm text-status-danger"
        >
          <span
            class="material-symbols-outlined mt-0.5 text-icon-md shrink-0"
            aria-hidden="true"
          >
            error
          </span>
          <div class="min-w-0 flex-1">
            <p class="break-words font-medium">{error}</p>
          </div>
          {#if onRetry}
            <button
              type="button"
              onclick={onRetry}
              class="shrink-0 rounded border border-status-danger/40 bg-surface-panel px-2 py-0.5 text-xs font-semibold text-status-danger hover:bg-status-danger/20 focus:outline-none focus-visible:ring-2 focus-visible:ring-status-danger"
            >
              Retry
            </button>
          {/if}
        </div>
      {/if}

      <button
        class="onboarding-btn font-label-sm-bold"
        onclick={onSelectFolder}
        disabled={initializing}
        aria-busy={initializing ? 'true' : undefined}
      >
        {initializing ? 'Initializing…' : 'Initialize Workspace Folder'}
      </button>
    </div>
  </div>
{/if}
