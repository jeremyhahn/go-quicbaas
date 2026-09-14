<!--
  CostView renders what the tenant has actually SPENT, read from the
  consumption ledger, with a drill-down along the ledger's own attribution
  chain: tenant -> project -> task -> job -> session -> round.

  WHAT IT DELIBERATELY DOES NOT RENDER (go-quicbaas#794). It used to show a
  gauge reading "0.0% Iterations" and a per-job breakdown of `0/10` — the
  WORKFLOW runner's iteration fields, which are structurally always zero for
  every other runner, presented as if they were measurements of spend. Those
  fields belong on the job page, for the jobs that have them, and they are not
  a cost. Three absences stay visible AS absences here:

    - a cost, while nothing prices a turn, is a dash (go-quicbaas#791);
    - an account nobody capped reads "No spend policy", not "0% of budget";
    - an account the server could not read reads "Spend policy unknown",
      which is not the same reassuring fact.

  See lib/cost.ts, which owns those rules so they are testable rather than
  re-derived inline here.
-->
<script lang="ts">
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import { DollarSign, ChevronRight } from 'lucide-svelte';
  import type { GetSpendReportResponse, SpendChild, SpendLevel } from '@quicbaas/sdk';
  import {
    UNMEASURED,
    childIsDrillable,
    childLabel,
    childScopeValue,
    costIsMeasured,
    formatCost,
    formatTokens,
    levelLabel,
    policyLabel,
    policyVariant,
    scopeLabel,
    spendErrorMessage,
    windowPercent,
    windowSummary,
  } from '../../lib/cost.ts';

  // The scope being read, as the chain's levels. Held as one object so a
  // drill step is a single assignment and the effect below reloads once.
  type Scope = {
    project_id: string;
    task_id: string;
    job_id: string;
    session_id: string;
  };

  const EMPTY_SCOPE: Scope = { project_id: '', task_id: '', job_id: '', session_id: '' };

  let scope = $state<Scope>({ ...EMPTY_SCOPE });
  let report = $state<GetSpendReportResponse | null>(null);
  let loadError = $state('');
  let loading = $state(false);

  // The chain is a PATH, so a drill step sets the next level and every level
  // below it must be cleared. Keeping the order here, once, is what stops a
  // stale deeper level from making the request unanswerable.
  const CHAIN: (keyof Scope)[] = ['project_id', 'task_id', 'job_id', 'session_id'];

  // The levels the breadcrumb names, in chain order.
  const CHAIN_LEVELS: SpendLevel[] = ['project', 'task', 'job', 'session'];

  const breadcrumb = $derived.by((): { label: string; depth: number }[] => {
    const crumbs = [{ label: 'Tenant', depth: 0 }];
    CHAIN.forEach((key, index) => {
      const value = scope[key];
      // scopeLabel maps the ledger's no-value sentinel back to the absence it
      // stands for; printing a raw '-' as a project name is the exact lie
      // this page exists to remove.
      if (value !== '') {
        crumbs.push({ label: scopeLabel(CHAIN_LEVELS[index]!, value), depth: index + 1 });
      }
    });
    return crumbs;
  });

  $effect(() => {
    const tenantID = orchestratorStore.activeTenantID;
    const asked = { ...scope };
    // THE REPORT ON SCREEN BELONGS TO THE BREADCRUMB ABOVE IT. The breadcrumb
    // changes on the click, so leaving the previous scope's report rendered
    // while the new read is in flight prints the parent's totals as the
    // child's -- a confidently wrong number, which is the one thing this page
    // exists to remove. An empty page that says it is loading is honest.
    report = null;
    loadError = '';
    void load(tenantID, asked);
  });

  async function load(tenantID: string, asked: Scope): Promise<void> {
    loading = true;
    try {
      const client = await getSDKClient();
      const resp = await client.orchestrator.getSpendReport({
        tenant_id: tenantID,
        project_id: asked.project_id || undefined,
        task_id: asked.task_id || undefined,
        job_id: asked.job_id || undefined,
        session_id: asked.session_id || undefined,
      });
      // A reply for a scope the operator has already left is discarded rather
      // than rendered under the new breadcrumb.
      if (!isCurrentRequest(tenantID, asked)) return;
      report = resp;
      loadError = '';
    } catch (err: unknown) {
      if (!isCurrentRequest(tenantID, asked)) return;
      // REPORTED, NOT SWALLOWED. A read that failed and a tenant that has
      // spent nothing must never render the same, which is the whole defect
      // this page was filed for.
      report = null;
      loadError = spendErrorMessage(err, 'spend');
    } finally {
      // Only the read still being waited on may say the page has stopped
      // loading. A superseded read clearing this leaves the successor in
      // flight under "No spend report loaded."
      if (isCurrentRequest(tenantID, asked)) loading = false;
    }
  }

  /**
   * isCurrentRequest says whether the reply to (tenantID, asked) is still the
   * one the page is waiting for.
   *
   * THE TENANT IS PART OF THE REQUEST'S IDENTITY, not just the scope. Switching
   * tenant at the same drill depth leaves the chain untouched, so a scope-only
   * check accepts the previous tenant's report and renders it as this one's.
   */
  function isCurrentRequest(tenantID: string, asked: Scope): boolean {
    return tenantID === orchestratorStore.activeTenantID && sameScope(asked, scope);
  }

  function sameScope(a: Scope, b: Scope): boolean {
    return CHAIN.every((key) => a[key] === b[key]);
  }

  function drillInto(child: SpendChild): void {
    if (!childIsDrillable(child)) return;
    const index = CHAIN.indexOf(levelKey(child.level));
    if (index < 0) return;
    const next: Scope = { ...scope };
    next[CHAIN[index]!] = childScopeValue(child);
    // Clear everything below the level just selected.
    for (let i = index + 1; i < CHAIN.length; i++) next[CHAIN[i]!] = '';
    scope = next;
  }

  function levelKey(level: SpendLevel | string): keyof Scope {
    return `${level}_id` as keyof Scope;
  }

  function drillUpTo(depth: number): void {
    const next: Scope = { ...scope };
    for (let i = depth; i < CHAIN.length; i++) next[CHAIN[i]!] = '';
    scope = next;
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader
    title="Cost Tracking"
    subtitle="Provider consumption recorded per turn, rolled up along the attribution chain"
    icon={DollarSign}
  />

  <!-- Breadcrumb: where in the chain this report is taken. -->
  <div class="flex items-center gap-1 text-xs text-base-content/60 flex-wrap">
    {#each breadcrumb as crumb, i (crumb.depth)}
      {#if i > 0}<ChevronRight class="w-3 h-3 shrink-0" />{/if}
      <button
        class="btn btn-ghost btn-xs font-mono"
        class:text-base-content={i === breadcrumb.length - 1}
        disabled={i === breadcrumb.length - 1}
        onclick={() => drillUpTo(crumb.depth)}
      >
        {crumb.label}
      </button>
    {/each}
  </div>

  {#if loadError !== ''}
    <div role="alert" class="alert alert-error">
      <span>{loadError}</span>
    </div>
  {:else if report === null}
    <div class="text-sm text-base-content/50">{loading ? 'Loading spend…' : 'No spend report loaded.'}</div>
  {:else}
    <!-- Totals -->
    <div class="grid grid-cols-2 lg:grid-cols-5 gap-3">
      <div class="stat bg-base-200 rounded-box p-4">
        <div class="stat-title text-xs">Total tokens</div>
        <div class="stat-value text-2xl">{formatTokens(report.total_tokens)}</div>
        <div class="stat-desc">{levelLabel(report.level)} scope</div>
      </div>
      <div class="stat bg-base-200 rounded-box p-4">
        <div class="stat-title text-xs">Input</div>
        <div class="stat-value text-2xl">{formatTokens(report.input_tokens)}</div>
      </div>
      <div class="stat bg-base-200 rounded-box p-4">
        <div class="stat-title text-xs">Output</div>
        <div class="stat-value text-2xl">{formatTokens(report.output_tokens)}</div>
      </div>
      <div class="stat bg-base-200 rounded-box p-4">
        <div class="stat-title text-xs">Turns</div>
        <div class="stat-value text-2xl">{report.entries}</div>
        <div class="stat-desc">{report.successes} ok · {report.failures} failed</div>
      </div>
      <div class="stat bg-base-200 rounded-box p-4">
        <div class="stat-title text-xs">Cost</div>
        <div class="stat-value text-2xl" data-testid="scope-cost">{formatCost(report)}</div>
        <div class="stat-desc">
          {#if costIsMeasured(report)}
            priced
          {:else}
            <!-- Not zero. Nothing prices a turn yet (go-quicbaas#791), and a
                 rendered 0 would claim the work was free. -->
            no price sheet — cost unmeasured
          {/if}
        </div>
      </div>
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Drill-down -->
      <div class="card bg-base-200 rounded-box">
        <div class="card-body p-5">
          <h3 class="text-sm font-semibold mb-3">
            {report.child_level ? `By ${levelLabel(report.child_level).toLowerCase()}` : 'Breakdown'}
          </h3>
          {#if !report.child_level}
            <p class="text-xs text-base-content/50">
              This is the deepest level of the attribution chain; there is nothing below it.
            </p>
          {:else if (report.children ?? []).length === 0}
            <EmptyState
              title="No spend recorded"
              description="No provider turn has been charged to this scope."
            />
          {:else}
            <div class="space-y-2 max-h-96 overflow-y-auto">
              {#each report.children ?? [] as child (childLabel(child))}
                <button
                  class="w-full flex items-center justify-between gap-3 px-3 py-2 rounded-box bg-base-300/50 hover:bg-base-300 transition-colors text-left"
                  class:cursor-default={!childIsDrillable(child)}
                  disabled={!childIsDrillable(child)}
                  onclick={() => drillInto(child)}
                >
                  <span class="text-xs font-mono truncate" class:italic={child.unattributed === true}>
                    {childLabel(child)}
                  </span>
                  <span class="text-xs text-base-content/60 shrink-0">
                    {formatTokens(child.total_tokens)} tokens
                  </span>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      </div>

      <!-- Accounts that paid -->
      <div class="card bg-base-200 rounded-box">
        <div class="card-body p-5">
          <h3 class="text-sm font-semibold mb-3">Accounts that paid</h3>
          {#if (report.accounts ?? []).length === 0}
            <p class="text-xs text-base-content/50">No pooled account was charged in this scope.</p>
          {:else}
            <div class="space-y-4 max-h-96 overflow-y-auto">
              {#each report.accounts ?? [] as account (account.account_id)}
                <div class="space-y-2" data-testid="spend-account">
                  <div class="flex items-center gap-2 flex-wrap">
                    <span class="text-xs font-medium">{account.display_name || account.account_id}</span>
                    {#if account.provider}
                      <span class="badge badge-ghost badge-xs">{account.provider}</span>
                    {/if}
                    <StatusBadge text={policyLabel(account)} variant={policyVariant(account)} />
                    <span class="text-xs text-base-content/50 ml-auto">
                      {formatTokens(account.total_tokens)} tokens
                    </span>
                  </div>

                  {#each account.windows ?? [] as window (window.name)}
                    {@const percent = windowPercent(window)}
                    <div class="space-y-1">
                      <div class="flex justify-between text-xs text-base-content/50">
                        <span>{window.name}</span>
                        <span>{windowSummary(window)}</span>
                      </div>
                      {#if percent !== null}
                        <progress
                          class="progress progress-primary w-full"
                          class:progress-error={window.exhausted}
                          value={percent}
                          max="100"
                          aria-label={`${window.name} quota usage`}
                        ></progress>
                      {/if}
                    </div>
                  {/each}

                  {#if account.breaker}
                    <div class="text-xs text-base-content/50">
                      Circuit breaker: <strong>{account.breaker.state}</strong>
                      ({account.breaker.failures} failures / {account.breaker.successes} successes in window)
                    </div>
                  {/if}

                  {#if account.policy_status !== 'metered'}
                    <div class="text-xs text-base-content/40">
                      {#if account.policy_status === 'none'}
                        No spend cap is configured for this account, so no budget can be shown.
                      {:else}
                        This deployment could not read the account, so its cap is unknown.
                      {/if}
                    </div>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    </div>

    <!-- Models -->
    {#if (report.models ?? []).length > 0}
      <div class="card bg-base-200 rounded-box">
        <div class="card-body p-5">
          <h3 class="text-sm font-semibold mb-3">By model</h3>
          <div class="grid grid-cols-2 lg:grid-cols-4 gap-3">
            {#each report.models ?? [] as model (`${model.engine}/${model.model}`)}
              <div class="stat bg-base-300 rounded-box p-3">
                <div class="stat-title text-xs truncate">{model.model}</div>
                <div class="stat-value text-xl">{formatTokens(model.total_tokens)}</div>
                <div class="stat-desc">{model.engine || UNMEASURED}</div>
              </div>
            {/each}
          </div>
        </div>
      </div>
    {/if}
  {/if}
</div>
