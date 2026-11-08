<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements'

  /**
   * Token-styled text/number/search input primitive. Automatically wires
   * aria-invalid and aria-describedby to an inline error message element when
   * `error` is a non-empty string.
   */
  interface Props extends Omit<
    HTMLInputAttributes,
    'value' | 'type' | 'class'
  > {
    value?: string | number
    id?: string
    name?: string
    type?: 'text' | 'number' | 'search' | 'password' | 'email' | 'url'
    placeholder?: string
    disabled?: boolean
    readonly?: boolean
    required?: boolean
    min?: number | string
    max?: number | string
    step?: number | string
    autocomplete?: HTMLInputElement['autocomplete']
    error?: string | boolean | null
    errorId?: string
    errorRole?: 'alert' | 'status'
    errorLive?: 'assertive' | 'polite'
    describedBy?: string
    ariaLabel?: string
    ariaLabelledby?: string
    dataTestId?: string
    class?: string
    ref?: HTMLInputElement | null
    oninput?: (e: Event & { currentTarget: HTMLInputElement }) => void
    onchange?: (e: Event & { currentTarget: HTMLInputElement }) => void
    onkeydown?: (e: KeyboardEvent & { currentTarget: HTMLInputElement }) => void
    onblur?: (e: FocusEvent & { currentTarget: HTMLInputElement }) => void
  }

  let {
    value = $bindable(''),
    id,
    name,
    type = 'text',
    placeholder,
    disabled = false,
    readonly = false,
    required = false,
    min,
    max,
    step,
    autocomplete,
    error = null,
    errorId,
    errorRole = 'alert',
    errorLive,
    describedBy,
    ariaLabel,
    ariaLabelledby,
    dataTestId,
    class: className = '',
    ref = $bindable(null),
    oninput,
    onchange,
    onkeydown,
    onblur,
    ...rest
  }: Props = $props()

  const fallbackUid = $props.id()

  const errorText = $derived(
    typeof error === 'string' && error.trim().length > 0 ? error.trim() : null
  )
  const hasError = $derived(Boolean(errorText) || error === true)

  const resolvedErrorId = $derived(
    errorId ??
      (id
        ? `${id}-error`
        : dataTestId
          ? `${dataTestId}-error`
          : `${fallbackUid}-error`)
  )

  const resolvedDescribedBy = $derived.by(() => {
    const parts: string[] = []
    if (describedBy) parts.push(describedBy)
    if (errorText) parts.push(resolvedErrorId)
    return parts.length > 0 ? parts.join(' ') : undefined
  })
</script>

<input
  bind:this={ref}
  bind:value
  {...rest}
  {id}
  {name}
  {type}
  {placeholder}
  {disabled}
  {readonly}
  {required}
  {min}
  {max}
  {step}
  {autocomplete}
  {oninput}
  {onchange}
  {onkeydown}
  {onblur}
  aria-label={ariaLabel ?? (rest['aria-label'] as string | undefined)}
  aria-labelledby={ariaLabelledby ??
    (rest['aria-labelledby'] as string | undefined)}
  aria-invalid={hasError ? true : undefined}
  aria-describedby={resolvedDescribedBy}
  data-testid={dataTestId ??
    ((rest as Record<string, unknown>)['data-testid'] as string | undefined)}
  class="ui-input w-full h-9 px-2.5 rounded-md bg-surface-panel border text-text-primary font-body-md text-type-sm transition-colors placeholder:text-text-muted/60 disabled:opacity-50 disabled:cursor-not-allowed {hasError
    ? 'border-status-danger'
    : 'border-surface-panel-border'} {className}"
/>
{#if errorText}
  <p
    id={resolvedErrorId}
    class="mt-1.5 text-type-2xs font-label-sm text-status-danger"
    role={errorRole}
    aria-live={errorLive ?? (errorRole === 'alert' ? 'assertive' : 'polite')}
  >
    {errorText}
  </p>
{/if}

<style>
  .ui-input:focus {
    outline: none;
  }
  .ui-input:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
</style>
