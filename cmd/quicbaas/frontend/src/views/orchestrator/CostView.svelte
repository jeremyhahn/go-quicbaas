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
  import CostSettingsPanel from '../../components/orchestrator/CostSettingsPanel.svelte';
  import TenantSpendLimitsEditor from '../../components/orchestrator/TenantSpendLimitsEditor.svelte';
  import AgentSpendLimitsEditor from '../../components/orchestrator/AgentSpendLimitsEditor.svelte';
  import { Bot, DollarSign, ChevronRight, Server, X } from 'lucide-svelte';
  import type { GetSpendReportResponse, SpendAgent, SpendChild, SpendHost, SpendLevel } from '@quicbaas/sdk';
  import {
    UNMEASURED,
    agentFilterLabel,
    agentLabel,
    agentScopeValue,
    childIsDrillable,
    childIsNamed,
    childLabel,
    childRound,
    childScopeValue,
    billedNote,
    billingLabel,
    energyNote,
    equivalentNote,
    formatBilled,
    formatBilledFloor,
    formatEquivalent,
    formatEquivalentFloor,
    formatEnergy,
    formatFeeShare,
    formatTokens,
    hostFilterLabel,
    hostLabel,
    hostScopeValue,
    levelLabel,
    measuredShare,
    moneyNote,
    policyLabel,
    policyVariant,
    reportProjectName,
    roundLabel,
    SPEND_RANGES,
    type SpendRange,
    scopeLabel,
    spendErrorMessage,
    spendRangeFrom,
    windowPercent,
    windowSummary,
  } from '../../lib/cost.ts';

  // The scope being read, as the chain's levels. Held as one object so a
  // drill step is a single assignment and the effect below reloads once.
  //
  // THE ROUND IS NOT PART OF THE ID CHAIN (go-quicbaas#886). It is the
  // terminal level and a number, where round 0 is a real round, so it is held
  // as `null` for "every round" rather than as an empty string, and is sent as
  // `round` with `round_set`.
  type ChainKey = 'project_id' | 'task_id' | 'job_id' | 'session_id';
  //
  // THE RANGE IS PART OF THE SCOPE, so changing it reloads exactly as a drill
  // step does and a reply for the previous range is discarded, never rendered
  // under the new one.
  //
  // THE HOST IS A FILTER, NOT A LEVEL. It narrows whatever scope is on screen
  // to the turns that ran on one host, and survives drilling up and down the
  // chain, exactly as the range does. '' is every host.
  //
  // THE AGENT IS A FILTER TOO, exactly like the host: the turns of the
  // sessions one tenant Agent ran. Narrowed to one, the report also carries
  // that Agent's own spend limits. '' is every Agent.
  type Scope = Record<ChainKey, string> & { round: number | null; range: SpendRange; host: string; agent: string };

  const EMPTY_SCOPE: Scope = { project_id: '', task_id: '', job_id: '', session_id: '', round: null, range: 'all', host: '', agent: '' };

  // Whether the tenant's own limits are open for editing.
  let editingTenantLimits = $state(false);

  // Whether the narrowed-to Agent's own limits are open for editing.
  let editingAgentLimits = $state(false);

  let scope = $state<Scope>({ ...EMPTY_SCOPE });
  let report = $state<GetSpendReportResponse | null>(null);
  let loadError = $state('');
  let loading = $state(false);
  // projectName is the name of the project the scope has drilled into, so the
  // breadcrumb reads as the project rather than its id (go-quicbaas#1058).
  let projectName = $state('');

  // The chain is a PATH, so a drill step sets the next level and every level
  // below it must be cleared. Keeping the order here, once, is what stops a
  // stale deeper level from making the request unanswerable.
  const CHAIN: ChainKey[] = ['project_id', 'task_id', 'job_id', 'session_id'];

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
        const name = key === 'project_id' ? projectName : '';
        crumbs.push({ label: scopeLabel(CHAIN_LEVELS[index]!, value, name), depth: index + 1 });
      }
    });
    if (scope.round !== null) crumbs.push({ label: roundLabel(scope.round), depth: CHAIN.length + 1 });
    return crumbs;
  });

  // The tenant the name on the breadcrumb was read under. A tenant switch
  // clears the name at once, so another tenant's project name is never shown
  // while the new tenant's report is in flight.
  let nameTenantID = '';

  $effect(() => {
    const tenantID = orchestratorStore.activeTenantID;
    const asked = { ...scope };
    if (tenantID !== nameTenantID) {
      nameTenantID = tenantID;
      projectName = '';
    }
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
        round: asked.round ?? undefined,
        // Presence, not value: round 0 is a real round, and an unset round
        // must widen the scope to every round rather than narrow it to 0.
        round_set: asked.round !== null,
        from: spendRangeFrom(asked.range, new Date()),
        host: asked.host || undefined,
        agent: asked.agent || undefined,
      });
      // A reply for a scope the operator has already left is discarded rather
      // than rendered under the new breadcrumb.
      if (!isCurrentRequest(tenantID, asked)) return;
      report = resp;
      projectName = reportProjectName(resp);
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
    return CHAIN.every((key) => a[key] === b[key]) && a.round === b.round && a.range === b.range && a.host === b.host && a.agent === b.agent;
  }

  function drillInto(child: SpendChild): void {
    if (!childIsDrillable(child)) return;
    // A round row is the terminal level: it narrows the session to one round
    // and leaves the id chain above it as it is.
    const round = childRound(child);
    if (round !== null) {
      scope = { ...scope, round };
      return;
    }
    const index = CHAIN.indexOf(levelKey(child.level));
    if (index < 0) return;
    const next: Scope = { ...scope };
    next[CHAIN[index]!] = childScopeValue(child);
    // The breadcrumb names the project it opened, as the row did.
    if (index === 0) projectName = childIsNamed(child) ? (child.name ?? '') : '';
    // Clear everything below the level just selected.
    for (let i = index + 1; i < CHAIN.length; i++) next[CHAIN[i]!] = '';
    next.round = null;
    scope = next;
  }

  /** narrowToHost narrows the scope on screen to one host's turns. */
  function narrowToHost(host: SpendHost): void {
    scope = { ...scope, host: hostScopeValue(host) };
  }

  /** narrowToAgent narrows the scope on screen to one Agent's turns. */
  function narrowToAgent(agent: SpendAgent): void {
    editingAgentLimits = false;
    scope = { ...scope, agent: agentScopeValue(agent) };
  }

  /** reload re-reads the scope on screen, after a change the report reflects. */
  function reload(): void {
    scope = { ...scope };
  }

  function levelKey(level: SpendLevel | string): ChainKey {
    return `${level}_id` as ChainKey;
  }

  function drillUpTo(depth: number): void {
    if (depth === 0) projectName = '';
    const next: Scope = { ...scope };
    for (let i = depth; i < CHAIN.length; i++) next[CHAIN[i]!] = '';
    // Every crumb a click can reach sits above the round, so climbing always
    // widens back to every round.
    next.round = null;
    scope = next;
  }
