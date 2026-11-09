<script lang="ts">
  import type { Snippet } from 'svelte'
  import type { HTMLAttributes } from 'svelte/elements'

  /**
   * Shared workspace view header bar for full-pane non-note views
   * (TypeDashboard, TagsExplorer, SettingsPanel) and aligned with TasksHub's
   * top bar. Standardizes padding, border-b, Material Symbols icon rhythm,
   * heading hierarchy (h1/h2), metadata badges, and right-aligned actions.
   */
  interface Props extends Omit<HTMLAttributes<HTMLElement>, 'title' | 'class'> {
    title: string
    titleId?: string
    titleClass?: string
    titleRef?: HTMLHeadingElement | null
    icon?: string
    iconClass?: string
    headingLevel?: 'h1' | 'h2'
    subtitle?: string
    leading?: Snippet
    meta?: Snippet
    actions?: Snippet
    children?: Snippet
    class?: string
    rowClass?: string
    dataTestId?: string
  }

  let {
    title,
    titleId,
    titleClass = '',
    titleRef = $bindable(null),
    icon,
    iconClass = 'text-accent-primary-start',
    headingLevel = 'h2',
    subtitle,
    leading,
    meta,
    actions,
    children,
    class: className = '',
    rowClass = '',
    dataTestId,
    ...rest
  }: Props = $props()
</script>

<header
  {...rest}
  data-testid={dataTestId ??
    ((rest as Record<string, unknown>)['data-testid'] as string | undefined)}
  class="ui-view-header flex flex-col border-b border-surface-panel-border flex-shrink-0 {className}"
>
  <div
    class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-6 py-3.5 sm:flex-nowrap {rowClass}"
  >
    <div class="flex min-w-0 flex-1 flex-wrap items-center gap-2.5">
      {#if leading}
        {@render leading()}
      {/if}
      {#if icon}
        <span
          class="material-symbols-outlined text-icon-lg select-none leading-none flex-shrink-0 {iconClass}"
          aria-hidden="true">{icon}</span
        >
      {/if}
      {#if headingLevel === 'h1'}
        <h1
          bind:this={titleRef}
          id={titleId}
          class="text-type-lg font-semibold text-text-primary tracking-tight truncate m-0 {titleClass}"
        >
          {title}
        </h1>
      {:else}
        <h2
          bind:this={titleRef}
          id={titleId}
          class="text-type-lg font-semibold text-text-primary tracking-tight truncate m-0 {titleClass}"
        >
          {title}
        </h2>
      {/if}
      {#if meta}
        <div class="flex min-w-0 items-center gap-2">
          {@render meta()}
        </div>
      {/if}
    </div>
    {#if actions}
      <div class="flex items-center gap-2 flex-shrink-0 ml-auto">
        {@render actions()}
      </div>
    {/if}
  </div>
  {#if subtitle}
    <p
      class="px-6 pb-3 -mt-2 text-text-muted text-type-sm font-body-md truncate m-0"
    >
      {subtitle}
    </p>
  {/if}
  {#if children}
    {@render children()}
  {/if}
</header>
