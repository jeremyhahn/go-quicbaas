<!--
  SessionActivityIndicator surfaces the session's TurnStatus (see
  sessionStore.svelte.ts) as a compact header badge so an operator can tell,
  at a glance and without reading the transcript, whether the agent is:

    - working      actively streaming a response / running a tool.
    - pending      a prompt was just sent but no activity has arrived yet.
    - idle         waiting for the next prompt.
    - disconnected the live tail errored or isn't supported on this transport.

  Driven entirely by sessionStore.turnStatus — no local timers or polling.
-->
<script lang="ts">
  import { sessionStore } from '../../stores/sessionStore.svelte.ts';
  import type { TurnStatus } from '../../stores/sessionStore.svelte.ts';
  import StatusBadge from '../core/StatusBadge.svelte';
  import type { StatusVariant } from '../core/StatusBadge.svelte';

  const status = $derived(sessionStore.turnStatus);

  const labels: Record<TurnStatus, string> = {
    working: 'Working',
    pending: 'Waiting',
    idle: 'Idle',
    disconnected: 'Disconnected',
  };

  const variants: Record<TurnStatus, StatusVariant> = {
    working: 'info',
    pending: 'ghost',
    idle: 'ghost',
    disconnected: 'error',
  };

  const ariaLabels: Record<TurnStatus, string> = {
    working: 'Agent is working',
    pending: 'Request sent, waiting for the agent to start',
    idle: 'Agent is idle, awaiting a prompt',
    disconnected: 'Live updates unavailable',
  };
</script>

<span role="status" aria-label={ariaLabels[status]} data-testid="session-activity-indicator" data-status={status}>
  <StatusBadge text={labels[status]} variant={variants[status]} pulse={status === 'working'} />
</span>
