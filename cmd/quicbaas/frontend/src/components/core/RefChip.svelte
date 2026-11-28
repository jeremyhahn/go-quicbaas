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
  RefChip is how a SAVED reference renders, everywhere RefPicker is how one
  gets EDITED: a small pill carrying the kind's glyph, the object's name, and
  a status dot, linking to its detail view when the app has one.

  It resolves its own label and status from the kind's RefProvider — a
  caller hands it the kind and the stored value, never a pre-fetched label,
  so a chip is never stale relative to what the picker itself would show.

  A MISSING target is not hidden or quietly relabelled: the dot goes red,
  the text stays the raw value (there is no name to show), and a warning
  glyph replaces the kind's own — a reference nothing resolves is exactly
  the failure this component exists to make visible.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { TriangleAlert } from 'lucide-svelte';
  import '../../lib/refs/providers.ts';
  import { getRefProvider } from '../../lib/refs/registry.ts';
  import { refStatusLabel, refStatusTone } from '../../lib/refs/status.ts';
  import type { RefKind, RefProviderContext, RefStatus } from '../../lib/refs/types.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import { themeStore } from '../../stores/theme.svelte.ts';
  import { STATUS_TONES } from '../../lib/theme/tokens.ts';

  let {
    kind,
    value,
    tenantID,
    context,
    linkable = true,
  }: {
    kind: RefKind;
    value: string;
    tenantID?: string;
    context?: Record<string, unknown>;
    linkable?: boolean;
  } = $props();

  const provider = $derived(getRefProvider(kind));
  const ctx = $derived<RefProviderContext>({ tenantID: tenantID ?? authStore.tenantID, ...(context ?? {}) });

  let label = $state(untrack(() => value));
  let status = $state<RefStatus | undefined>(undefined);
  let resolvedFor: string | undefined;

  async function resolve(): Promise<void> {
    if (provider === undefined || value === '') return;
    if (resolvedFor === value) return;
    resolvedFor = value;
    try {
      const found = (await provider.search(value, ctx)).find((o) => o.value === value);
      if (found !== undefined) {
        label = found.label;
        status = found.status;
        return;
      }
    } catch {
      // fall through to the direct status probe below
    }
    label = value;
    status = provider.status !== undefined ? await provider.status(value, ctx).catch(() => 'unknown' as RefStatus) : undefined;
  }

  $effect(() => {
    void value;
    void resolve();
  });

  const tone = $derived(themeStore.theme === 'light' ? STATUS_TONES.light : STATUS_TONES.dark);
  const missing = $derived(status === 'missing');
  const route = $derived(provider?.detailRoute?.(value, ctx));
  const href = $derived(linkable && route !== undefined ? `#${route}` : undefined);
</script>

{#if value !== ''}
  {#if provider === undefined}
    <span class="inline-flex items-center gap-1 rounded-full border border-base-300 px-2 py-0.5 font-mono text-xs text-base-content/70" data-testid="ref-chip-{kind}">
      {value}
    </span>
  {:else}
    {@const Glyph = provider.glyph}
    <svelte:element
      this={href !== undefined ? 'a' : 'span'}
      {href}
      class="inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium leading-none {href !== undefined ? 'hover:border-primary hover:text-primary' : ''} {missing ? 'border-error/40 text-error' : 'border-base-300 text-base-content/80'}"
      title="{provider.label}: {label}{status !== undefined ? ` (${refStatusLabel(status)})` : ''}"
      data-testid="ref-chip-{kind}"
      data-status={status ?? 'unknown'}
    >
      {#if missing}
        <TriangleAlert class="h-3 w-3 shrink-0" aria-hidden="true" />
      {:else}
        <Glyph class="h-3 w-3 shrink-0 opacity-60" aria-hidden="true" />
      {/if}
      <span class="max-w-[16rem] truncate font-mono">{label}</span>
      {#if missing}
        <span class="text-[10px] uppercase tracking-wide">missing</span>
      {:else if status !== undefined}
        <span
          class="h-1.5 w-1.5 shrink-0 rounded-full"
          style:background-color={tone[refStatusTone(status)].fg}
          aria-hidden="true"
        ></span>
      {/if}
    </svelte:element>
  {/if}
{/if}
