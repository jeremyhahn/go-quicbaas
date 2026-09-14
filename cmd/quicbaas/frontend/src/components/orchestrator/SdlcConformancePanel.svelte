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
  SdlcConformancePanel answers, for one bound project, "is QuicBaaS actually
  able to drive this repository, and if not, what is wrong".

  IT SHOWS EVERY CHECK, INCLUDING THE ONES THAT PASSED. A panel listing only
  failures cannot be told apart from one whose checks never ran — which is
  precisely the confusion an unwired check creates, and the reason this exists
  as a surface rather than a script somebody remembers to run.

  EVERY FINDING CARRIES ITS REMEDY, because the repairs differ. Provisioning
  fixes a missing webhook or label; a workflow still committed to the
  repository is a commit somebody has to make; branch protection is a setting
  only the repository's owner may change. The repair button is therefore
  offered only when provisioning would actually change something — see
  hasRepairableFindings.
-->
<script lang="ts">
  import type { ConformanceReport } from '@quicbaas/sdk';
  import {
    checkStatusClass,
    conformanceBadgeClass,
    conformanceSummary,
    hasRepairableFindings,
    isRepairable,
  } from '../../lib/sdlcConformance.ts';
  import { RefreshCw, Wrench } from 'lucide-svelte';

  let {
    report,
    busy = false,
    error = '',
    onRefresh,
    onRepair,
  }: {
    report: ConformanceReport | null;
    busy?: boolean;
    error?: string;
    onRefresh: () => void;
    onRepair: () => void;
  } = $props();

  const findings = $derived(report?.findings ?? []);
  const repairable = $derived(hasRepairableFindings(report));
</script>

<div class="rounded-box bg-base-100 p-3 flex flex-col gap-2" data-testid="sdlc-conformance">
  <div class="flex items-center gap-2 flex-wrap">
    <span class="badge badge-sm {conformanceBadgeClass(report)}" data-testid="conformance-badge">
      {report?.conformant ? 'conformant' : 'not conformant'}
    </span>
    <span class="text-xs">{conformanceSummary(report)}</span>
    <span class="grow"></span>
    <button
      class="btn btn-xs btn-ghost"
      disabled={busy}
      onclick={onRefresh}
      data-testid="conformance-refresh"
    >
      <RefreshCw class="w-3 h-3" /> Re-check
    </button>
    {#if repairable}
      <!--
        Provisioning is a WRITE to the forge, kept behind its own button so
        reading this panel can never change a repository.
      -->
      <button
        class="btn btn-xs btn-primary"
        disabled={busy}
        onclick={onRepair}
        data-testid="conformance-repair"
      >
        <Wrench class="w-3 h-3" /> Provision
      </button>
    {/if}
  </div>

  {#if error}
    <p class="text-xs text-error" data-testid="conformance-error">{error}</p>
  {/if}

  {#if report}
    <div class="flex flex-wrap gap-1">
      {#each report.checks ?? [] as check (check.name)}
        <span class="badge badge-xs {checkStatusClass(check.status)}" title={check.status}>
          {check.name}
        </span>
      {/each}
    </div>

    {#if findings.length > 0}
      <ul class="flex flex-col gap-2 mt-1" data-testid="conformance-findings">
        {#each findings as finding (finding.class + finding.subject)}
          <li class="text-xs border-l-2 border-error pl-2">
            <div class="font-mono">
              {finding.class}{finding.subject ? ` · ${finding.subject}` : ''}
              {#if !isRepairable(finding)}
                <span class="badge badge-xs badge-ghost ml-1">manual</span>
              {/if}
            </div>
            <div class="opacity-70">{finding.detail}</div>
            <div class="opacity-90">Fix: {finding.remedy}</div>
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</div>
