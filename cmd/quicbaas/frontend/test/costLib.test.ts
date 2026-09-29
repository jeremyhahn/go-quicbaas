/**
 * costLib.test.ts
 *
 * The honesty rules the Cost views render by, tested where they live rather
 * than through a DOM. go-quicbaas#794: a number the system does not have must
 * be absent and labelled, never a zero.
 */

import { describe, it, expect } from 'vitest';
import {
  agentFilterLabel,
  agentLabel,
  agentScopeValue,
  reportProjectName,
  SPEND_UNATTRIBUTED_LEVEL,
  UNMEASURED,
  childIsDrillable,
  childIsNamed,
  childLabel,
  childRound,
  childScopeValue,
  billedNote,
  billingLabel,
  costSettingsForm,
  costSettingsRequest,
  electricitySourceLabel,
  energyNote,
  measuredShare,
  hostLabel,
  hostScopeValue,
  hostFilterLabel,
  equivalentNote,
  formatBilled,
  formatBilledFloor,
  formatEquivalent,
  formatEquivalentFloor,
  formatEnergy,
  formatFeeShare,
  formatMoney,
  formatDurationNanos,
  formatTokens,
  levelLabel,
  moneyNote,
  noIterationBudgetNotice,
  policyLabel,
  policyVariant,
  roundLabel,
  scopeLabel,
  spendErrorMessage,
  spendRangeFrom,
  windowPercent,
  windowSummary,
} from '../src/lib/cost.ts';
import type { SpendChild, SpendWindowStatus } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// The iteration budget
//
// WHICH runner iterates is lib/jobDetail.ts's `isWorkflowJob`, tested in
// jobDetailLib.test.ts. This file owns only the sentence shown when that
// answers no.
// ---------------------------------------------------------------------------

describe('noIterationBudgetNotice', () => {
  it('names the runner so the reader knows which one ran', () => {
    expect(noIterationBudgetNotice('sdlc')).toContain('sdlc');
    expect(noIterationBudgetNotice('sdlc')).toContain('does not iterate');
  });

  it('says so plainly when the runner is not known', () => {
    expect(noIterationBudgetNotice('')).toBe('This job has no iteration budget.');
    expect(noIterationBudgetNotice(undefined)).toBe('This job has no iteration budget.');
  });
});

// ---------------------------------------------------------------------------
// Tokens and cost
// ---------------------------------------------------------------------------

describe('formatTokens', () => {
  it('renders a real zero as a measurement, because spending nothing is a fact', () => {
    expect(formatTokens(0)).toBe('0');
  });

  it('renders a count with thousands separators', () => {
    expect(formatTokens(1234567)).toBe((1234567).toLocaleString());
  });

  it('renders an absent or nonsense count as unmeasured', () => {
    expect(formatTokens(undefined)).toBe(UNMEASURED);
    expect(formatTokens(null)).toBe(UNMEASURED);
    expect(formatTokens(Number.NaN)).toBe(UNMEASURED);
    expect(formatTokens(Number.POSITIVE_INFINITY)).toBe(UNMEASURED);
  });
});

describe('formatMoney', () => {
  it('renders micro-units as currency', () => {
    expect(formatMoney(1_500_000)).toBe('$1.50');
    expect(formatMoney(1_234_567_890)).toBe('$1,234.57');
    expect(formatMoney(0)).toBe('$0.00');
  });

  it('never renders a sub-cent charge as free', () => {
    expect(formatMoney(20)).toBe('<$0.01');
  });

  it('is a dash for a figure that is not a number', () => {
    expect(formatMoney(Number.NaN)).toBe(UNMEASURED);
  });
});

describe('formatEquivalent / equivalentNote', () => {
  // go-quicbaas#1058: the Cost page showed no dollars at all.
  it('is a dash when nothing in the scope was priced, never a zero', () => {
    expect(formatEquivalent({ equivalent_micros: 0, priced_entries: 0, unpriced_entries: 4 })).toBe(UNMEASURED);
    expect(equivalentNote({ equivalent_micros: 0, priced_entries: 0, unpriced_entries: 4 })).toBe('no price for these models');
    expect(formatEquivalent(null)).toBe(UNMEASURED);
  });

  it('renders the priced figure and says when it is a floor', () => {
    const totals = { equivalent_micros: 42_500_000, priced_entries: 10, unpriced_entries: 2 };
    expect(formatEquivalent(totals)).toBe('$42.50');
    expect(equivalentNote(totals)).toBe('at least — 2 turns unpriced');
    expect(equivalentNote({ ...totals, unpriced_entries: 0 })).toBe('at list price');
  });

  it('renders a priced zero as a measured zero', () => {
    expect(formatEquivalent({ equivalent_micros: 0, priced_entries: 3 })).toBe('$0.00');
  });
});

