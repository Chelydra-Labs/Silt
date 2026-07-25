<script lang="ts">
  // Advanced per-provider tuning: Answer Style / Thinking Depth / Length
  // (chat) and Index Density (embedding), plus the shared Timeout field.
  // Extracted verbatim from AIProviderTab's advancedTuningGrid snippet —
  // the view is a thin layer over the shared reactive controller.
  import { supportsReasoningEffort } from './aiProviderController.svelte'
  import type {
    Which,
    AIProviderController
  } from './aiProviderController.svelte'
  import PresetControl from '../PresetControl.svelte'
  import InfoTooltip from '../InfoTooltip.svelte'
  import { getEmbeddingCapabilities } from '../../../settings/modelCapabilities'

  interface Props {
    which: Which
    ai: AIProviderController
  }
  let { which, ai }: Props = $props()

  // {@const} may not sit directly under a plain element, so these become
  // component-scoped deriveds.
  const b = $derived(ai.config![which])
  const idPrefix = $derived(`ai-${which}`)
  const embedCaps = $derived(
    which === 'embedding' ? getEmbeddingCapabilities(b.model ?? '') : null
  )
</script>

<div class="flex flex-col gap-5">
  {#if which === 'chat'}
    <PresetControl
      label="Answer Style"
      tooltipText="How predictable or creative should the AI's answers be? Lower means more consistent and factual. Higher means more varied and exploratory."
      tooltipTechnical="Technical: Temperature (0.0-2.0)."
      options={[
        {
          value: 0.2,
          label: 'Precise',
          description:
            'Consistent, factual answers. Best for research and facts.'
        },
        {
          value: 0.5,
          label: 'Natural',
          description:
            'Conversational, natural responses. Good for most questions.'
        },
        {
          value: 0.9,
          label: 'Creative',
          description: 'Varied, exploratory answers. Best for brainstorming.'
        }
      ]}
      value={b.temperature ?? 0.5}
      customLabel="Temperature"
      customMin={0}
      customMax={2}
      customStep={0.1}
      onchange={(v) => {
        b.temperature = v
        void ai.persistProvider(which)
      }}
    />

    {#if supportsReasoningEffort(b.provider_type)}
      <PresetControl
        label="Thinking Depth"
        tooltipText="How much the AI works through a problem before answering. Deeper thinking produces more thorough answers but takes longer."
        tooltipTechnical="Technical: Reasoning effort (none-max). Not all models support this."
        options={[
          {
            value: 'none',
            label: 'Quick',
            description: 'Fast responses with light reasoning.'
          },
          {
            value: 'medium',
            label: 'Standard',
            description: 'Balanced reasoning for everyday questions.'
          },
          {
            value: 'high',
            label: 'Deep',
            description:
              'Thorough analysis before answering. Slower but more complete.'
          }
        ]}
        value={b.reasoning_effort ?? 'medium'}
        customLabel="Reasoning effort"
        customSelectOptions={[
          { value: 'none', label: 'none' },
          { value: 'minimal', label: 'minimal' },
          { value: 'low', label: 'low' },
          { value: 'medium', label: 'medium' },
          { value: 'high', label: 'high' },
          { value: 'xhigh', label: 'xhigh' },
          { value: 'max', label: 'max' }
        ]}
        onchange={(v) => {
          b.reasoning_effort = v
          void ai.persistProvider(which)
        }}
      />
    {/if}

    <PresetControl
      label="Answer Length"
      tooltipText="How long should the AI's answer be? Shorter answers are faster."
      tooltipTechnical="Technical: Maximum output tokens."
      options={[
        {
          value: 512,
          label: 'Concise',
          description: 'Short, to-the-point answers.'
        },
        {
          value: 2048,
          label: 'Standard',
          description: 'Moderate length with enough detail.'
        },
        {
          value: 4096,
          label: 'Detailed',
          description: 'In-depth answers with full explanations.'
        }
      ]}
      value={b.max_tokens ?? 2048}
      customLabel="Max tokens"
      customMin={1}
      customStep={1}
      customSuffix="tokens"
      onchange={(v) => {
        b.max_tokens = v
        void ai.persistProvider(which)
      }}
    />
  {/if}

  {#if which === 'embedding'}
    {#if embedCaps?.supportsTruncation === false}
      <div class="flex flex-col gap-1.5">
        <span
          class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
          >Index Density</span
        >
        <span
          class="inline-flex items-center gap-1.5 self-start rounded-lg border border-surface-panel-border bg-surface-panel/40 px-3 py-1.5 text-type-xs text-text-muted"
        >
          Fixed{embedCaps.nativeDimensions
            ? ` at ${embedCaps.nativeDimensions} dimensions`
            : ' dimensions'} (this model doesn't support truncation)
        </span>
      </div>
    {:else}
      <PresetControl
        label="Index Density"
        tooltipText="How detailed each search entry is. Higher means more precise search but more storage. Compact uses truncated dimensions to save space with minimal quality loss."
        tooltipTechnical="Technical: Embedding output dimensions (Matryoshka Representation Learning truncation). Only supported by some models."
        options={[
          {
            value: 0,
            label: 'Auto',
            description:
              "Uses the model's recommended setting. Best for most users."
          },
          {
            value: 768,
            label: 'Compact',
            description:
              'Smaller index, faster search. Slight quality tradeoff.'
          },
          {
            value: 1024,
            label: 'Balanced',
            description: 'Good middle ground for large vaults.'
          }
        ]}
        value={b.dimensions ?? 0}
        customLabel="Dimensions"
        customMin={1}
        customStep={1}
        customSuffix="dimensions"
        onchange={(v) => {
          b.dimensions = v === 0 ? undefined : v
          void ai.persistProvider(which)
        }}
      />
      {#if embedCaps?.supportsTruncation === undefined}
        <p class="text-type-2xs text-text-muted m-0">
          If this model doesn't support truncation, the API will reject it —
          fall back to Auto.
        </p>
      {/if}
    {/if}
  {/if}

  <div class="flex flex-col gap-1.5 max-w-xs">
    <div class="flex items-center gap-1.5">
      <label
        class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
        for="{idPrefix}-timeout">Timeout</label
      >
      <InfoTooltip
        text="How long to wait before giving up on a response. Increase this if you use a slow model."
        technical="Technical: Request timeout in milliseconds."
        label="What is Timeout?"
      />
    </div>
    <input
      id="{idPrefix}-timeout"
      type="number"
      min="1000"
      step="500"
      bind:value={b.timeout_ms}
      onblur={() => void ai.persistProvider(which)}
      class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
    />
    {#if ai.advancedFieldError(which, 'timeout_ms')}
      <span class="text-error text-type-2xs font-label-sm" role="alert"
        >{ai.advancedFieldError(which, 'timeout_ms')}</span
      >
    {/if}
  </div>
</div>
