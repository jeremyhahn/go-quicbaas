<!--
  Copyright (c) 2026 Jeremy Hahn
  Copyright (c) 2026 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  AttentionBanner renders every active environmental alert (an agent
  offline, a pooled credential erroring, the node's barrier sealed) at the
  top of the page it is mounted on. Each alert says what to do about it and
  can be dismissed for the rest of this browser session — dismissal never
  resolves the alert itself, only the banner's own visibility, so the item
  still counts in the "Needs you" inbox until whatever it names is fixed.
-->
<script lang="ts">
  import { attentionStore, dismissAlert } from '../../stores/attentionStore.svelte.ts';
  import { routeAttention } from '../../stores/router.svelte.ts';
  import { severityVariant, kindLabel } from '../../lib/attention.ts';
  import { AlertTriangle, X } from 'lucide-svelte';

  const ALERT_REMEDY: Record<string, string> = {
    agent_offline: 'Repair or remove the agent from the fleet.',
    account_error: 'Rotate or re-authorize the pooled credential.',
    barrier_sealed: 'Unseal the barrier before any data operation can run.',
  };
</script>

{#if attentionStore.visibleAlerts.length > 0}
  <div class="flex flex-col gap-2 px-4 pt-4" data-testid="attention-banner">
    {#each attentionStore.visibleAlerts as alert (alert.id)}
      <div class="alert {severityVariant(alert.severity) === 'error' ? 'alert-error' : 'alert-warning'} shadow-sm items-start">
        <AlertTriangle class="h-5 w-5 shrink-0 mt-0.5" />
        <div class="flex-1">
          <p class="font-semibold text-sm">{kindLabel(alert.kind)}: {alert.subject}</p>
          <p class="text-sm opacity-90">{alert.message}</p>
          <p class="text-xs opacity-75 mt-1">{ALERT_REMEDY[alert.kind] ?? ''}</p>
        </div>
        <button class="btn btn-ghost btn-xs" onclick={() => routeAttention()}>View</button>
        <button
          class="btn btn-ghost btn-xs btn-circle"
          aria-label="Dismiss this alert for this session"
          onclick={() => dismissAlert(alert.id)}
          data-testid="attention-banner-dismiss"
        >
          <X class="h-4 w-4" />
        </button>
      </div>
    {/each}
  </div>
{/if}