describe('formatBilled / billedNote', () => {
  it('shows a subscription scope as billed zero, which is a measurement', () => {
    const totals = { billed_micros: 0, billed_entries: 5, unbilled_entries: 0 };
    expect(formatBilled(totals)).toBe('$0.00');
    expect(billedNote(totals)).toBe('actually billed');
  });

  it('is a dash when no turn has a known bill', () => {
    expect(formatBilled({ billed_micros: 0, billed_entries: 0, unbilled_entries: 3 })).toBe(UNMEASURED);
    expect(billedNote({ billed_micros: 0, billed_entries: 0, unbilled_entries: 3 })).toBe('billing not configured');
    expect(formatBilled(undefined)).toBe(UNMEASURED);
  });

  it('says when the billed figure is a floor', () => {
    expect(billedNote({ billed_micros: 5, billed_entries: 1, unbilled_entries: 1 })).toBe(
      'at least — 1 turn with unknown billing',
    );
  });
});

describe('formatEquivalentFloor / formatBilledFloor / moneyNote', () => {
  it('marks a partially covered row as a floor', () => {
    const totals = {
      equivalent_micros: 1_200_000, priced_entries: 2, unpriced_entries: 1,
      billed_micros: 500_000, billed_entries: 1, unbilled_entries: 2,
    };
    expect(formatEquivalentFloor(totals)).toBe('≥$1.20');
    expect(formatBilledFloor(totals)).toBe('≥$0.50');
    expect(moneyNote(totals)).toBe(
      'equivalent: at least — 1 turn unpriced · billed: at least — 2 turns with unknown billing',
    );
  });

  it('leaves a fully covered row exact and an uncovered one a dash', () => {
    expect(formatEquivalentFloor({ equivalent_micros: 1_200_000, priced_entries: 2, unpriced_entries: 0 })).toBe('$1.20');
    expect(formatBilledFloor({ billed_micros: 0, billed_entries: 2, unbilled_entries: 0 })).toBe('$0.00');
    expect(formatEquivalentFloor({ equivalent_micros: 0, priced_entries: 0, unpriced_entries: 3 })).toBe(UNMEASURED);
    expect(formatBilledFloor({ billed_micros: 0, billed_entries: 0, unbilled_entries: 3 })).toBe(UNMEASURED);
    expect(formatEquivalentFloor(undefined)).toBe(UNMEASURED);
  });
});

describe('formatFeeShare', () => {
  it('renders a fee share as money, a zero share as zero', () => {
    expect(formatFeeShare(46_000_000)).toBe('$46.00');
    expect(formatFeeShare(0)).toBe('$0.00');
  });

  it('renders an absent share as a dash, never as zero', () => {
    expect(formatFeeShare(undefined)).toBe(UNMEASURED);
    expect(formatFeeShare(null)).toBe(UNMEASURED);
  });
});

describe('spendRangeFrom', () => {
  const now = new Date('2026-09-24T12:00:00Z');

  it('starts each range its length before now', () => {
    expect(spendRangeFrom('24h', now)).toBe('2026-09-23T12:00:00.000Z');
    expect(spendRangeFrom('7d', now)).toBe('2026-09-17T12:00:00.000Z');
    expect(spendRangeFrom('30d', now)).toBe('2026-08-25T12:00:00.000Z');
  });

  it('sends no start for every turn', () => {
    expect(spendRangeFrom('all', now)).toBeUndefined();
  });
});

