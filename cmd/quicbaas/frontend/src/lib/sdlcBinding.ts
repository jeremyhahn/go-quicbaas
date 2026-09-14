// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
 * The SDLC binding as the Projects edit dialog edits it.
 *
 * The binding is a project's AUTONOMOUS DEVELOPMENT configuration: which
 * forge repository it delivers to, which SDLC it follows, which agent writes
 * the code, what verifies the work, and how its pull requests are judged.
 * It is the SDLC module's record, reached through the module's own
 * endpoints, and it exists only where the module is mounted.
 */

import type { ProjectBinding } from '@quicbaas/sdk';

/** SCM_TYPES are the forge platforms a binding may name. */
export const SCM_TYPES = ['gitea', 'github', 'gitlab'] as const;

/** ScmType is one of the forge platforms a binding may name. */
export type ScmType = (typeof SCM_TYPES)[number];

/**
 * SDLC_STRATEGIES are the registered SDLCs a project may follow. Each decides
 * branch name, base and target, whether a pull request is required, the merge
 * method and what follows a merge.
 */
export const SDLC_STRATEGIES = ['github-flow', 'trunk-based', 'gitflow'] as const;

/** LABEL_KEYS are the module's own label vocabulary, in display order. */
export const LABEL_KEYS = [
  'in_progress',
  'in_review',
  'merged',
  'blocked',
  'needs_info',
  'escalated',
] as const;

/** ReviewerRow maps one forge login to the model family behind it. */
export interface ReviewerRow {
  login: string;
  family: string;
}

/** LabelRow maps one module label key to the forge label displaying it. */
export interface LabelRow {
  key: string;
  label: string;
}

/** BindingFormState is every binding field the dialog edits. */
export interface BindingFormState {
  forgeType: string;
  baseUrl: string;
  owner: string;
  repo: string;
  triggerLabel: string;
  tokenSecretRef: string;
  webhookSecretRef: string;
  /**
   * sshHostKey pins the forge's SSH host public key: one OpenSSH public-key
   * line. Required when the project's repository URL is an ssh one — the
   * worker container verifies the forge against it instead of trusting
   * whatever answers.
   */
  sshHostKey: string;
  labels: LabelRow[];
  strategy: string;
  workerBackend: string;
  workerModel: string;
  verification: string[];
  requiredApprovals: number;
  requireIndependentReviewer: boolean;
  requireGreenChecks: boolean;
  checksSettleWindowSeconds: number;
  reviewers: ReviewerRow[];
  maxReviewRounds: number;
  rankingDefaultPriority: number;
  rankingAnalyzerName: string;
  rankingDeclaredDependents: string;
}

/** NANOS_PER_SECOND converts the wire's Go duration to the field's seconds. */
const NANOS_PER_SECOND = 1_000_000_000;

/**
 * emptyBindingForm is what an unbound project's section opens with: the
 * company's own defaults (github-flow, one independent approval, green
 * checks, three rounds) rather than a blank slate that would bind a project
 * to an unreviewed pipeline by accident.
 */
export function emptyBindingForm(): BindingFormState {
  return {
    forgeType: 'gitea',
    baseUrl: '',
    owner: '',
    repo: '',
    triggerLabel: 'task:ready',
    tokenSecretRef: '',
    webhookSecretRef: '',
    sshHostKey: '',
    labels: [],
    strategy: 'github-flow',
    workerBackend: '',
    workerModel: '',
    verification: [],
    requiredApprovals: 1,
    requireIndependentReviewer: true,
    requireGreenChecks: true,
    checksSettleWindowSeconds: 0,
    reviewers: [],
    maxReviewRounds: 3,
    rankingDefaultPriority: 0,
    rankingAnalyzerName: '',
    rankingDeclaredDependents: '',
  };
}

/** bindingFormFrom fills the form from the binding the module returned. */
export function bindingFormFrom(binding: ProjectBinding): BindingFormState {
  const empty = emptyBindingForm();
  const labels = binding.forge?.labels ?? {};
  const reviewers = binding.review?.reviewers ?? {};
  return {
    forgeType: binding.forge?.type || empty.forgeType,
    baseUrl: binding.forge?.base_url ?? '',
    owner: binding.forge?.owner ?? '',
    repo: binding.forge?.repo ?? '',
    triggerLabel: binding.forge?.trigger_label ?? '',
    tokenSecretRef: binding.forge?.token_secret_ref ?? '',
    webhookSecretRef: binding.forge?.webhook_secret_ref ?? '',
    sshHostKey: binding.forge?.ssh_host_key ?? '',
    labels: Object.keys(labels)
      .sort()
      .map((key) => ({ key, label: labels[key] ?? '' })),
    strategy: binding.strategy || empty.strategy,
    workerBackend: binding.worker?.backend ?? '',
    workerModel: binding.worker?.model ?? '',
    verification: [...(binding.verification ?? [])],
    requiredApprovals: binding.review?.required_approvals ?? empty.requiredApprovals,
    requireIndependentReviewer:
      binding.review?.require_independent_reviewer ?? empty.requireIndependentReviewer,
    requireGreenChecks: binding.review?.require_green_checks ?? empty.requireGreenChecks,
    checksSettleWindowSeconds: Math.round(
      (binding.review?.checks_settle_window ?? 0) / NANOS_PER_SECOND,
    ),
    reviewers: Object.keys(reviewers)
      .sort()
      .map((login) => ({ login, family: reviewers[login] ?? '' })),
    maxReviewRounds: binding.max_review_rounds ?? empty.maxReviewRounds,
    rankingDefaultPriority: binding.ranking?.default_priority ?? 0,
    rankingAnalyzerName: binding.ranking?.analyzer_name ?? '',
    rankingDeclaredDependents: (binding.ranking?.declared_dependents ?? []).join(', '),
  };
}

