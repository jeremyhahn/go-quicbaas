// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

/**
 * Onboarding conformance, as the Projects dialog presents it.
 *
 * The report answers "is this project correctly onboarded, and if not, what
 * is wrong". The presentation rules live here rather than in the component
 * for the usual reason — they are testable this way — and for one specific
 * to this surface: **which findings the UI may offer to repair is a
 * correctness question, not a styling one**. Provisioning fixes a missing
 * webhook and a missing label. It does not fix a workflow still committed to
 * the repository, and it deliberately does not create branch protection —
 * who may merge is the repository owner's policy, and a button that changed
 * it behind their back would be the UI widening the module's authority.
 *
 * Mirrors pkg/orchestration/sdlc/onboarding.go. Conflicts with that file are defects here.
 */

import type { ConformanceFinding, ConformanceReport } from '@quicbaas/sdk';

/** ConformanceState is what the dialog does about the report it has. */
export type ConformanceState = 'absent' | 'loading' | 'ready' | 'error';

/**
 * REPAIRABLE_CLASSES are the finding classes `onboard` actually fixes.
 *
 * Kept as a set rather than a negation so a class added upstream is NOT
 * offered a repair button by default: a button that claims to fix something
 * and does not is worse than no button, because the operator stops looking.
 */
export const REPAIRABLE_CLASSES: ReadonlySet<string> = new Set([
  'webhook_missing',
  'webhook_inactive',
  'webhook_events_missing',
  'label_missing',
]);

/** isRepairable reports whether `onboard` would fix this finding. */
export function isRepairable(finding: ConformanceFinding): boolean {
  return REPAIRABLE_CLASSES.has(finding.class);
}

/**
 * hasRepairableFindings decides whether the repair button is offered at all.
 * A report whose only complaints are a stale workflow and a protection rule
 * gets no button, because pressing it would change nothing and teach the
 * operator that the button is a lie.
 */
export function hasRepairableFindings(report: ConformanceReport | null): boolean {
  return (report?.findings ?? []).some(isRepairable);
}

/**
 * checkStatusClass maps a check's verdict to its badge.
 *
 * `unsupported` and `not_applicable` are NEUTRAL, not warnings: the platform
 * cannot answer, or the binding does not ask for the thing checked, and
 * colouring either amber would send somebody looking for a fault that is a
 * configuration.
 */
export function checkStatusClass(status: string): string {
  switch (status) {
    case 'pass':
      return 'badge-success';
    case 'fail':
      return 'badge-error';
    case 'error':
      return 'badge-warning';
    default:
      return 'badge-ghost';
  }
}

/** conformanceBadgeClass is the badge for the report as a whole. */
export function conformanceBadgeClass(report: ConformanceReport | null): string {
  if (!report) return 'badge-ghost';
  return report.conformant ? 'badge-success' : 'badge-error';
}

/**
 * conformanceSummary is the one line shown before anything is expanded.
 *
 * It names the COUNT, because "not conformant" alone is the useless answer
 * this whole surface exists to replace, and an operator scanning a fleet
 * needs to know whether a project has one problem or seven before deciding
 * where to look.
 */
export function conformanceSummary(report: ConformanceReport | null): string {
  if (!report) return 'Not checked';
  if (report.conformant) return 'Onboarded — QuicBaaS can drive this repository';
  const findings = report.findings ?? [];
  if (findings.length === 0) {
    // Non-conformant with nothing to show is a contract violation upstream;
    // say so rather than render an empty panel that looks like a pass.
    return 'Not conformant (the report named no reason — this is a defect)';
  }
  const noun = findings.length === 1 ? 'problem' : 'problems';
  return `Not conformant — ${findings.length} ${noun}`;
}

/**
 * conformanceAvailabilityFor maps a probe failure onto what the dialog does.
 *
 * A node that does not run the module (503) hides the panel; anything else is
 * an error the operator is told about, because a silently missing panel and a
 * silently failing one look identical and only one of them is fine.
 */
export function conformanceAvailabilityFor(err: unknown): ConformanceState {
  const status = (err as { status?: number } | null)?.status;
  if (status === 503 || status === 501) return 'absent';
  return 'error';
}