describe('formatEnergy / energyNote', () => {
  it('renders Wh below a kWh and kWh above', () => {
    expect(formatEnergy({ energy_mwh: 12_000, electricity_entries: 1 })).toBe('12 Wh');
    expect(formatEnergy({ energy_mwh: 6_250, electricity_entries: 1 })).toBe('6.25 Wh');
    expect(formatEnergy({ energy_mwh: 2_500_000, electricity_entries: 3 })).toBe('2.5 kWh');
    expect(formatEnergy({ energy_mwh: 4, electricity_entries: 1 })).toBe('<0.01 Wh');
  });

  it('is a dash when no turn was costed from electricity', () => {
    expect(formatEnergy({ energy_mwh: 0, electricity_entries: 0 })).toBe(UNMEASURED);
    expect(formatEnergy(undefined)).toBe(UNMEASURED);
    expect(energyNote({ electricity_entries: 0 })).toBe('no turn costed from electricity');
    expect(energyNote({ electricity_entries: 1 })).toBe('1 local turn costed from electricity · all estimated');
  });

  it('says how much of the energy was measured rather than estimated', () => {
    expect(energyNote({ electricity_entries: 5, measured_energy_entries: 3 })).toBe(
      '5 local turns costed from electricity · 3 of 5 measured',
    );
    expect(measuredShare({ electricity_entries: 2, measured_energy_entries: 2 })).toBe('all measured');
    expect(measuredShare({ electricity_entries: 2 })).toBe('all estimated');
    expect(measuredShare({ electricity_entries: 0, measured_energy_entries: 0 })).toBe(UNMEASURED);
    // A count the server over-reports is clamped, never shown as 3 of 2.
    expect(measuredShare({ electricity_entries: 2, measured_energy_entries: 3 })).toBe('all measured');
  });
});

describe('host rows', () => {
  it('labels an unattributed host as an absence and addresses it by the sentinel', () => {
    expect(hostLabel({ host: 'gpu-host:9091' })).toBe('gpu-host:9091');
    expect(hostLabel({ unattributed: true })).toBe('No host recorded');
    expect(hostScopeValue({ host: 'gpu-host:9091' })).toBe('gpu-host:9091');
    expect(hostScopeValue({ unattributed: true })).toBe(SPEND_UNATTRIBUTED_LEVEL);
    expect(hostFilterLabel(SPEND_UNATTRIBUTED_LEVEL)).toBe('No host recorded');
    expect(hostFilterLabel('vm-host:9091')).toBe('vm-host:9091');
  });
});

describe('electricitySourceLabel', () => {
  it('names every source, and an unknown one as unknown', () => {
    expect(electricitySourceLabel('default')).toBe('the default');
    expect(electricitySourceLabel('tenant_settings')).toBe('set for this tenant');
    expect(electricitySourceLabel('tenant_config')).toContain('tenant');
    expect(electricitySourceLabel('deployment_config')).toContain('deployment');
    expect(electricitySourceLabel('nonsense')).toBe('unknown source');
  });
});

describe('costSettingsForm / costSettingsRequest', () => {
  const settings = {
    settings: { tenant_id: 'acme', electricity_price_per_kwh: 0.2, engine_power_watts: { ollama: 250, openvino: 12 } },
    electricity: { price_per_kwh: 0.2, source: 'tenant_settings' as const },
  };

  it('round-trips what the tenant set, and only that', () => {
    const form = costSettingsForm(settings);
    expect(form.price).toBe('0.2');
    expect(form.engines).toEqual([{ engine: 'ollama', watts: '250' }, { engine: 'openvino', watts: '12' }]);
    const built = costSettingsRequest('acme', form);
    expect(built).toEqual({
      request: { tenant_id: 'acme', electricity_price_per_kwh: 0.2, engine_power_watts: { ollama: 250, openvino: 12 } },
    });
    expect(costSettingsForm(null)).toEqual({ price: '', engines: [] });
  });

  it('leaves a blank price and blank rows out, which inherits the configuration', () => {
    expect(costSettingsRequest('acme', { price: ' ', engines: [{ engine: '', watts: '' }] })).toEqual({
      request: { tenant_id: 'acme' },
    });
  });

  it('refuses what the server would refuse, before the round trip', () => {
    const bad = [
      { price: '-1', engines: [] },
      { price: 'cheap', engines: [] },
      { price: '', engines: [{ engine: '', watts: '5' }] },
      { price: '', engines: [{ engine: 'a/b', watts: '5' }] },
      { price: '', engines: [{ engine: 'ollama', watts: '' }] },
      { price: '', engines: [{ engine: 'ollama', watts: '-3' }] },
      { price: '', engines: [{ engine: 'ollama', watts: '1' }, { engine: 'ollama', watts: '2' }] },
    ];
    for (const form of bad) {
      expect('error' in costSettingsRequest('acme', form)).toBe(true);
    }
  });
});