/**
 * validateBindingForm returns the first reason the binding cannot be saved,
 * or the empty string when it can. The rules mirror the module's own
 * fail-closed validation (pkg/sdlc/binding.go) so a refusal is visible before
 * the round trip.
 */
export function validateBindingForm(form: BindingFormState, availableForges: string[]): string {
  if (form.forgeType.trim() === '') {
    return 'Select the forge this project delivers to.';
  }
  if (availableForges.length > 0 && !availableForges.includes(form.forgeType)) {
    return `No ${form.forgeType} adapter is registered in this build.`;
  }
  if (form.baseUrl.trim() === '') return "The forge's API root is required.";
  if (form.owner.trim() === '' || form.repo.trim() === '') {
    return 'Name the repository owner and name.';
  }
  if (form.triggerLabel.trim() === '') {
    return 'A trigger label is required; without it no issue is ever handed to the module.';
  }
  if (form.tokenSecretRef.trim() === '') {
    return 'A token secret reference is required; an adapter with no token cannot read the forge.';
  }
  const trigger = form.triggerLabel.trim().toLowerCase();
  for (const row of form.labels) {
    if (row.label.trim().toLowerCase() === trigger) {
      return `Display label "${row.key}" collides with the trigger label.`;
    }
  }
  if (form.strategy.trim() === '') return 'Select the SDLC this project follows.';
  if (form.workerBackend.trim() === '') {
    return 'A worker backend is required; autonomous development has no worker to spawn without one.';
  }
  if (form.verification.some((command) => command.trim() === '')) {
    return 'A verification step must be a command; a blank step would pass without running anything.';
  }
  if (form.requiredApprovals < 1) {
    return 'Required approvals must be at least one; zero would merge unreviewed code.';
  }
  if (form.checksSettleWindowSeconds < 0) {
    return 'The checks settle window must not be negative.';
  }
  if (form.maxReviewRounds < 0) return 'Max review rounds must not be negative.';
  for (const row of form.reviewers) {
    if (row.login.trim() === '' || row.family.trim() === '') {
      return 'Every reviewer maps a non-empty login to a non-empty model family.';
    }
  }
  return '';
}

/** rowsToRecord folds key/value rows into the map the wire carries. */
function rowsToRecord(rows: { key: string; value: string }[]): Record<string, string> | undefined {
  const record: Record<string, string> = {};
  for (const row of rows) {
    const key = row.key.trim();
    if (key === '') continue;
    record[key] = row.value.trim();
  }
  return Object.keys(record).length > 0 ? record : undefined;
}

/** bindingFrom builds the binding the module is asked to store. */
export function bindingFrom(projectID: string, form: BindingFormState): ProjectBinding {
  const declaredDependents = form.rankingDeclaredDependents
    .split(',')
    .map((name) => name.trim())
    .filter((name) => name !== '');
  return {
    project_id: projectID,
    forge: {
      type: form.forgeType.trim(),
      base_url: form.baseUrl.trim(),
      owner: form.owner.trim(),
      repo: form.repo.trim(),
      trigger_label: form.triggerLabel.trim(),
      token_secret_ref: form.tokenSecretRef.trim() || undefined,
      webhook_secret_ref: form.webhookSecretRef.trim() || undefined,
      ssh_host_key: form.sshHostKey.trim() || undefined,
      labels: rowsToRecord(form.labels.map((row) => ({ key: row.key, value: row.label }))),
    },
    strategy: form.strategy.trim(),
    worker: {
      backend: form.workerBackend.trim(),
      model: form.workerModel.trim(),
    },
    verification: form.verification.map((command) => command.trim()).filter((c) => c !== ''),
    review: {
      required_approvals: form.requiredApprovals,
      require_independent_reviewer: form.requireIndependentReviewer,
      require_green_checks: form.requireGreenChecks,
      checks_settle_window:
        form.checksSettleWindowSeconds > 0
          ? form.checksSettleWindowSeconds * NANOS_PER_SECOND
          : undefined,
      reviewers: rowsToRecord(form.reviewers.map((row) => ({ key: row.login, value: row.family }))),
    },
    max_review_rounds: form.maxReviewRounds > 0 ? form.maxReviewRounds : undefined,
    ranking: {
      default_priority: form.rankingDefaultPriority !== 0 ? form.rankingDefaultPriority : undefined,
      analyzer_name: form.rankingAnalyzerName.trim() || undefined,
      declared_dependents: declaredDependents.length > 0 ? declaredDependents : undefined,
    },
  };
}

/**
 * BindingAvailability is what the binding probe learned. The three answers
 * are genuinely different and the dialog treats them differently: the module
 * is not on this node (503, section hidden), it is mounted and this project
 * is not bound (404, section shown unbound), or it is bound (200).
 */
export type BindingAvailability = 'absent' | 'unbound' | 'bound' | 'error';

/**
 * bindingAvailabilityFor maps a probe failure onto an availability. A 503 is
 * the module answering that it is not running on this node; a 404 is the
 * module answering that this project has no binding yet. Anything else is a
 * real error and is reported rather than swallowed.
 */
export function bindingAvailabilityFor(err: unknown): BindingAvailability {
  const status = (err as { status?: unknown } | null)?.status;
  if (status === 503 || status === 501) return 'absent';
  if (status === 404) return 'unbound';
  return 'error';
}
