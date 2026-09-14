/**
 * sdlcConformance.test.ts
 *
 * The onboarding conformance panel's presentation rules. Two of them are
 * correctness rather than styling and are the reason this logic is in a
 * module rather than in the component:
 *
 *   - WHICH findings the repair button claims to fix. Provisioning fixes a
 *     missing webhook or label; it does not delete a workflow from the
 *     repository and it deliberately does not create branch protection. A
 *     button that claims to fix something and does not is worse than no
 *     button, because the operator stops looking.
 *   - WHICH probe failures hide the panel. A node that does not run the
 *     module has nothing to show; anything else is an error the operator must
 *     be told about, because a silently missing panel and a silently failing
 *     one look identical and only one of them is fine.
 */

import { describe, it, expect } from 'vitest';
import type { ConformanceFinding, ConformanceReport } from '@quicbaas/sdk';
import {
  checkStatusClass,
  conformanceAvailabilityFor,
  conformanceBadgeClass,
  conformanceSummary,
  hasRepairableFindings,
  isRepairable,
} from '../src/lib/sdlcConformance.ts';

function finding(cls: string): ConformanceFinding {
  return { class: cls, check: 'x', subject: 's', detail: 'd', remedy: 'r' };
}

function report(overrides: Partial<ConformanceReport> = {}): ConformanceReport {
  return {
    project_id: 'p1',
    conformant: true,
    checks: [],
    checked_at: '2026-09-12T12:00:00Z',
    ...overrides,
  };
}

describe('isRepairable', () => {
  it('offers a repair only for what provisioning actually fixes', () => {
    for (const cls of [
      'webhook_missing',
      'webhook_inactive',
      'webhook_events_missing',
      'label_missing',
    ]) {
      expect(isRepairable(finding(cls)), cls).toBe(true);
    }
  });

  it('does not claim to fix a commit or a setting only the owner may change', () => {
    // A stale workflow is a commit to the repository. Branch protection
    // decides who may merge, and provisioning it behind the owner's back
    // would be the module widening its own authority.
    for (const cls of [
      'stale_workflow',
      'protection_missing',
      'protection_context_missing',
      'binding_invalid',
      'webhook_misdirected',
      'check_failed',
      'check_unsupported',
    ]) {
      expect(isRepairable(finding(cls)), cls).toBe(false);
    }
  });

  it('does not offer a repair for a class it has never heard of', () => {
    // Fail closed: a class added upstream must not be silently promised a fix.
    expect(isRepairable(finding('some_future_class'))).toBe(false);
  });
});

describe('hasRepairableFindings', () => {
  it('is false for a clean report and for a null one', () => {
    expect(hasRepairableFindings(report())).toBe(false);
    expect(hasRepairableFindings(null)).toBe(false);
  });

  it('is false when nothing provisioning does would change', () => {
    const r = report({
      conformant: false,
      findings: [finding('stale_workflow'), finding('protection_context_missing')],
    });
    expect(hasRepairableFindings(r)).toBe(false);
  });

  it('is true as soon as one finding is repairable', () => {
    const r = report({
      conformant: false,
      findings: [finding('stale_workflow'), finding('label_missing')],
    });
    expect(hasRepairableFindings(r)).toBe(true);
  });
});

describe('conformanceSummary', () => {
  it('names the count, because "not conformant" alone is useless', () => {
    expect(conformanceSummary(report({ conformant: false, findings: [finding('label_missing')] })))
      .toBe('Not conformant — 1 problem');
    expect(
      conformanceSummary(
        report({ conformant: false, findings: [finding('label_missing'), finding('stale_workflow')] }),
      ),
    ).toBe('Not conformant — 2 problems');
  });

  it('says so plainly when the repository is driveable', () => {
    expect(conformanceSummary(report())).toContain('Onboarded');
  });

  it('calls out a non-conformant report that named no reason', () => {
    // The server contract says every non-conformant report carries findings.
    // Rendering an empty panel would look like a pass.
    expect(conformanceSummary(report({ conformant: false, findings: [] }))).toContain('defect');
  });

  it('distinguishes "not checked" from "conformant"', () => {
    expect(conformanceSummary(null)).toBe('Not checked');
  });
});

describe('checkStatusClass', () => {
  it('is neutral for a check the platform cannot answer', () => {
    // Amber would send somebody looking for a fault that is a configuration.
    expect(checkStatusClass('unsupported')).toBe('badge-ghost');
    expect(checkStatusClass('not_applicable')).toBe('badge-ghost');
  });

  it('separates a failed check from a check that could not run', () => {
    expect(checkStatusClass('fail')).toBe('badge-error');
    expect(checkStatusClass('error')).toBe('badge-warning');
    expect(checkStatusClass('pass')).toBe('badge-success');
  });

  it('is neutral for a status it does not know', () => {
    expect(checkStatusClass('something-new')).toBe('badge-ghost');
  });
});

describe('conformanceBadgeClass', () => {
  it('reflects the report as a whole and stays neutral without one', () => {
    expect(conformanceBadgeClass(report())).toBe('badge-success');
    expect(conformanceBadgeClass(report({ conformant: false }))).toBe('badge-error');
    expect(conformanceBadgeClass(null)).toBe('badge-ghost');
  });
});

describe('conformanceAvailabilityFor', () => {
  it('hides the panel only where the module is not running', () => {
    expect(conformanceAvailabilityFor({ status: 503 })).toBe('absent');
    expect(conformanceAvailabilityFor({ status: 501 })).toBe('absent');
  });

  it('reports everything else, rather than looking like an absent module', () => {
    expect(conformanceAvailabilityFor({ status: 403 })).toBe('error');
    expect(conformanceAvailabilityFor({ status: 500 })).toBe('error');
    expect(conformanceAvailabilityFor(new Error('network'))).toBe('error');
    expect(conformanceAvailabilityFor(null)).toBe('error');
  });
});