describe('billingLabel', () => {
  it('names each mode, with a subscription\'s fee', () => {
    expect(billingLabel({ mode: 'subscription', monthly_fee_micros: 200_000_000 })).toBe('Subscription · $200.00/mo');
    expect(billingLabel({ mode: 'subscription' })).toBe('Subscription');
    expect(billingLabel({ mode: 'api' })).toBe('Metered API');
  });

  it('says billing is not configured rather than guessing', () => {
    expect(billingLabel(undefined)).toBe('Billing not configured');
    expect(billingLabel({})).toBe('Billing not configured');
  });
});

// ---------------------------------------------------------------------------
// Spend policy
// ---------------------------------------------------------------------------

describe('policyLabel', () => {
  it('says "No spend policy" for an account that was read and meters nothing', () => {
    expect(policyLabel({ policy_status: 'none' })).toBe('No spend policy');
  });

  it('says "unknown" for an account the server could not read', () => {
    // A DIFFERENT FACT. Collapsing it into "no spend policy" would let an
    // unwired deployment report every account as uncapped.
    expect(policyLabel({ policy_status: 'unknown' })).toBe('Spend policy unknown');
  });

  it('says "Metered" for an account with a cap', () => {
    expect(policyLabel({ policy_status: 'metered' })).toBe('Metered');
  });

  it('falls back to unknown for a status this build does not recognise', () => {
    expect(policyLabel({ policy_status: 'something-new' as never })).toBe('Spend policy unknown');
  });
});

describe('policyVariant', () => {
  it('maps each known status onto its own variant', () => {
    expect(policyVariant({ policy_status: 'metered' })).toBe('success');
    expect(policyVariant({ policy_status: 'none' })).toBe('info');
    expect(policyVariant({ policy_status: 'unknown' })).toBe('warning');
  });

  it('renders an unrecognised status as a warning, never as the reassuring one', () => {
    expect(policyVariant({ policy_status: 'something-new' as never })).toBe('warning');
  });
});

// ---------------------------------------------------------------------------
// Quota windows
// ---------------------------------------------------------------------------

const fiveHour: SpendWindowStatus = {
  name: 'five-hour',
  size: 5 * 3_600 * 1_000_000_000,
  max_tokens: 1000,
  used_tokens: 250,
  remaining_tokens: 750,
  exhausted: false,
};

describe('windowPercent', () => {
  it('is the share of the allowance drawn', () => {
    expect(windowPercent(fiveHour)).toBe(25);
  });

  it('clamps an over-spend at 100 rather than reporting more than a full window', () => {
    expect(windowPercent({ max_tokens: 100, used_tokens: 500 })).toBe(100);
  });

  it('is null for a window with no allowance, rather than Infinity or a false 0%', () => {
    expect(windowPercent({ max_tokens: 0, used_tokens: 10 })).toBeNull();
    expect(windowPercent({ max_tokens: Number.NaN, used_tokens: 10 })).toBeNull();
  });
  it('is as full as the fullest metered axis when a window meters dollars too', () => {
    expect(windowPercent({ max_tokens: 1000, used_tokens: 100, max_equivalent_micros: 10_000_000, used_equivalent_micros: 8_000_000 })).toBe(80);
    expect(windowPercent({ max_tokens: 0, used_tokens: 0, max_equivalent_micros: 4, used_equivalent_micros: 1 })).toBe(25);
  });
});

describe('windowSummary', () => {
  it('reads as spent-of-allowance over the window length', () => {
    expect(windowSummary(fiveHour)).toBe('250 / 1,000 tokens per 5h');
  });

  it('names each metered axis, dollars at list price', () => {
    expect(
      windowSummary({ ...fiveHour, max_equivalent_micros: 10_000_000, used_equivalent_micros: 2_500_000 }),
    ).toBe('250 / 1,000 tokens · $2.50 / $10.00 per 5h');
    expect(
      windowSummary({ ...fiveHour, max_tokens: 0, max_equivalent_micros: 10_000_000, used_equivalent_micros: 0 }),
    ).toBe('$0.00 / $10.00 per 5h');
  });
});

