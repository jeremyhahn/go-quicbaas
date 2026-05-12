<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  BarrierStatusBadge renders a colour-coded label that summarises the
  configured barrier strategy. The strategy is the source of truth for
  every UI affordance on the BarrierView so this badge appears at the top
  of the page and inside hardware-backed banners.
-->
<script lang="ts">
  import type { BarrierStrategy } from '../types.ts';

  interface Props {
    strategy: BarrierStrategy;
    sealed: boolean;
    hardwareBacked: boolean;
  }

  const { strategy, sealed, hardwareBacked }: Props = $props();

  // Tailwind/DaisyUI colour class for the badge. Disabled stays neutral so
  // it does not draw attention as an alert state.
  const variantClass: Record<BarrierStrategy, string> = {
    'disabled':   'badge-ghost',
    'software':   'badge-info',
    'shamir':     'badge-warning',
    'tpm2':       'badge-success',
    'pkcs11':     'badge-success',
    'aws-kms':    'badge-success',
    'gcp-kms':    'badge-success',
    'azure-kms':  'badge-success',
    'vault-kms':  'badge-success',
  };

  const labelClass = $derived(variantClass[strategy] ?? 'badge-ghost');
</script>

<div class="flex items-center gap-2" data-testid="barrier-status-badge">
  <span
    class="badge badge-lg font-semibold {labelClass}"
    aria-label="Barrier strategy"
  >
    {strategy}
  </span>
  <span
    class="badge badge-sm font-mono"
    class:badge-error={sealed}
    class:badge-success={!sealed}
    aria-label="Barrier sealed state"
  >
    {sealed ? 'sealed' : 'unsealed'}
  </span>
  {#if hardwareBacked}
    <span class="badge badge-sm badge-outline" aria-label="Hardware backed">
      hardware_backed
    </span>
  {/if}
</div>
