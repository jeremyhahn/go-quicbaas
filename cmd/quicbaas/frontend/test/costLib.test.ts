/**
 * costLib.test.ts
 *
 * The honesty rules the Cost views render by, tested where they live rather
 * than through a DOM. go-quicbaas#794: a number the system does not have must
 * be absent and labelled, never a zero.
 */

import { describe, it, expect } from 'vitest';
import {
  reportProjectName,
  SPEND_UNATTRIBUTED_LEVEL,
  UNMEASURED,
  childIsDrillable,
  childIsNamed,
  childLabel,
  childScopeValue,
  costIsMeasured,
  formatCost,
  formatDurationNanos,
  formatTokens,
  levelLabel,
  noIterationBudgetNotice,
  policyLabel,
  policyVariant,
  scopeLabel,
  spendErrorMessage,
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

describe('formatCost', () => {
  it('is a dash when the server says the cost was never measured', () => {
    // Nothing prices a turn (go-quicbaas#791), so this is EVERY production
    // report today. Rendering the zero would claim the work was free.
    expect(formatCost({ cost_micros: 0, cost_measured: false })).toBe(UNMEASURED);
  });

  it('is still a dash when an unmeasured report carries a non-zero figure', () => {
    expect(formatCost({ cost_micros: 12_000_000, cost_measured: false })).toBe(UNMEASURED);
  });

  it('renders the figure once the server says it was measured', () => {
    expect(formatCost({ cost_micros: 1_500_000, cost_measured: true })).toBe('1.50');
  });

  it('is a dash for no report at all', () => {
    expect(formatCost(null)).toBe(UNMEASURED);
  });
});

describe('costIsMeasured', () => {
  it('is false for an unmeasured report and for no report', () => {
    expect(costIsMeasured({ cost_measured: false })).toBe(false);
    expect(costIsMeasured(null)).toBe(false);
  });

  it('is true only when the server said so', () => {
    expect(costIsMeasured({ cost_measured: true })).toBe(true);
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
});

describe('windowSummary', () => {
  it('reads as spent-of-allowance over the window length', () => {
    expect(windowSummary(fiveHour)).toBe('250 / 1,000 tokens per 5h');
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