describe('formatDurationNanos', () => {
  it('picks the coarsest unit that divides evenly', () => {
    expect(formatDurationNanos(5 * 3_600 * 1e9)).toBe('5h');
    expect(formatDurationNanos(7 * 86_400 * 1e9)).toBe('7d');
    expect(formatDurationNanos(90 * 60 * 1e9)).toBe('90m');
    expect(formatDurationNanos(30 * 1e9)).toBe('30s');
  });

  it('is unmeasured for a missing or non-positive duration', () => {
    expect(formatDurationNanos(0)).toBe(UNMEASURED);
    expect(formatDurationNanos(-1)).toBe(UNMEASURED);
    expect(formatDurationNanos(undefined)).toBe(UNMEASURED);
  });

  // The policy validator accepts any POSITIVE time.Duration, and the wire
  // carries nanoseconds precisely so a window shorter than a second survives
  // it. Rounding to whole seconds rendered a 100ms window as "0s" -- a real
  // window that reads as no window at all -- and 1.5s as "2s".
  it('keeps a window shorter than a second', () => {
    expect(formatDurationNanos(100 * 1e6)).toBe('100ms');
    expect(formatDurationNanos(1e6)).toBe('1ms');
    expect(formatDurationNanos(1.5 * 1e6)).toBe('1.5ms');
  });

  it('keeps a fractional second rather than rounding it away', () => {
    expect(formatDurationNanos(1.5 * 1e9)).toBe('1.5s');
    expect(formatDurationNanos(2.25 * 1e9)).toBe('2.25s');
  });
});

// ---------------------------------------------------------------------------
// The drill-down
// ---------------------------------------------------------------------------

const attributed: SpendChild = { id: 'task-1', level: 'task', total_tokens: 10 };
const unattributed: SpendChild = { level: 'task', unattributed: true, total_tokens: 5 };

describe('childLabel', () => {
  it('names an attributed row by its id', () => {
    expect(childLabel(attributed)).toBe('task-1');
  });

  it('names the no-value bucket as an absence, never as an entity called "-"', () => {
    expect(childLabel(unattributed)).toBe('No task');
  });

  // go-quicbaas#1058: the By Project card listed bare project ids.
  it('names a project row by its name when the server supplied one', () => {
    const project: SpendChild = { id: 'p-9f2c', level: 'project', name: 'go-truststrap', total_tokens: 7 };
    expect(childLabel(project)).toBe('go-truststrap');
    expect(childIsNamed(project)).toBe(true);
    // The id is still what a drill step addresses.
    expect(childScopeValue(project)).toBe('p-9f2c');
  });

  it('falls back to the id for a project that no longer exists', () => {
    const gone: SpendChild = { id: 'p-dead', level: 'project', name: '  ', total_tokens: 7 };
    expect(childLabel(gone)).toBe('p-dead');
    expect(childIsNamed(gone)).toBe(false);
  });

  it('never names the no-value bucket, even if a name were sent', () => {
    expect(childIsNamed({ ...unattributed, name: 'x' })).toBe(false);
    expect(childLabel({ ...unattributed, name: 'x' })).toBe('No task');
  });
});

describe('childScopeValue / childIsDrillable', () => {
  it('addresses an attributed row by its id', () => {
    expect(childScopeValue(attributed)).toBe('task-1');
    expect(childIsDrillable(attributed)).toBe(true);
  });

  it('addresses the no-value bucket by the ledger sentinel, so it can be opened', () => {
    expect(childScopeValue(unattributed)).toBe(SPEND_UNATTRIBUTED_LEVEL);
    expect(childIsDrillable(unattributed)).toBe(true);
  });

  it('refuses to drill a row that names nothing at all', () => {
    expect(childIsDrillable({ level: 'task', total_tokens: 1 })).toBe(false);
  });
});

// go-quicbaas#886: a round is the terminal level and is addressed by a NUMBER,
// so a round row is drillable only when its id is one.
describe('childRound', () => {
  it('reads a round row as the number it addresses, including round 0', () => {
    expect(childRound({ id: '3', level: 'round', total_tokens: 1 })).toBe(3);
    expect(childRound({ id: '0', level: 'round', total_tokens: 1 })).toBe(0);
    expect(childIsDrillable({ id: '0', level: 'round', total_tokens: 1 })).toBe(true);
  });

  it('refuses a round row that is not a non-negative integer', () => {
    for (const id of ['', 'x', '-1', '1.5', '2e3', ' 4']) {
      const child: SpendChild = { id, level: 'round', total_tokens: 1 };
      expect(childRound(child)).toBeNull();
      expect(childIsDrillable(child)).toBe(false);
    }
  });

  it('refuses an unattributed round, which no number can address', () => {
    const child: SpendChild = { level: 'round', unattributed: true, total_tokens: 1 };
    expect(childRound(child)).toBeNull();
    expect(childIsDrillable(child)).toBe(false);
  });

  it('is not a round for a row at another level', () => {
    expect(childRound({ id: '3', level: 'session', total_tokens: 1 })).toBeNull();
  });
});

