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

const { storeMock, getSpendReport } = vi.hoisted(() => ({
  storeMock: { activeTenantID: 'acme' },
  getSpendReport: vi.fn(),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: storeMock,
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({ orchestrator: { getSpendReport } }),
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
  cost_micros: 0,
  cost_measured: false,
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
    expect(screen.getByText(/cost unmeasured/i)).toBeTruthy();
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
});