</script>

<div class="page">
  <PageHeader
    title="Cost Tracking"
    subtitle="Provider consumption recorded per turn, rolled up along the attribution chain"
    icon={DollarSign}
  />

  <!-- Where in the attribution chain this report is taken. Each level above
       the current one is a way back up. -->
  <nav class="-mt-2 flex flex-wrap items-center gap-1 text-sm" aria-label="Spend scope">
    <span class="mr-1 text-xs font-medium text-base-content/60">Scope</span>
    {#each breadcrumb as crumb, i (crumb.depth)}
      {#if i > 0}<ChevronRight class="h-3.5 w-3.5 shrink-0 text-base-content/40" aria-hidden="true" />{/if}
      <button
        class="rounded-field px-1.5 py-0.5 transition-colors enabled:text-primary enabled:hover:bg-base-200 disabled:font-semibold disabled:text-base-content"
        disabled={i === breadcrumb.length - 1}
        aria-current={i === breadcrumb.length - 1 ? 'location' : undefined}
        onclick={() => drillUpTo(crumb.depth)}
      >
        {crumb.label}
      </button>
    {/each}
    {#if scope.host !== ''}
      <span class="ml-2 inline-flex items-center gap-1 rounded-field border border-base-300/80 bg-base-200 py-0.5 pl-2 pr-1 text-xs" data-testid="spend-host-filter">
        <Server class="h-3.5 w-3.5 text-base-content/60" aria-hidden="true" />
        <span class="font-mono">{hostFilterLabel(scope.host)}</span>
        <button
          class="rounded-field p-0.5 text-base-content/60 hover:bg-base-300 hover:text-base-content"
          aria-label="Show every host"
          onclick={() => (scope = { ...scope, host: '' })}
        >
          <X class="h-3 w-3" />
        </button>
      </span>
    {/if}
    {#if scope.agent !== ''}
      <span class="ml-2 inline-flex items-center gap-1 rounded-field border border-base-300/80 bg-base-200 py-0.5 pl-2 pr-1 text-xs" data-testid="spend-agent-filter">
        <Bot class="h-3.5 w-3.5 text-base-content/60" aria-hidden="true" />
        <span class="font-mono">{agentFilterLabel(scope.agent)}</span>
        <button
          class="rounded-field p-0.5 text-base-content/60 hover:bg-base-300 hover:text-base-content"
          aria-label="Show every Agent"
          onclick={() => { editingAgentLimits = false; scope = { ...scope, agent: '' }; }}
        >
          <X class="h-3 w-3" />
        </button>
      </span>
    {/if}
    <label class="ml-auto flex items-center gap-2 text-xs text-base-content/60">
      Range
      <select
        class="select select-xs"
        aria-label="Spend range"
        data-testid="spend-range"
        value={scope.range}
        onchange={(event) => (scope = { ...scope, range: event.currentTarget.value as SpendRange })}
      >
        {#each SPEND_RANGES as option (option.value)}
          <option value={option.value}>{option.label}</option>
        {/each}
      </select>
    </label>
  </nav>

  {#if loadError !== ''}
    <div role="alert" class="alert alert-error">
      <span>{loadError}</span>
    </div>
  {:else if report === null}
    <div class="flex items-center gap-2 rounded-box border border-base-300/80 bg-base-200 px-5 py-8 text-sm text-base-content/60">
      {#if loading}<span class="loading loading-spinner loading-sm text-primary"></span>{/if}
      {loading ? 'Loading spend…' : 'No spend report loaded.'}
    </div>
  {:else}
    <!-- Money first: this is a cost page. Two money figures (go-quicbaas#1058):
         the list-price equivalent is what the tokens would cost on the
         provider's metered API; billed is what they added to the bill actually
         paid — zero per turn on a subscription. Neither renders a zero it did
         not measure. Local inference is costed from electricity: the energy
         those turns drew; their dollars are in both money figures. -->
    <section class="grid grid-cols-1 gap-3 sm:grid-cols-3" aria-label="Spend">
      <div class="stat min-w-0 rounded-box border border-base-300/80 bg-base-200 px-5 py-4">
        <div class="stat-title text-xs font-medium text-base-content/60">List-price equivalent</div>
        <div class="stat-value display text-[2.25rem]" data-testid="scope-cost">{formatEquivalent(report, report.currency)}</div>
        <div class="stat-desc mt-1 whitespace-normal text-xs text-base-content/60">{equivalentNote(report)}</div>
      </div>
      <div class="stat min-w-0 rounded-box border border-base-300/80 bg-base-200 px-5 py-4">
        <div class="stat-title text-xs font-medium text-base-content/60">Billed</div>
        <div class="stat-value display text-[2.25rem]" data-testid="scope-billed">{formatBilled(report, report.currency)}</div>
        <div class="stat-desc mt-1 whitespace-normal text-xs text-base-content/60">{billedNote(report)}</div>
        {#if typeof report.subscription_fee_micros === 'number'}
          <!-- Subscriptions bill nothing per turn; their flat fee, pro-rated
               over the range, is shown beside the per-turn figure rather than
               folded into it. -->
          <div class="stat-desc whitespace-normal text-xs text-base-content/60" data-testid="scope-subscription-fees">
            + {formatFeeShare(report.subscription_fee_micros, report.currency)} subscription fees over the range
          </div>
        {/if}
      </div>
      <div class="stat min-w-0 rounded-box border border-base-300/80 bg-base-200 px-5 py-4">
        <div class="stat-title text-xs font-medium text-base-content/60">Local energy</div>
        <div class="stat-value display text-[2.25rem]" data-testid="scope-energy">{formatEnergy(report)}</div>
        <div class="stat-desc mt-1 whitespace-normal text-xs text-base-content/60">{energyNote(report)}</div>
      </div>
    </section>

    <!-- The scope's OWN spend limits (go-quicbaas#1058): a project's at the
         project level, the tenant's at the tenant level. A spent window
         defers admission for the project or the whole tenant. -->
    {#if report.scope_policy_status}
      <section class="rounded-box border border-base-300/80 bg-base-200 px-5 py-4" aria-label="Spend limits" data-testid="scope-limits">
        <div class="mb-2 flex items-center gap-2">
          <h3 class="text-[0.9375rem] font-semibold">{levelLabel(report.level)} spend limits</h3>
          <StatusBadge text={policyLabel({ policy_status: report.scope_policy_status })} variant={policyVariant({ policy_status: report.scope_policy_status })} />
          {#if report.level === 'tenant' && !editingTenantLimits}
            <!-- A project's limits are edited with the project; the tenant's
                 have no other home, so they are edited here. -->
            <button class="btn btn-ghost btn-xs ml-auto text-primary" data-testid="edit-tenant-limits" onclick={() => (editingTenantLimits = true)}>
              Edit limits
            </button>
          {/if}
        </div>
        {#if report.level === 'tenant' && editingTenantLimits}
          <TenantSpendLimitsEditor
            tenantID={orchestratorStore.activeTenantID}
            onclose={() => (editingTenantLimits = false)}
            onsaved={() => { editingTenantLimits = false; reload(); }}
          />
        {/if}
        {#if report.scope_policy_status === 'none'}
          <p class="text-xs text-base-content/60">No spend limit is set here, so admission is never deferred for spend at this level.</p>
        {:else if report.scope_policy_status === 'unknown'}
          <p class="text-xs text-base-content/60">This deployment could not read the limits, so they are unknown.</p>
        {:else}
          <div class="space-y-2">
            {#each report.scope_windows ?? [] as window (window.name)}
              {@const percent = windowPercent(window)}
              <div class="space-y-1" data-testid="scope-window">
                <div class="flex justify-between text-xs text-base-content/60">
                  <span>{window.name}{#if window.exhausted} · <strong class="text-error">spent — admission deferred</strong>{/if}</span>
                  <span class="tabular-nums">{windowSummary(window)}</span>
                </div>
                {#if percent !== null}
                  <progress
                    class="progress progress-primary h-1.5 w-full"
                    class:progress-error={window.exhausted}
                    value={percent}
                    max="100"
                    aria-label={`${window.name} spend limit usage`}
                  ></progress>
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      </section>
    {/if}

    <!-- The narrowed-to Agent's OWN spend limits: a spent window passes a
         pool's work to its next Agent and holds work naming this Agent alone.
         The project's and tenant's limits above still apply. -->
    {#if report.agent_policy_status}
      <section class="rounded-box border border-base-300/80 bg-base-200 px-5 py-4" aria-label="Agent spend limits" data-testid="agent-limits">
        <div class="mb-2 flex items-center gap-2">
          <h3 class="text-[0.9375rem] font-semibold">Agent {report.agent} spend limits</h3>
          <StatusBadge text={policyLabel({ policy_status: report.agent_policy_status })} variant={policyVariant({ policy_status: report.agent_policy_status })} />
          {#if report.agent_policy_status !== 'unknown' && !editingAgentLimits}
            <button class="btn btn-ghost btn-xs ml-auto text-primary" data-testid="edit-agent-limits" onclick={() => (editingAgentLimits = true)}>
              Edit limits
            </button>
          {/if}
        </div>
        {#if editingAgentLimits && report.agent}
          <AgentSpendLimitsEditor
            tenantID={orchestratorStore.activeTenantID}
            agentID={report.agent}
            onclose={() => (editingAgentLimits = false)}
            onsaved={() => { editingAgentLimits = false; reload(); }}
          />
        {/if}
        {#if report.agent_policy_status === 'none'}
          <p class="text-xs text-base-content/60">This Agent has no spend limit of its own, so it is never passed over for spend.</p>
        {:else if report.agent_policy_status === 'unknown'}
          <p class="text-xs text-base-content/60">This Agent's limits could not be read: the catalog is not wired here, or the Agent no longer exists.</p>
        {:else}
          <div class="space-y-2">
            {#each report.agent_windows ?? [] as window (window.name)}
              {@const percent = windowPercent(window)}
              <div class="space-y-1" data-testid="agent-window">
                <div class="flex justify-between text-xs text-base-content/60">
                  <span>{window.name}{#if window.exhausted} · <strong class="text-error">spent — passed over until it frees</strong>{/if}</span>
                  <span class="tabular-nums">{windowSummary(window)}</span>
                </div>
                {#if percent !== null}
                  <progress
                    class="progress progress-primary h-1.5 w-full"
                    class:progress-error={window.exhausted}
                    value={percent}
                    max="100"
                    aria-label={`${window.name} Agent spend limit usage`}
                  ></progress>
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      </section>
    {/if}

    <!-- Volume: one ledger strip rather than four more tiles competing with
         the money above. -->
    <section
      class="grid grid-cols-2 divide-base-300/80 rounded-box border border-base-300/80 bg-base-200 max-md:gap-y-3 max-md:py-3 md:grid-cols-4 md:divide-x"
      aria-label="Volume"
    >
      <div class="stat min-w-0 px-5 py-3">
        <div class="stat-title text-xs font-medium text-base-content/60">Total tokens</div>
        <div class="stat-value text-xl font-semibold">{formatTokens(report.total_tokens)}</div>
        <div class="stat-desc whitespace-normal text-xs text-base-content/60">{levelLabel(report.level)} scope</div>
      </div>
      <div class="stat min-w-0 px-5 py-3">
        <div class="stat-title text-xs font-medium text-base-content/60">Input</div>
        <div class="stat-value text-xl font-semibold">{formatTokens(report.input_tokens)}</div>
        <div class="stat-desc whitespace-normal text-xs text-base-content/60">
          {formatTokens(report.cache_read_tokens ?? 0)} cache read · {formatTokens(report.cache_write_tokens ?? 0)} written
        </div>
      </div>
      <div class="stat min-w-0 px-5 py-3">
        <div class="stat-title text-xs font-medium text-base-content/60">Output</div>
        <div class="stat-value text-xl font-semibold">{formatTokens(report.output_tokens)}</div>
      </div>
      <div class="stat min-w-0 px-5 py-3">
        <div class="stat-title text-xs font-medium text-base-content/60">Turns</div>
        <div class="stat-value text-xl font-semibold">{formatTokens(report.entries ?? 0)}</div>
        <div class="stat-desc whitespace-normal text-xs text-base-content/60">{report.successes ?? 0} ok · {report.failures ?? 0} failed</div>
      </div>
    </section>

    <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
      <!-- Drill-down -->
      <div class="card rounded-box bg-base-200">
        <div class="card-body gap-0 p-5">
          <h3 class="mb-3 text-[0.9375rem] font-semibold">
            {report.child_level ? `By ${levelLabel(report.child_level).toLowerCase()}` : 'Breakdown'}
          </h3>
          {#if !report.child_level}
            <p class="text-sm text-base-content/60">
              This is the deepest level of the attribution chain; there is nothing below it.
            </p>
          {:else if (report.children ?? []).length === 0}
            <EmptyState
              title="No spend recorded"
              description="No provider turn has been charged to this scope."
            />
          {:else}
            <div class="-mx-2 max-h-96 divide-y divide-base-300/70 overflow-y-auto">
              {#each report.children ?? [] as child (childScopeValue(child))}
                <button
                  class="group flex w-full flex-col gap-0.5 rounded-field px-2 py-2.5 text-left transition-colors enabled:hover:bg-base-300/50"
                  class:cursor-default={!childIsDrillable(child)}
                  disabled={!childIsDrillable(child)}
                  onclick={() => drillInto(child)}
                >
                  <span class="flex w-full items-baseline justify-between gap-3">
                    <span
                      class="truncate text-sm font-medium"
                      class:font-mono={!childIsNamed(child)}
                      class:italic={child.unattributed === true}
                      title={childIsNamed(child) ? child.id : undefined}
                    >
                      {childLabel(child)}
                    </span>
                    <span class="shrink-0 text-sm font-semibold tabular-nums">{formatTokens(child.total_tokens)} <span class="font-normal text-base-content/60">tokens</span></span>
                  </span>
                  <span
                    class="text-xs tabular-nums text-base-content/60"
                    title={moneyNote(child)}
                    data-testid="spend-child-money"
                  >
                    equivalent {formatEquivalentFloor(child, report.currency)}
                    · billed {formatBilledFloor(child, report.currency)}
                    {#if (child.electricity_entries ?? 0) > 0}· {formatEnergy(child)}{/if}
                  </span>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      </div>

      <!-- Accounts that paid -->
      <div class="card rounded-box bg-base-200">
        <div class="card-body gap-0 p-5">
          <h3 class="mb-3 text-[0.9375rem] font-semibold">Accounts that paid</h3>
          {#if (report.accounts ?? []).length === 0}
            <p class="text-sm text-base-content/60">No pooled account was charged in this scope.</p>
          {:else}
            <div class="max-h-96 divide-y divide-base-300/70 overflow-y-auto">
              {#each report.accounts ?? [] as account (account.account_id)}
                <div class="space-y-2 py-3 first:pt-0 last:pb-0" data-testid="spend-account">
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="text-sm font-medium">{account.display_name || account.account_id}</span>
                    {#if account.provider}
                      <span class="badge badge-ghost badge-xs">{account.provider}</span>
                    {/if}
                    <StatusBadge text={policyLabel(account)} variant={policyVariant(account)} />
                    <span class="ml-auto text-sm font-semibold tabular-nums">
                      {formatTokens(account.total_tokens)} <span class="font-normal text-base-content/60">tokens</span>
                    </span>
                  </div>
                  <div
                    class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-base-content/60"
                    title={moneyNote(account)}
                    data-testid="spend-account-money"
                  >
                    <span>{billingLabel(account.billing, report.currency)}</span>
                    <span class="tabular-nums">equivalent {formatEquivalentFloor(account, report.currency)}</span>
                    <span class="tabular-nums">billed {formatBilledFloor(account, report.currency)}</span>
                    {#if typeof account.subscription_fee_micros === 'number'}
                      <span class="tabular-nums" data-testid="spend-account-fee">
                        fee over range {formatFeeShare(account.subscription_fee_micros, report.currency)}
                      </span>
                    {/if}
                  </div>

                  {#each account.windows ?? [] as window (window.name)}
                    {@const percent = windowPercent(window)}
                    <div class="space-y-1">
                      <div class="flex justify-between text-xs text-base-content/60">
                        <span>{window.name}</span>
                        <span class="tabular-nums">{windowSummary(window)}</span>
                      </div>
                      {#if percent !== null}
                        <progress
                          class="progress progress-primary h-1.5 w-full"
                          class:progress-error={window.exhausted}
                          value={percent}
                          max="100"
                          aria-label={`${window.name} quota usage`}
                        ></progress>
                      {/if}
                    </div>
                  {/each}

                  {#if account.breaker}
                    <div class="text-xs text-base-content/60">
                      Circuit breaker: <strong>{account.breaker.state}</strong>
                      ({account.breaker.failures} failures / {account.breaker.successes} successes in window)
                    </div>
                  {/if}

                  {#if account.policy_status !== 'metered'}
                    <div class="text-xs text-base-content/60">
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

    <!-- Hosts: where the turns ran. Each row narrows the whole page to that
         host; the energy line says how much of it the host's own device
         telemetry measured rather than a configured draw estimated. -->
    {#if (report.hosts ?? []).length > 0}
      <div class="card rounded-box bg-base-200" data-testid="spend-hosts">
        <div class="card-body gap-0 p-5">
          <h3 class="mb-3 text-[0.9375rem] font-semibold">By host</h3>
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {#each report.hosts ?? [] as host (hostScopeValue(host))}
              {@const narrowed = scope.host === hostScopeValue(host)}
              <button
                class="min-w-0 rounded-field border border-base-300/80 bg-base-100 px-4 py-3 text-left transition-colors enabled:hover:border-primary/60"
                disabled={narrowed}
                aria-label={narrowed ? `${hostLabel(host)} (shown)` : `Show only ${hostLabel(host)}`}
                onclick={() => narrowToHost(host)}
                data-testid="spend-host"
              >
                <div class="flex items-center gap-2">
                  <Server class="h-3.5 w-3.5 shrink-0 text-base-content/60" aria-hidden="true" />
                  <span class="truncate text-xs text-base-content/80" class:font-mono={host.unattributed !== true} class:italic={host.unattributed === true} title={hostLabel(host)}>
                    {hostLabel(host)}
                  </span>
                </div>
                <div class="mt-1 text-xl font-semibold tabular-nums">{formatTokens(host.total_tokens)} <span class="text-xs font-normal text-base-content/60">tokens</span></div>
                <div class="mt-1 text-xs tabular-nums text-base-content/60" title={moneyNote(host)} data-testid="spend-host-money">
                  equivalent {formatEquivalentFloor(host, report.currency)} · billed {formatBilledFloor(host, report.currency)}
                </div>
                {#if (host.electricity_entries ?? 0) > 0}
                  <div class="mt-0.5 text-xs tabular-nums text-base-content/60" data-testid="spend-host-energy">
                    {formatEnergy(host)} · {measuredShare(host)}
                  </div>
                {/if}
              </button>
            {/each}
          </div>
        </div>
      </div>
    {/if}

    <!-- Agents: which tenant Agent each turn's session ran as. Each row
         narrows the whole page to that Agent and shows its own limits. -->
    {#if (report.agents ?? []).length > 0}
      <div class="card rounded-box bg-base-200" data-testid="spend-agents">
        <div class="card-body gap-0 p-5">
          <h3 class="mb-3 text-[0.9375rem] font-semibold">By Agent</h3>
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {#each report.agents ?? [] as agent (agentScopeValue(agent))}
              {@const narrowed = scope.agent === agentScopeValue(agent)}
              <button
                class="min-w-0 rounded-field border border-base-300/80 bg-base-100 px-4 py-3 text-left transition-colors enabled:hover:border-primary/60"
                disabled={narrowed}
                aria-label={narrowed ? `${agentLabel(agent)} (shown)` : `Show only ${agentLabel(agent)}`}
                onclick={() => narrowToAgent(agent)}
                data-testid="spend-agent"
              >
                <div class="flex items-center gap-2">
                  <Bot class="h-3.5 w-3.5 shrink-0 text-base-content/60" aria-hidden="true" />
                  <span class="truncate text-xs text-base-content/80" class:italic={agent.unattributed === true} title={agent.agent_id ?? agentLabel(agent)}>
                    {agentLabel(agent)}
                  </span>
                </div>
                <div class="mt-1 text-xl font-semibold tabular-nums">{formatTokens(agent.total_tokens)} <span class="text-xs font-normal text-base-content/60">tokens</span></div>
                <div class="mt-1 text-xs tabular-nums text-base-content/60" title={moneyNote(agent)} data-testid="spend-agent-money">
                  equivalent {formatEquivalentFloor(agent, report.currency)} · billed {formatBilledFloor(agent, report.currency)}
                </div>
              </button>
            {/each}
          </div>
        </div>
      </div>
    {/if}

    <!-- Models -->
    {#if (report.models ?? []).length > 0}
      <div class="card rounded-box bg-base-200">
        <div class="card-body gap-0 p-5">
          <h3 class="mb-3 text-[0.9375rem] font-semibold">By model</h3>
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {#each report.models ?? [] as model (`${model.engine}/${model.model}`)}
              <div class="min-w-0 rounded-field border border-base-300/80 bg-base-100 px-4 py-3">
                <div class="flex items-baseline justify-between gap-2">
                  <span class="truncate font-mono text-xs text-base-content/80" title={model.model}>{model.model}</span>
                  <span class="shrink-0 text-xs text-base-content/60">{model.engine || UNMEASURED}</span>
                </div>
                <div class="mt-1 text-xl font-semibold tabular-nums">{formatTokens(model.total_tokens)} <span class="text-xs font-normal text-base-content/60">tokens</span></div>
                <div class="mt-1 text-xs tabular-nums text-base-content/60" title={moneyNote(model)} data-testid="spend-model-money">
                  equivalent {formatEquivalentFloor(model, report.currency)}
                  · billed {formatBilledFloor(model, report.currency)}
                  {#if (model.electricity_entries ?? 0) > 0}· {formatEnergy(model)}{/if}
                </div>
              </div>
            {/each}
          </div>
        </div>
      </div>
    {/if}
  {/if}

  <!-- The tenant's electricity rate and engine draws, which local turns are
       costed from. Shown whatever the report's state: a tenant with no spend
       yet still sets them. -->
  <CostSettingsPanel tenantID={orchestratorStore.activeTenantID} />
</div>
