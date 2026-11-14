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
  PolicyTesterView is the top-level Security > Policy Tester route component.

  It runs read-only checks against a tenant's policies through `client.iam`;
  none of them changes a policy:
    - Check: testPolicy decides a subject's action on a resource.
    - Simulate: simulatePolicy evaluates a policy document that is not stored
      (loaded with getPolicy or pasted), optionally with a trace;
      validatePolicy checks the document.
    - Effective permissions: getEffectivePermissions lists what a subject may
      do on each resource pattern.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import type {
    IAMSubject,
    IAMCheckRequest,
    IAMCheckResponse,
    IAMPolicyDocument,
    IAMPolicySimulationResult,
    IAMPolicyValidationResult,
    IAMEffectivePermissionsResult,
  } from '@quicbaas/sdk';

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let tenantIDs: string[] = $state([]);
  let tenantsLoading = $state(false);
  let tenantsError: string | null = $state(null);
  let selectedTenantID = $state('');

  // Subject (shared by every panel)
  let subjectID = $state('');
  let subjectTenantID = $state('');
  let subjectRoles = $state('');
  let subjectAuthenticated = $state(true);
  let subjectMFA = $state(false);

  // Check (action and resource are shared with Simulate)
  let action = $state('');
  let resource = $state('');
  let checking = $state(false);
  let checkError: string | null = $state(null);
  let checkResult: IAMCheckResponse | null = $state(null);

  // Simulate / validate
  let policyName = $state('');
  let policyText = $state('');
  let includeTrace = $state(true);
  let loadingPolicy = $state(false);
  let simulating = $state(false);
  let validating = $state(false);
  let jsonError: string | null = $state(null);
  let simulateError: string | null = $state(null);
  let simulateResult: IAMPolicySimulationResult | null = $state(null);
  let validateResult: IAMPolicyValidationResult | null = $state(null);

  // Effective permissions
  let resourcePatterns = $state('');
  let effectiveActions = $state('');
  let explaining = $state(false);
  let effectiveError: string | null = $state(null);
  let effectiveResult: IAMEffectivePermissionsResult | null = $state(null);

  // ---------------------------------------------------------------------------
  // Tenants
  // ---------------------------------------------------------------------------

  async function loadTenants(): Promise<void> {
    tenantsLoading = true;
    tenantsError = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listTenants();
      const seen = new Set<string>();
      tenantIDs = [];
      for (const t of resp.tenants) {
        if (seen.has(t.id)) continue;
        seen.add(t.id);
        tenantIDs.push(t.id);
      }
      if (selectedTenantID === '' && tenantIDs.length > 0) {
        selectTenant(tenantIDs[0] ?? '');
      }
    } catch (e) {
      tenantsError = e instanceof Error ? e.message : 'Failed to load tenants';
    } finally {
      tenantsLoading = false;
    }
  }

  function selectTenant(id: string): void {
    selectedTenantID = id;
    subjectTenantID = id;
  }

  $effect(() => {
    void loadTenants();
  });

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function splitList(text: string, sep: RegExp): string[] {
    return text.split(sep).map((s) => s.trim()).filter((s) => s !== '');
  }

  function buildSubject(): IAMSubject {
    const roles = splitList(subjectRoles, /,/);
    const subject: IAMSubject = {
      id: subjectID.trim(),
      tenant_id: subjectTenantID.trim() || selectedTenantID,
      authenticated: subjectAuthenticated,
      mfa_verified: subjectMFA,
    };
    if (roles.length > 0) subject.roles = roles;
    return subject;
  }

  function buildCheck(): IAMCheckRequest {
    return { subject: buildSubject(), action: action.trim(), resource: resource.trim() };
  }

  function isPolicyDocument(v: unknown): v is IAMPolicyDocument {
    return typeof v === 'object' && v !== null && !Array.isArray(v) && 'metadata' in v && 'spec' in v;
  }

  // parseDocument parses the textarea; on failure it sets jsonError and
  // returns null so no call is made.
  function parseDocument(): IAMPolicyDocument | null {
    jsonError = null;
    let parsed: unknown;
    try {
      parsed = JSON.parse(policyText);
    } catch (e) {
      jsonError = `Invalid JSON: ${e instanceof Error ? e.message : String(e)}`;
      return null;
    }
    if (!isPolicyDocument(parsed)) {
      jsonError = 'The policy document must be a JSON object with metadata and spec';
      return null;
    }
    return parsed;
  }

  function errorText(e: unknown, fallback: string): string {
    return e instanceof Error ? e.message : fallback;
  }

  const subjectReady = $derived(subjectID.trim() !== '' && selectedTenantID !== '');
  const checkReady = $derived(subjectReady && action.trim() !== '' && resource.trim() !== '');

  // ---------------------------------------------------------------------------
  // Check
  // ---------------------------------------------------------------------------

  async function runCheck(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!checkReady) return;
    checking = true;
    checkError = null;
    checkResult = null;
    try {
      const client = await getSDKClient();
      checkResult = await client.iam.testPolicy(selectedTenantID, buildCheck());
    } catch (e) {
      checkError = errorText(e, 'The policy check failed');
    } finally {
      checking = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Simulate / validate
  // ---------------------------------------------------------------------------

  async function loadPolicy(): Promise<void> {
    const name = policyName.trim();
    if (!name || selectedTenantID === '') return;
    loadingPolicy = true;
    simulateError = null;
    jsonError = null;
    try {
      const client = await getSDKClient();
      const doc = await client.iam.getPolicy(selectedTenantID, name);
      policyText = JSON.stringify(doc, null, 2);
    } catch (e) {
      simulateError = errorText(e, 'Loading the policy failed');
    } finally {
      loadingPolicy = false;
    }
  }

  async function runSimulate(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    simulateResult = null;
    validateResult = null;
    simulateError = null;
    const doc = parseDocument();
    if (doc === null || !checkReady) return;
    simulating = true;
    try {
      const client = await getSDKClient();
      simulateResult = await client.iam.simulatePolicy(selectedTenantID, {
        policy: doc,
        check_request: buildCheck(),
        include_trace: includeTrace,
      });
    } catch (e) {
      simulateError = errorText(e, 'The simulation failed');
    } finally {
      simulating = false;
    }
  }

  async function runValidate(): Promise<void> {
    simulateResult = null;
    validateResult = null;
    simulateError = null;
    const doc = parseDocument();
    if (doc === null || selectedTenantID === '') return;
    validating = true;
    try {
      const client = await getSDKClient();
      const name = doc.metadata.name || policyName.trim();
      validateResult = await client.iam.validatePolicy(
        selectedTenantID,
        { name, description: doc.metadata.description, spec_yaml: JSON.stringify(doc) },
        name || undefined,
      );
    } catch (e) {
      simulateError = errorText(e, 'The validation failed');
    } finally {
      validating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Effective permissions
  // ---------------------------------------------------------------------------

  async function runEffective(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const patterns = splitList(resourcePatterns, /\n/);
    if (!subjectReady || patterns.length === 0) return;
    const actions = splitList(effectiveActions, /[,\n]/);
    explaining = true;
    effectiveError = null;
    effectiveResult = null;
    try {
      const client = await getSDKClient();
      effectiveResult = await client.iam.getEffectivePermissions(selectedTenantID, {
        subject: buildSubject(),
        resource_patterns: patterns,
        ...(actions.length > 0 ? { actions } : {}),
      });
    } catch (e) {
      effectiveError = errorText(e, 'Reading the effective permissions failed');
    } finally {
      explaining = false;
    }
  }

  function joinOrDash(list: string[] | undefined): string {
    return list && list.length > 0 ? list.join(', ') : '—';
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Policy Tester" subtitle="Check, simulate and explain a tenant's policies without changing them" />

  <!-- Tenant selector -->
  <section class="space-y-2" aria-label="Tenant">
    <label class="flex items-center gap-2 text-sm">
      <span>Tenant</span>
      <select
        class="select select-bordered select-sm"
        aria-label="Tenant"
        value={selectedTenantID}
        disabled={tenantsLoading}
        onchange={(e) => { selectTenant(e.currentTarget.value); }}
      >
        {#if tenantIDs.length === 0}
          <option value="">{tenantsLoading ? 'Loading…' : 'No tenants'}</option>
        {/if}
        {#each tenantIDs as id (id)}
          <option value={id}>{id}</option>
        {/each}
      </select>
    </label>
    {#if tenantsError}
      <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
        <span>{tenantsError}</span>
        <button class="btn btn-xs btn-ghost" onclick={() => { void loadTenants(); }}>Retry</button>
      </div>
    {/if}
  </section>

  <!-- Subject -->
  <section class="card bg-base-200 p-4 space-y-2" aria-label="Subject">
    <h2 class="text-lg font-semibold">Subject</h2>
    <div class="flex flex-wrap gap-2 items-center">
      <input class="input input-bordered input-sm" placeholder="Subject ID" aria-label="Subject ID" bind:value={subjectID} />
      <input class="input input-bordered input-sm" placeholder="Tenant ID" aria-label="Subject tenant ID" bind:value={subjectTenantID} />
      <input class="input input-bordered input-sm" placeholder="Roles (comma-separated)" aria-label="Roles" bind:value={subjectRoles} />
      <label class="flex items-center gap-1 text-sm">
        <input type="checkbox" class="checkbox checkbox-sm" bind:checked={subjectAuthenticated} />
        Authenticated
      </label>
      <label class="flex items-center gap-1 text-sm">
        <input type="checkbox" class="checkbox checkbox-sm" bind:checked={subjectMFA} />
        MFA verified
      </label>
    </div>
    <div class="flex flex-wrap gap-2">
      <input class="input input-bordered input-sm" placeholder="Action" aria-label="Action" bind:value={action} />
      <input class="input input-bordered input-sm" placeholder="Resource" aria-label="Resource" bind:value={resource} />
    </div>
  </section>

  <!-- Check -->
  <section class="space-y-3" aria-label="Check">
    <h2 class="text-lg font-semibold">Check</h2>
    <form aria-label="Check policy" onsubmit={runCheck}>
      <button type="submit" class="btn btn-primary btn-sm" disabled={checking || !checkReady}>
        {checking ? 'Checking…' : 'Check'}
      </button>
    </form>
    {#if checkError}
      <div class="alert alert-error text-sm" role="alert"><span>{checkError}</span></div>
    {/if}
    {#if checkResult}
      <div class="card bg-base-200 p-4 text-sm space-y-1" aria-label="Check result">
        <div>
          <span class={`badge ${checkResult.allowed ? 'badge-success' : 'badge-error'}`}>
            {checkResult.allowed ? 'Allowed' : 'Denied'}
          </span>
        </div>
        <div>Matched policy: {checkResult.matched_policy || '—'}</div>
        <div>Matched rule: {checkResult.matched_rule || '—'}</div>
        <div>Reason: {checkResult.reason || '—'}</div>
      </div>
    {/if}
  </section>

  <!-- Simulate -->
  <section class="space-y-3" aria-label="Simulate">
    <h2 class="text-lg font-semibold">Simulate</h2>
    <form class="card bg-base-200 p-4 space-y-2" aria-label="Simulate policy" onsubmit={runSimulate}>
      <div class="flex flex-wrap gap-2 items-center">
        <input class="input input-bordered input-sm" placeholder="Policy name" aria-label="Policy name" bind:value={policyName} />
        <button
          type="button"
          class="btn btn-sm"
          disabled={loadingPolicy || !policyName.trim() || selectedTenantID === ''}
          onclick={() => { void loadPolicy(); }}
        >
          {loadingPolicy ? 'Loading…' : 'Load'}
        </button>
        <label class="flex items-center gap-1 text-sm">
          <input type="checkbox" class="checkbox checkbox-sm" bind:checked={includeTrace} />
          Include trace
        </label>
      </div>
      <textarea
        class="textarea textarea-bordered w-full font-mono text-xs"
        rows="10"
        placeholder={'{"apiVersion": "...", "kind": "Policy", "metadata": {...}, "spec": {...}}'}
        aria-label="Policy document"
        bind:value={policyText}
      ></textarea>
      {#if jsonError}
        <p class="text-error text-sm" role="alert">{jsonError}</p>
      {/if}
      <div class="flex gap-2">
        <button type="submit" class="btn btn-primary btn-sm" disabled={simulating || !checkReady || !policyText.trim()}>
          {simulating ? 'Simulating…' : 'Simulate'}
        </button>
        <button
          type="button"
          class="btn btn-sm"
          disabled={validating || !policyText.trim() || selectedTenantID === ''}
          onclick={() => { void runValidate(); }}
        >
          {validating ? 'Validating…' : 'Validate'}
        </button>
      </div>
    </form>
    {#if simulateError}
      <div class="alert alert-error text-sm" role="alert"><span>{simulateError}</span></div>
    {/if}
    {#if validateResult}
      <div class="card bg-base-200 p-4 text-sm space-y-1" aria-label="Validation result">
        {#if validateResult.valid}
          <span class="badge badge-success">Valid</span>
        {:else}
          <span class="badge badge-error">Invalid</span>
          <ul class="list-disc pl-5">
            {#each validateResult.errors ?? [] as err, i (i)}
              <li>{err}</li>
            {/each}
          </ul>
        {/if}
      </div>
    {/if}
    {#if simulateResult}
      <div class="card bg-base-200 p-4 text-sm space-y-2" aria-label="Simulation result">
        <div>
          <span class={`badge ${simulateResult.allowed ? 'badge-success' : 'badge-error'}`}>
            {simulateResult.allowed ? 'Allowed' : 'Denied'}
          </span>
        </div>
        <div>Matched policy: {simulateResult.matched_policy || '—'}</div>
        <div>Matched rule: {simulateResult.matched_rule || '—'}</div>
        <div>Reason: {simulateResult.reason || '—'}</div>
        {#if simulateResult.evaluation_steps && simulateResult.evaluation_steps.length > 0}
          <div class="overflow-x-auto rounded-box">
            <table class="table table-sm" aria-label="Evaluation steps">
              <thead>
                <tr><th>Step</th><th>Policy</th><th>Rule</th><th>Action</th><th>Result</th><th>Details</th></tr>
              </thead>
              <tbody>
                {#each simulateResult.evaluation_steps as step (step.step_number)}
                  <tr>
                    <td>{step.step_number}</td>
                    <td>{step.policy_name}</td>
                    <td>{step.rule_name || '—'}</td>
                    <td>{step.action}</td>
                    <td>{step.result ? 'match' : 'no match'}</td>
                    <td>{step.details || '—'}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    {/if}
  </section>

  <!-- Effective permissions -->
  <section class="space-y-3" aria-label="Effective permissions">
    <h2 class="text-lg font-semibold">Effective Permissions</h2>
    <form class="card bg-base-200 p-4 space-y-2" aria-label="Effective permissions query" onsubmit={runEffective}>
      <textarea
        class="textarea textarea-bordered w-full font-mono text-xs"
        rows="3"
        placeholder="Resource patterns, one per line"
        aria-label="Resource patterns"
        bind:value={resourcePatterns}
      ></textarea>
      <input
        class="input input-bordered input-sm w-full"
        placeholder="Actions (optional, comma-separated)"
        aria-label="Actions"
        bind:value={effectiveActions}
      />
      <button type="submit" class="btn btn-primary btn-sm" disabled={explaining || !subjectReady || !resourcePatterns.trim()}>
        {explaining ? 'Explaining…' : 'Explain'}
      </button>
    </form>
    {#if effectiveError}
      <div class="alert alert-error text-sm" role="alert"><span>{effectiveError}</span></div>
    {/if}
    {#if effectiveResult}
      <div class="space-y-2" aria-label="Effective permissions result">
        <p class="text-sm">Policies evaluated: {effectiveResult.total_policies_evaluated}</p>
        <div class="overflow-x-auto rounded-box">
          <table class="table table-sm" aria-label="Permission sets">
            <thead>
              <tr><th>Resource pattern</th><th>Allowed</th><th>Denied</th></tr>
            </thead>
            <tbody>
              {#each Object.entries(effectiveResult.permissions ?? {}) as [pattern, set] (pattern)}
                <tr>
                  <td class="font-mono text-xs">{pattern}</td>
                  <td class="text-xs">{joinOrDash(set.allowed)}</td>
                  <td class="text-xs">{joinOrDash(set.denied)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </div>
    {/if}
  </section>
</div>
