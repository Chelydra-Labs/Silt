<script lang="ts">
  import PriorityPill from './PriorityPill.svelte'

  interface Props {
    priority: number
    onCycle: (next: number) => Promise<unknown> | void
    onHostClick: () => void
    onHostKeydown: () => void
  }

  let { priority, onCycle, onHostClick, onHostKeydown }: Props = $props()
</script>

<!-- Test-only event sink: the pill must remain the only real control, so a
     non-interactive host is deliberate here. -->
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
  data-testid="host-row"
  role="group"
  aria-label="Host row"
  onclick={onHostClick}
  onkeydown={onHostKeydown}
>
  <PriorityPill {priority} {onCycle} testId="priority-test" />
</div>
