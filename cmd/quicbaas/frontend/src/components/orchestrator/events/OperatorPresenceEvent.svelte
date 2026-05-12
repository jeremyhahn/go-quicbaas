<!--
  OperatorPresenceEvent renders a small chip for operator join/leave events.
-->
<script lang="ts">
  let {
    kind,
    operatorId,
  }: {
    kind: 'operator_joined' | 'operator_left';
    operatorId: string;
  } = $props();
</script>

<div class="flex justify-center my-1" aria-live="polite">
  <span
    class="badge badge-xs gap-1 {kind === 'operator_joined' ? 'badge-success' : 'badge-ghost'}"
    role="status"
  >
    <span aria-hidden="true">{kind === 'operator_joined' ? '&#x2192;' : '&#x2190;'}</span>
    {operatorId}
    {kind === 'operator_joined' ? 'joined' : 'left'}
  </span>
</div>
