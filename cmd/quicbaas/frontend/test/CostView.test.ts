/**
 * CostView.test.ts
 *
 * go-quicbaas#794: Cost Tracking must report what was actually SPENT, read
 * from the consumption ledger, and must never render a figure the system does
 * not have as a zero.
 *
 * Verifies:
 * 1. The page reads the ledger at tenant scope and shows the tokens.
 * 2. No iteration gauge appears anywhere on it.
 * 3. An unpriced report shows a dash for cost and says why.
 * 4. An account with no cap reads "No spend policy"; one the server could not
 *    read reads "unknown".
 * 5. A drill step narrows the scope along the chain, including into the
 *    unattributed bucket.
 * 6. A deployment that records no spend says so instead of showing zero.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { tick } from 'svelte';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { GetSpendReportResponse } from '@quicbaas/sdk';

const { storeMock, getSpendReport, getCostSettings, getTenantSpendPolicy, setTenantSpendPolicy, getAgent, updateAgent } = vi.hoisted(() => ({
  storeMock: { activeTenantID: 'acme' },
  getSpendReport: vi.fn(),
  getCostSettings: vi.fn(),
  getTenantSpendPolicy: vi.fn(),
  setTenantSpendPolicy: vi.fn(),
  getAgent: vi.fn(),
  updateAgent: vi.fn(),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: storeMock,
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: { getSpendReport, getCostSettings, getTenantSpendPolicy, setTenantSpendPolicy },
    agents: { getAgent, updateAgent },
  }),
}));

import CostView from '../src/views/orchestrator/CostView.svelte';

// The tenant report the burn-in deployment actually produces: real tokens, an
// unpriced cost, one account nobody capped, and a project subtree plus a
// bucket of spend that belongs to no project.
const tenantReport: GetSpendReportResponse = {
  level: 'tenant',
  tenant_id: 'acme',
  entries: 3,
  input_tokens: 120,
  output_tokens: 80,
  total_tokens: 200,
  currency: 'USD',
  priced_entries: 0,
  unpriced_entries: 3,
  billed_entries: 0,
  unbilled_entries: 3,
  successes: 2,
  failures: 1,
  child_level: 'project',
  children: [
    { id: 'proj-1', level: 'project', total_tokens: 150 },
    { level: 'project', unattributed: true, total_tokens: 50 },
  ],
  accounts: [
    { account_id: 'acct-1', display_name: 'alice-work', total_tokens: 200, policy_status: 'none' },
  ],
  models: [{ engine: 'acp', model: 'claude-opus-5', total_tokens: 200 }],
};

const projectReport: GetSpendReportResponse = {
  ...tenantReport,
  level: 'project',
  project_id: 'proj-1',
  total_tokens: 150,
  child_level: 'task',
  children: [{ id: 'task-1', level: 'task', total_tokens: 150 }],
};

beforeEach(() => {
  getTenantSpendPolicy.mockReset();
  setTenantSpendPolicy.mockReset();
  getAgent.mockReset();
  updateAgent.mockReset();
  getCostSettings.mockReset();
  getCostSettings.mockResolvedValue({
    settings: { tenant_id: 'acme' },
    electricity: { price_per_kwh: 0.12, source: 'default' },
  });
  getSpendReport.mockReset();
  getSpendReport.mockResolvedValue(tenantReport);
  // One test moves the operator to another tenant; the rest start on acme.
  storeMock.activeTenantID = 'acme';
});

describe('CostView', () => {
  it('reads the ledger at tenant scope and shows the tokens spent', async () => {
    render(CostView);

    await waitFor(() => expect(getSpendReport).toHaveBeenCalled());
    expect(getSpendReport.mock.calls[0]![0]).toMatchObject({ tenant_id: 'acme' });
    // Total / input / output all come from the ledger roll-up. `200` appears
    // more than once (the total and the model breakdown), so assert on the
    // count rather than on uniqueness.
    expect((await screen.findAllByText('200')).length).toBeGreaterThan(0);
    expect(screen.getByText('120')).toBeTruthy();
    expect(screen.getByText('80')).toBeTruthy();
    expect(screen.getByText('2 ok · 1 failed')).toBeTruthy();
  });

  it('shows no iteration gauge at all', async () => {
    render(CostView);
    await waitFor(() => expect(getSpendReport).toHaveBeenCalled());
    await screen.findAllByText('200');

    // The defect: the page used to read "0.0% Iterations" and "N iterations
    // across M jobs" — the workflow runner's fields, which are not a cost.
    expect(screen.queryByText(/Iterations/i)).toBeNull();
    expect(screen.queryByText(/iterations across/i)).toBeNull();
  });

  it('renders an unpriced cost as a dash and says a price sheet is missing', async () => {
    render(CostView);
    const cost = await screen.findByTestId('scope-cost');
    // Not "0.00". Nothing prices a turn, so the figure is UNMEASURED.
    expect(cost.textContent?.trim()).toBe('—');
    expect(screen.getByText(/no price for these models/i)).toBeTruthy();
  });

  // go-quicbaas#1058: the Cost page showed no dollars.
  it('shows both money figures: the list-price equivalent and what was billed', async () => {
    getSpendReport.mockResolvedValue({
      ...tenantReport,
      equivalent_micros: 42_500_000,
      priced_entries: 3,
      unpriced_entries: 0,
      billed_micros: 0,
      billed_entries: 3,
      unbilled_entries: 0,
      accounts: [
        {
          account_id: 'acct-1', display_name: 'alice-work', total_tokens: 200, policy_status: 'none',
          billing: { mode: 'subscription', monthly_fee_micros: 200_000_000 },
          equivalent_micros: 42_500_000, priced_entries: 3, billed_micros: 0, billed_entries: 3,
        },
      ],
    });
    render(CostView);
    expect((await screen.findByTestId('scope-cost')).textContent?.trim()).toBe('$42.50');
    // A subscription's turns bill zero, and zero is a measurement here.
    expect(screen.getByTestId('scope-billed').textContent?.trim()).toBe('$0.00');
    expect(screen.getByText('Subscription · $200.00/mo')).toBeTruthy();
  });

  it('marks every breakdown row whose money covers only some of its turns', async () => {
    // Mixed coverage: priced and billed turns alongside unpriced and unbilled
    // ones. A row showing the bare figure would read as the row's whole cost.
    const mixed = {
      equivalent_micros: 1_200_000, priced_entries: 2, unpriced_entries: 1,
      billed_micros: 500_000, billed_entries: 1, unbilled_entries: 2,
    };
    getSpendReport.mockResolvedValue({
      ...tenantReport,
      children: [{ id: 'proj-1', level: 'project', total_tokens: 150, ...mixed }],
      accounts: [{ account_id: 'acct-1', total_tokens: 200, policy_status: 'none', ...mixed }],
      models: [{ engine: 'acp', model: 'claude-opus-5', total_tokens: 200, ...mixed }],
    });
    render(CostView);

    const rows = [
      await screen.findByTestId('spend-child-money'),
      screen.getByTestId('spend-account-money'),
      screen.getByTestId('spend-model-money'),
    ];
    for (const row of rows) {
      const text = row.textContent?.replace(/\s+/g, ' ') ?? '';
      expect(text).toContain('equivalent ≥$1.20');
      expect(text).toContain('billed ≥$0.50');
      expect(row.getAttribute('title')).toBe(
        'equivalent: at least — 1 turn unpriced · billed: at least — 2 turns with unknown billing',
      );
    }
  });

  it('shows the energy local inference drew, and a dash when none was costed from electricity', async () => {
    getSpendReport.mockResolvedValue({
      ...tenantReport,
      energy_mwh: 12_000,
      electricity_entries: 2,
      models: [{ engine: 'openvino', model: 'qwen3-8b', total_tokens: 200, energy_mwh: 12_000, electricity_entries: 2 }],
    });
    render(CostView);
    expect((await screen.findByTestId('scope-energy')).textContent?.trim()).toBe('12 Wh');
    expect(screen.getByText('2 local turns costed from electricity · all estimated')).toBeTruthy();
    expect(screen.getByTestId('spend-model-money').textContent).toContain('12 Wh');
  });

  it('shows the electricity rate in force for the tenant', async () => {
    render(CostView);
    expect((await screen.findByTestId('cost-settings-rate')).textContent).toContain('$0.12/kWh');
    expect(getCostSettings).toHaveBeenCalledWith({ tenant_id: 'acme' });
  });

  it('says "No spend policy" for an account nobody capped', async () => {
    render(CostView);
    expect(await screen.findByText('No spend policy')).toBeTruthy();
    expect(
      screen.getByText(/No spend cap is configured for this account/i),
    ).toBeTruthy();
  });

  it('says the cap is unknown when the server could not read the account', async () => {
    getSpendReport.mockResolvedValue({
      ...tenantReport,
      accounts: [{ account_id: 'acct-9', total_tokens: 200, policy_status: 'unknown' }],
    });
    render(CostView);
    expect(await screen.findByText('Spend policy unknown')).toBeTruthy();
    expect(screen.getByText(/could not read the account/i)).toBeTruthy();
  });

  it('renders a metered account with its window instead of a budget gauge', async () => {
    getSpendReport.mockResolvedValue({
      ...tenantReport,
      accounts: [
        {
          account_id: 'acct-1',
          total_tokens: 200,
          policy_status: 'metered',
          windows: [
            {
              name: 'five-hour',
              size: 5 * 3_600 * 1e9,
              max_tokens: 1000,
              used_tokens: 200,
              remaining_tokens: 800,
              exhausted: false,
            },
          ],
        },
      ],
    });
    render(CostView);
    expect(await screen.findByText('Metered')).toBeTruthy();
    expect(screen.getByText('200 / 1,000 tokens per 5h')).toBeTruthy();
  });

  it('labels the bucket of spend that belongs to no project', async () => {
    render(CostView);
    // Never "-": that would read as a project whose id is a dash.
    expect(await screen.findByText('No project')).toBeTruthy();
  });

  it('drills into a project and narrows the scope', async () => {
    render(CostView);
    const row = await screen.findByText('proj-1');
    getSpendReport.mockResolvedValue(projectReport);

    await fireEvent.click(row);

    await waitFor(() => {
      const last = getSpendReport.mock.calls[getSpendReport.mock.calls.length - 1]![0];
      expect(last).toMatchObject({ tenant_id: 'acme', project_id: 'proj-1' });
    });
    expect(await screen.findByText('task-1')).toBeTruthy();
  });

  // go-quicbaas#1058: the By Project card listed bare project ids.
  it('names project rows and the breadcrumb, and drills by the id', async () => {
    getSpendReport.mockResolvedValue({
      ...tenantReport,
      children: [{ id: 'proj-1', level: 'project', name: 'go-truststrap', total_tokens: 150 }],
    });
    render(CostView);
    const row = await screen.findByText('go-truststrap');
    expect(screen.queryByText('proj-1')).toBeNull();
    getSpendReport.mockResolvedValue({ ...projectReport, project_name: 'go-truststrap' });

    await fireEvent.click(row);

    await waitFor(() => {
      const last = getSpendReport.mock.calls[getSpendReport.mock.calls.length - 1]![0];
      expect(last).toMatchObject({ tenant_id: 'acme', project_id: 'proj-1' });
    });
    // The breadcrumb names the project it opened.
    expect(await screen.findByRole('button', { name: 'go-truststrap' })).toBeTruthy();
  });

  it('drills into the unattributed bucket using the ledger sentinel', async () => {
    render(CostView);
    const row = await screen.findByText('No project');

    await fireEvent.click(row);

    await waitFor(() => {
      const last = getSpendReport.mock.calls[getSpendReport.mock.calls.length - 1]![0];
      expect(last).toMatchObject({ tenant_id: 'acme', project_id: '-' });
    });
  });

  // go-quicbaas#886: a session report's children are rounds. The row rendered
  // enabled and a click issued no request at all.
  it('drills from a session into a round and names it in the breadcrumb', async () => {
    const report = (
      level: GetSpendReportResponse['level'],
      child_level: GetSpendReportResponse['child_level'],
      children: GetSpendReportResponse['children'],
    ): GetSpendReportResponse => ({ ...tenantReport, level, child_level, children });
    getSpendReport.mockReset();
    getSpendReport
      .mockResolvedValueOnce(report('tenant', 'project', [{ id: 'proj-1', level: 'project', total_tokens: 9 }]))
      .mockResolvedValueOnce(report('project', 'task', [{ id: 'task-1', level: 'task', total_tokens: 9 }]))
      .mockResolvedValueOnce(report('task', 'job', [{ id: 'job-1', level: 'job', total_tokens: 9 }]))
      .mockResolvedValueOnce(report('job', 'session', [{ id: 'sess-1', level: 'session', total_tokens: 9 }]))
      .mockResolvedValueOnce(
        report('session', 'round', [
          { id: '0', level: 'round', total_tokens: 4 },
          { id: '2', level: 'round', total_tokens: 5 },
        ]),
      )
      .mockResolvedValueOnce({ ...report('round', undefined, []), round: 2 });

    render(CostView);
    for (const id of ['proj-1', 'task-1', 'job-1', 'sess-1']) {
      await fireEvent.click(await screen.findByText(id));
    }
    await fireEvent.click(await screen.findByText('2'));

    await waitFor(() => expect(getSpendReport).toHaveBeenCalledTimes(6));
    expect(getSpendReport.mock.calls[5]![0]).toEqual({
      tenant_id: 'acme',
      project_id: 'proj-1',
      task_id: 'task-1',
      job_id: 'job-1',
      session_id: 'sess-1',
      round: 2,
      round_set: true,
    });
    expect(await screen.findByRole('button', { name: 'Round 2' })).toBeTruthy();
    // No round is sent at any shallower scope: round_set absent means every round.
    for (const call of getSpendReport.mock.calls.slice(0, 5)) {
      expect(call[0]).not.toHaveProperty('round_set', true);
    }

    // Climbing back to the session clears the round.
    getSpendReport.mockResolvedValueOnce(report('session', 'round', []));
    await fireEvent.click(screen.getByRole('button', { name: 'sess-1' }));
    await waitFor(() => expect(getSpendReport).toHaveBeenCalledTimes(7));
    expect(getSpendReport.mock.calls[6]![0]).toMatchObject({ session_id: 'sess-1', round_set: false });
    expect(screen.queryByRole('button', { name: 'Round 2' })).toBeNull();
  });

  it('says the deployment records no spend rather than showing zero', async () => {
    getSpendReport.mockRejectedValue({ status: 503, message: 'ledger not available' });
    render(CostView);

    expect(
      await screen.findByText(/not recording provider consumption/i),
    ).toBeTruthy();
    // Crucially NOT a report full of zeroes.
    expect(screen.queryByTestId('scope-cost')).toBeNull();
  });

  it('names the permission when the read is forbidden', async () => {
    getSpendReport.mockRejectedValue({ status: 403, message: 'forbidden' });
    render(CostView);
    expect(await screen.findByText(/orchestrator:status/)).toBeTruthy();
  });

  // ---------------------------------------------------------------------------
  // A report on screen must belong to the scope the breadcrumb names
  // ---------------------------------------------------------------------------

  it('stops showing the parent scope the moment a drill step is taken', async () => {
    // The drill read never settles, so the page stays in exactly the state it
    // enters on the click -- which is what an operator sees on any read slower
    // than their eye.
    getSpendReport.mockReset();
    getSpendReport.mockResolvedValueOnce(tenantReport);
    getSpendReport.mockReturnValueOnce(new Promise(() => {}));

    render(CostView);
    const row = await screen.findByText('proj-1');
    expect(await screen.findByTestId('scope-cost')).toBeTruthy();

    await fireEvent.click(row);

    // The breadcrumb already names proj-1, so the tenant's own totals must not
    // still be sitting under it: that prints the parent's spend as the child's.
    await waitFor(() => {
      expect(screen.queryByTestId('scope-cost')).toBeNull();
    });
    expect(screen.getByText(/Loading spend/i)).toBeTruthy();
  });

  // go-quicbaas#1058, ruling F4: a subscription's fee is pro-rated over the
  // report's range and shown beside the per-turn billed figure.
  it('shows the subscription fees pro-rated over the range, scope and account', async () => {
    getSpendReport.mockResolvedValue({
      ...tenantReport,
      subscription_fee_micros: 46_000_000,
      accounts: [
        {
          account_id: 'acct-1', display_name: 'alice-work', total_tokens: 200, policy_status: 'none',
          billing: { mode: 'subscription', monthly_fee_micros: 200_000_000 },
          subscription_fee_micros: 46_000_000,
        },
      ],
    });
    render(CostView);
    expect((await screen.findByTestId('scope-subscription-fees')).textContent).toContain('$46.00');
    expect(screen.getByTestId('spend-account-fee').textContent).toContain('$46.00');
  });

  it('shows no fee line when the report carries no fee share', async () => {
    render(CostView);
    await screen.findByTestId('scope-cost');
    expect(screen.queryByTestId('scope-subscription-fees')).toBeNull();
    expect(screen.queryByTestId('spend-account-fee')).toBeNull();
  });

  it('reads the chosen range and reloads when it changes', async () => {
    render(CostView);
    await waitFor(() => expect(getSpendReport).toHaveBeenCalledTimes(1));
    expect(getSpendReport.mock.calls[0]![0].from).toBeUndefined();

    const before = Date.now();
    await fireEvent.change(screen.getByTestId('spend-range'), { target: { value: '7d' } });
    await waitFor(() => expect(getSpendReport).toHaveBeenCalledTimes(2));
    const from = Date.parse(getSpendReport.mock.calls[1]![0].from as string);
    const week = 7 * 24 * 60 * 60 * 1000;
    expect(from).toBeGreaterThanOrEqual(before - week - 1000);
    expect(from).toBeLessThanOrEqual(Date.now() - week + 1000);
  });

  // go-quicbaas#1058, ruling F6: the scope's own limits, which defer
  // admission when spent.
  it('shows the scope limits and marks a spent window as deferring admission', async () => {
    getSpendReport.mockResolvedValue({
      ...tenantReport,
      scope_policy_status: 'metered',
      scope_windows: [
        { name: 'weekly', size: 604_800_000_000_000, max_tokens: 1000, used_tokens: 1200, remaining_tokens: 0, exhausted: true },
      ],
    });
    render(CostView);
    const limits = await screen.findByTestId('scope-limits');
    expect(limits.textContent).toContain('weekly');
    expect(limits.textContent).toContain('admission deferred');
  });

  it('says so when the scope has no spend limits', async () => {
    getSpendReport.mockResolvedValue({ ...tenantReport, scope_policy_status: 'none' });
    render(CostView);
    expect((await screen.findByTestId('scope-limits')).textContent).toContain('No spend limit is set here');
    expect(screen.queryByTestId('scope-window')).toBeNull();
  });

  it('discards a report that arrives for a tenant the operator has left', async () => {
    let settle: ((value: GetSpendReportResponse) => void) | undefined;
    getSpendReport.mockReset();
    getSpendReport.mockReturnValueOnce(
      new Promise<GetSpendReportResponse>((resolve) => {
        settle = resolve;
      }),
    );

    render(CostView);
    await waitFor(() => expect(getSpendReport).toHaveBeenCalled());

    // The operator switches tenant while acme's read is still in flight. The
    // drill-down scope is unchanged, so a scope-only guard lets this through.
    storeMock.activeTenantID = 'other';
    settle!(tenantReport);

    // Drain the resolved read and Svelte's render queue before judging. A
    // waitFor would pass on its first tick, before the stale report could have
    // been applied, and prove nothing.
    await Promise.resolve();
    await Promise.resolve();
    await tick();

    // acme's totals must never render as "other"'s.
    expect(screen.queryByTestId('scope-cost')).toBeNull();
  });

  it('breaks the spend down by host and says how much of its energy was measured', async () => {
    getSpendReport.mockResolvedValue({
      ...tenantReport,
      energy_mwh: 9_000,
      electricity_entries: 3,
      measured_energy_entries: 2,
      hosts: [
        { host: 'gpu-host:9091', total_tokens: 150, energy_mwh: 9_000, electricity_entries: 3, measured_energy_entries: 2 },
        { unattributed: true, total_tokens: 50 },
      ],
    });
    render(CostView);
    const hosts = await screen.findAllByTestId('spend-host');
    expect(hosts).toHaveLength(2);
    expect(hosts[0]!.textContent).toContain('gpu-host:9091');
    expect(screen.getByTestId('spend-host-energy').textContent).toContain('2 of 3 measured');
    expect(hosts[1]!.textContent).toContain('No host recorded');
    expect(screen.getByText('3 local turns costed from electricity · 2 of 3 measured')).toBeTruthy();
  });

  it('narrows the page to a host, keeps it while drilling, and clears it', async () => {
    getSpendReport.mockImplementation((req: { host?: string; project_id?: string }) =>
      Promise.resolve({
        ...(req.project_id ? projectReport : tenantReport),
        host: req.host,
        hosts: [{ host: 'gpu-host:9091', total_tokens: 150 }],
      }),
    );
    render(CostView);
    await fireEvent.click(await screen.findByRole('button', { name: 'Show only gpu-host:9091' }));
    await waitFor(() => expect(getSpendReport.mock.calls.at(-1)![0]).toMatchObject({ host: 'gpu-host:9091' }));
    expect((await screen.findByTestId('spend-host-filter')).textContent).toContain('gpu-host:9091');

    // Drilling into a project keeps the host: it is a filter, not a level.
    await fireEvent.click(await screen.findByText('proj-1'));
    await waitFor(() =>
      expect(getSpendReport.mock.calls.at(-1)![0]).toMatchObject({ project_id: 'proj-1', host: 'gpu-host:9091' }),
    );

    await fireEvent.click(await screen.findByRole('button', { name: 'Show every host' }));
    await waitFor(() => expect(getSpendReport.mock.calls.at(-1)![0].host).toBeUndefined());
    expect(screen.queryByTestId('spend-host-filter')).toBeNull();
  });

  it('edits the tenant limits in place and reloads the report after saving', async () => {
    getSpendReport.mockResolvedValue({ ...tenantReport, scope_policy_status: 'none' });
    getTenantSpendPolicy.mockResolvedValue({
      tenant_id: 'acme',
      limits: { windows: [{ name: 'daily', size: 86_400_000_000_000, max_equivalent_micros: 50_000_000 }] },
    });
    setTenantSpendPolicy.mockResolvedValue({ tenant_id: 'acme' });
    render(CostView);

    await fireEvent.click(await screen.findByTestId('edit-tenant-limits'));
    await waitFor(() => expect(getTenantSpendPolicy).toHaveBeenCalledWith({ tenant_id: 'acme' }));
    expect(await screen.findAllByTestId('spend-window-row')).toHaveLength(1);

    const calls = getSpendReport.mock.calls.length;
    await fireEvent.click(screen.getByRole('button', { name: 'Save limits' }));
    await waitFor(() => expect(setTenantSpendPolicy).toHaveBeenCalled());
    expect(setTenantSpendPolicy.mock.calls[0]![0]).toEqual({
      tenant_id: 'acme',
      limits: { windows: [{ name: 'daily', size: 86_400_000_000_000, max_equivalent_micros: 50_000_000 }] },
    });
    await waitFor(() => expect(getSpendReport.mock.calls.length).toBeGreaterThan(calls));
    expect(screen.queryByTestId('tenant-limits-editor')).toBeNull();
  });

  it('refuses to save tenant limits that cap nothing', async () => {
    getSpendReport.mockResolvedValue({ ...tenantReport, scope_policy_status: 'none' });
    getTenantSpendPolicy.mockResolvedValue({ tenant_id: 'acme' });
    render(CostView);

    await fireEvent.click(await screen.findByTestId('edit-tenant-limits'));
    await fireEvent.click(await screen.findByRole('button', { name: /Add window/ }));
    await fireEvent.click(screen.getByRole('button', { name: 'Save limits' }));
    expect((await screen.findByTestId('tenant-limits-error')).textContent).toContain('caps nothing');
    expect(setTenantSpendPolicy).not.toHaveBeenCalled();
  });

  it('says why the tenant limits could not be read', async () => {
    getSpendReport.mockResolvedValue({ ...tenantReport, scope_policy_status: 'none' });
    getTenantSpendPolicy.mockRejectedValue(Object.assign(new Error('forbidden'), { status: 403 }));
    render(CostView);
    await fireEvent.click(await screen.findByTestId('edit-tenant-limits'));
    expect((await screen.findByTestId('tenant-limits-error')).textContent).toMatch(/permission|forbidden/i);
    expect(screen.getByRole('button', { name: 'Save limits' }).hasAttribute('disabled')).toBe(true);
  });

  it('breaks the spend down by Agent, narrows to one and shows its own limits', async () => {
    getSpendReport.mockImplementation((req: { agent?: string }) =>
      Promise.resolve({
        ...tenantReport,
        agent: req.agent,
        agents: [
          { agent_id: 'claude-coder', name: 'Claude coder', total_tokens: 150 },
          { unattributed: true, total_tokens: 50 },
        ],
        ...(req.agent
          ? {
              agent_policy_status: 'metered',
              agent_windows: [{ name: 'daily', size: 86_400_000_000_000, max_tokens: 100, used_tokens: 150, remaining_tokens: 0, exhausted: true }],
            }
          : {}),
      }),
    );
    render(CostView);
    const agents = await screen.findAllByTestId('spend-agent');
    expect(agents).toHaveLength(2);
    expect(agents[1]!.textContent).toContain('No Agent (raw backend)');
    expect(screen.queryByTestId('agent-limits')).toBeNull();

    await fireEvent.click(screen.getByRole('button', { name: 'Show only Claude coder' }));
    await waitFor(() => expect(getSpendReport.mock.calls.at(-1)![0]).toMatchObject({ agent: 'claude-coder' }));
    expect((await screen.findByTestId('spend-agent-filter')).textContent).toContain('claude-coder');
    expect((await screen.findByTestId('agent-window')).textContent).toContain('passed over until it frees');

    await fireEvent.click(screen.getByRole('button', { name: 'Show every Agent' }));
    await waitFor(() => expect(getSpendReport.mock.calls.at(-1)![0].agent).toBeUndefined());
  });

  it('edits the narrowed-to Agent\'s limits, keeping every other field of the Agent', async () => {
    const coder = { id: 'claude-coder', name: 'Claude coder', roles: ['coding'], backend: 'claude', model: 'claude-opus-5-5', image: 'worker:1' };
    getSpendReport.mockResolvedValue({ ...tenantReport, agent: 'claude-coder', agent_policy_status: 'none' });
    getAgent.mockResolvedValue(coder);
    updateAgent.mockResolvedValue(coder);
    render(CostView);

    await fireEvent.click(await screen.findByTestId('edit-agent-limits'));
    await waitFor(() => expect(getAgent).toHaveBeenCalledWith('claude-coder', 'acme'));
    await fireEvent.click(await screen.findByRole('button', { name: /Add window/ }));
    const inputs = screen.getAllByTestId('spend-window-row')[0]!.querySelectorAll('input');
    // name, hours, tokens: a daily window of 1000 tokens.
    await fireEvent.input(inputs[0]!, { target: { value: 'daily' } });
    await fireEvent.input(inputs[1]!, { target: { value: '24' } });
    await fireEvent.input(inputs[2]!, { target: { value: '1000' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save limits' }));
    await waitFor(() => expect(updateAgent).toHaveBeenCalled());
    const [id, saved, tenant] = updateAgent.mock.calls[0]!;
    expect(id).toBe('claude-coder');
    expect(tenant).toBe('acme');
    expect(saved).toMatchObject({ ...coder, spend: { windows: [{ name: 'daily', max_tokens: 1000 }] } });
  });

  it('says why an Agent\'s limits could not be read and refuses to save', async () => {
    getSpendReport.mockResolvedValue({ ...tenantReport, agent: 'claude-coder', agent_policy_status: 'none' });
    getAgent.mockRejectedValue(Object.assign(new Error('forbidden'), { status: 403 }));
    render(CostView);
    await fireEvent.click(await screen.findByTestId('edit-agent-limits'));
    expect((await screen.findByTestId('agent-limits-error')).textContent).toMatch(/permission|forbidden/i);
    expect(screen.getByRole('button', { name: 'Save limits' }).hasAttribute('disabled')).toBe(true);
    expect(updateAgent).not.toHaveBeenCalled();
  });
});