describe('roundLabel', () => {
  it('names the round for the breadcrumb', () => {
    expect(roundLabel(0)).toBe('Round 0');
    expect(roundLabel(12)).toBe('Round 12');
  });
});

describe('scopeLabel', () => {
  it('renders the sentinel as the absence it stands for', () => {
    expect(scopeLabel('project', SPEND_UNATTRIBUTED_LEVEL)).toBe('No project');
  });

  it('renders a real id unchanged', () => {
    expect(scopeLabel('project', 'proj-1')).toBe('proj-1');
  });

  it('renders the name when one is known, and the id when it is blank', () => {
    expect(scopeLabel('project', 'proj-1', 'go-truststrap')).toBe('go-truststrap');
    expect(scopeLabel('project', 'proj-1', ' ')).toBe('proj-1');
  });

  it('keeps the sentinel an absence even beside a name', () => {
    expect(scopeLabel('project', SPEND_UNATTRIBUTED_LEVEL, 'x')).toBe('No project');
  });
});

describe('levelLabel', () => {
  it('names each chain level for a person', () => {
    expect(levelLabel('project')).toBe('Project');
    expect(levelLabel('round')).toBe('Round');
  });

  it('renders an unknown level as itself rather than as a blank', () => {
    expect(levelLabel('galaxy')).toBe('galaxy');
    expect(levelLabel(undefined)).toBe('');
  });
});

// ---------------------------------------------------------------------------
// Refusals
// ---------------------------------------------------------------------------

describe('spendErrorMessage', () => {
  it('says the deployment records no spend on a 503, not that nothing was spent', () => {
    const msg = spendErrorMessage({ status: 503 }, 'spend');
    expect(msg).toContain('not recording provider consumption');
  });

  it('names the permission on a 403', () => {
    expect(spendErrorMessage({ status: 403 }, 'spend')).toContain('orchestrator:status');
  });

  it('tells the reader to sign in again on a 401', () => {
    expect(spendErrorMessage({ status: 401 }, 'spend')).toContain('Sign in again');
  });

  it('carries the server’s own message on a malformed scope', () => {
    expect(spendErrorMessage(new Error('task_id cannot be scoped'), 'spend')).toContain(
      'task_id cannot be scoped',
    );
  });

  it('carries the underlying text for anything else', () => {
    expect(spendErrorMessage(new Error('boom'), 'spend')).toBe('Could not load spend: boom');
  });
});

describe('reportProjectName', () => {
  // Round-2 review of #1063: a report without a name must CLEAR the breadcrumb
  // name. Keeping the previous one showed a project of the tenant the operator
  // had just left, under the new tenant, indefinitely.
  it('replaces the name on every accepted report, clearing it when the report has none', () => {
    let name = reportProjectName({ project_name: 'alpha' });
    expect(name).toBe('alpha');
    name = reportProjectName({});
    expect(name).toBe('');
    name = reportProjectName({ project_name: 'beta' });
    expect(name).toBe('beta');
  });
});

describe('Agent rows', () => {
  it('names an Agent by its name, then its ID, and the raw-backend row as an absence', () => {
    expect(agentLabel({ agent_id: 'claude-coder', name: 'Claude coder' })).toBe('Claude coder');
    expect(agentLabel({ agent_id: 'claude-coder' })).toBe('claude-coder');
    expect(agentLabel({ unattributed: true })).toBe('No Agent (raw backend)');
  });

  it('narrows to the Agent, or to the sentinel for raw-backend sessions', () => {
    expect(agentScopeValue({ agent_id: 'claude-coder' })).toBe('claude-coder');
    expect(agentScopeValue({ unattributed: true })).toBe(SPEND_UNATTRIBUTED_LEVEL);
    expect(agentFilterLabel(SPEND_UNATTRIBUTED_LEVEL)).toBe('No Agent (raw backend)');
    expect(agentFilterLabel('claude-coder')).toBe('claude-coder');
  });
});
