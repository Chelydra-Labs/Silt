<script lang="ts">
  import PinToggle from './PinToggle.svelte'

  interface Props {
    onToggle: (next: boolean) => Promise<unknown>
    onHostClick: () => void
    onHostKeydown: () => void
  }

  let { onToggle, onHostClick, onHostKeydown }: Props = $props()
</script>

<!-- Test-only event sink: the toggle must remain the only real control, so a
     non-interactive host is deliberate here. -->
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
  data-testid="host-row"
  role="group"
  aria-label="Host row"
  onclick={onHostClick}
  onkeydown={onHostKeydown}
>
  <PinToggle pinned={false} {onToggle} testId="pin-test" />
</div>
