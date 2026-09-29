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

import type { ForgeUser, ProjectBinding, ReviewerConfig } from '@quicbaas/sdk';
import { formatAgentRef, parseAgentRef } from '@quicbaas/sdk';

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

/**
 * ReviewerProfileRow is one reviewer profile as the section edits it. The
 * list's FIRST row is the project's default reviewer; the rest are the
 * alternates an operator may choose per pull request.
 *
 * `countsTowardMerge` is the RESOLVED role, never "unset": the section shows
 * what a profile does, and bindingFrom writes the flag back only where it
 * differs from the role's default — so moving an advisory alternate into the
 * default slot keeps it advisory instead of silently promoting it.
 */
export interface ReviewerProfileRow {
  name: string;
  /** The ID of the Agent that reviews; its runtime, model and image live on the Agent. */
  agent: string;
  login: string;
  tokenSecretRef: string;
  statusContext: string;
  /** The local model server an opencode reviewer on ollama/<name> runs against. */
  endpoint: string;
  /**
   * The fleet node the reviewer is pinned to. Carried through a save so the
   * dialog never erases it; it is edited by the fleet-node picker, not typed.
   */
  hostAffinity: string;
  /** Prompt material appended after the reviewer charter for every review this seat runs. */
  promptAppend: string;
  countsTowardMerge: boolean;
}

/** DEFAULT_REVIEWER_PROFILE is the name the default answers to when it has none. */
export const DEFAULT_REVIEWER_PROFILE = 'default';

/** REVIEW_STATUS_CONTEXT is the one status context branch protection requires. */
export const REVIEW_STATUS_CONTEXT = 'quicbaas/review';

/** PROFILE_NAME matches the module's profile-name vocabulary. */
const PROFILE_NAME = /^[a-z0-9][a-z0-9._-]{0,63}$/;

/** emptyReviewerProfile is what "Add profile" starts from: an advisory alternate. */
export function emptyReviewerProfile(): ReviewerProfileRow {
  return {
    name: '',
    agent: '',
    login: '',
    tokenSecretRef: '',
    statusContext: '',
    endpoint: '',
    hostAffinity: '',
    promptAppend: '',
    countsTowardMerge: false,
  };
}

/** profileRowFrom reads one wire profile, resolving its role's default. */
function profileRowFrom(config: ReviewerConfig, isDefault: boolean): ReviewerProfileRow {
  return {
    name: config.name ?? '',
    agent: config.agent_id ?? '',
    login: config.login ?? '',
    tokenSecretRef: config.token_secret_ref ?? '',
    statusContext: config.status_context ?? '',
    endpoint: config.endpoint ?? '',
    hostAffinity: config.host_affinity ?? '',
    promptAppend: config.prompt_append ?? '',
    countsTowardMerge: config.counts_toward_merge ?? isDefault,
  };
}

/** profileFrom writes one row back, keeping the flag only where it matters. */
function profileFrom(row: ReviewerProfileRow, isDefault: boolean): ReviewerConfig {
  const config: ReviewerConfig = {
    name: row.name.trim() || undefined,
    agent_id: row.agent.trim(),
    login: row.login.trim(),
    token_secret_ref: row.tokenSecretRef.trim(),
    status_context: row.statusContext.trim() || undefined,
    endpoint: row.endpoint.trim() || undefined,
    host_affinity: row.hostAffinity.trim() || undefined,
    prompt_append: row.promptAppend.trim() !== '' ? row.promptAppend : undefined,
  };
  if (row.countsTowardMerge !== isDefault) {
    config.counts_toward_merge = row.countsTowardMerge;
  }
  return config;
}

/** profileName is the name a row answers to. */
export function profileName(row: ReviewerProfileRow, index: number): string {
  const name = row.name.trim();
  if (name !== '') return name;
  return index === 0 ? DEFAULT_REVIEWER_PROFILE : '';
}

/**
 * profileStatusContext is the commit status a row publishes under: the one
 * required context when it counts toward merge, its own when it is advisory.
 */
export function profileStatusContext(rows: ReviewerProfileRow[], index: number): string {
  const row = rows[index];
  if (!row) return '';
  const defaultRow = rows[0];
  const required =
    defaultRow && defaultRow.countsTowardMerge
      ? defaultRow.statusContext.trim() || REVIEW_STATUS_CONTEXT
      : REVIEW_STATUS_CONTEXT;
  if (row.countsTowardMerge) return required;
  return row.statusContext.trim() || `${REVIEW_STATUS_CONTEXT}/${profileName(row, index)}`;
}

/**
 * validateReviewerProfiles returns the first reason the profiles cannot be
 * saved, or the empty string. It mirrors pkg/sdlc/reviewer_profiles.go: every
 * profile is its own forge identity, so names, logins and tokens are unique.
 */
export function validateReviewerProfiles(rows: ReviewerProfileRow[]): string {
  const names = new Set<string>();
  const logins = new Set<string>();
  const tokens = new Set<string>();
  for (const [i, row] of rows.entries()) {
    const which = i === 0 ? 'The default reviewer' : `Reviewer profile ${i + 1}`;
    const name = profileName(row, i);
    if (name === '') return `${which} needs a name; an override chooses a profile by its name.`;
    if (!PROFILE_NAME.test(name)) {
      return `${which}: "${name}" must be lowercase letters, digits, ".", "_" or "-".`;
    }
    if (row.agent.trim() === '') return `${which} needs an Agent; a profile with no Agent reviews nothing.`;
    if (row.login.trim() === '') {
      return `${which} needs a forge login; it is how its own verdicts are recognised.`;
    }
    if (row.tokenSecretRef.trim() === '') {
      return `${which} needs its own token secret; a reviewer using the author's token approves its own change.`;
    }
    const login = row.login.trim().toLowerCase();
    const token = row.tokenSecretRef.trim();
    const context = row.statusContext.trim();
    if (i > 0 && row.countsTowardMerge && context !== '' && context !== profileStatusContext(rows, i)) {
      return `${which} counts toward merge, so it publishes under ${profileStatusContext(rows, i)}; clear its status context.`;
    }
    if (!row.countsTowardMerge && profileStatusContext(rows, i) === profileStatusContext(rows, 0) && rows[0]?.countsTowardMerge) {
      return `${which} is advisory and must not publish under the required context ${profileStatusContext(rows, 0)}.`;
    }
    if (names.has(name)) return `Two reviewer profiles are named "${name}".`;
    if (logins.has(login)) return `Two reviewer profiles post as "${row.login.trim()}"; each needs its own login.`;
    if (tokens.has(token)) return `Two reviewer profiles share the token secret "${token}"; each needs its own.`;
    names.add(name);
    logins.add(login);
    tokens.add(token);
  }
  return '';
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
  /**
   * worker is the Agent or pool that writes the code, as "agent:<id>",
   * "pool:<id>" or a bare Agent ID.
   */
  worker: string;
  /** Prompt material appended after the worker charter for every worker session. */
  workerPromptAppend: string;
  /** Prompt material appended after the triage charter for every triage session. */
  triagePromptAppend: string;
  verification: string[];
  requiredApprovals: number;
  requireIndependentReviewer: boolean;
  requireGreenChecks: boolean;
  checksSettleWindowSeconds: number;
  reviewers: ReviewerRow[];
  /** The reviewer profiles; the first is the project's default. */
  reviewerProfiles: ReviewerProfileRow[];
  maxReviewRounds: number;
  rankingDefaultPriority: number;
  rankingAnalyzerName: string;
  rankingDeclaredDependents: string;
  /**
   * rulers are the forge logins, beyond the repository's owner and the node's
   * configured rulers, whose comment on a parked issue is an owner ruling.
   * Picked from the repository's collaborators, never typed.
   */
  rulers: string[];
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
    worker: '',
    workerPromptAppend: '',
    triagePromptAppend: '',
    verification: [],
    requiredApprovals: 1,
    requireIndependentReviewer: true,
    requireGreenChecks: true,
    checksSettleWindowSeconds: 0,
    reviewers: [],
    reviewerProfiles: [],
    maxReviewRounds: 3,
    rankingDefaultPriority: 0,
    rankingAnalyzerName: '',
    rankingDeclaredDependents: '',
    rulers: [],
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
    worker: formatAgentRef(binding.worker),
    workerPromptAppend: binding.worker_prompt_append ?? '',
    triagePromptAppend: binding.triage_prompt_append ?? '',
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
    reviewerProfiles: reviewerProfilesFrom(binding),
    maxReviewRounds: binding.max_review_rounds ?? empty.maxReviewRounds,
    rankingDefaultPriority: binding.ranking?.default_priority ?? 0,
    rankingAnalyzerName: binding.ranking?.analyzer_name ?? '',
    rankingDeclaredDependents: (binding.ranking?.declared_dependents ?? []).join(', '),
    rulers: [...(binding.rulers ?? [])],
  };
}

/** reviewerProfilesFrom reads the default and the alternates, default first. */
function reviewerProfilesFrom(binding: ProjectBinding): ReviewerProfileRow[] {
  const rows: ReviewerProfileRow[] = [];
  if ((binding.reviewer?.agent_id ?? '').trim() !== '') {
    rows.push(profileRowFrom(binding.reviewer ?? {}, true));
  }
  for (const alternate of binding.reviewers ?? []) {
    rows.push(profileRowFrom(alternate, false));
  }
  return rows;
}

/** PROMPT_APPEND_MAX_BYTES mirrors orchestration.MaxPromptAppendBytes. */
export const PROMPT_APPEND_MAX_BYTES = 64 * 1024;

/**
 * promptAppendTooLarge reports whether appended prompt material exceeds the
 * server's bound, measured as the server measures it: UTF-8 bytes.
 */
export function promptAppendTooLarge(text: string): boolean {
  return new TextEncoder().encode(text).length > PROMPT_APPEND_MAX_BYTES;
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
  if (form.worker.trim() === '') {
    return 'A worker Agent or pool is required; autonomous development has no worker to spawn without one.';
  }
  try {
    parseAgentRef(form.worker);
  } catch {
    return 'The worker is agent:<id>, pool:<id> or an Agent ID.';
  }
  if (promptAppendTooLarge(form.workerPromptAppend)) {
    return "The worker's appended prompt is over 64 KiB.";
  }
  if (promptAppendTooLarge(form.triagePromptAppend)) {
    return "Triage's appended prompt is over 64 KiB.";
  }
  if (form.reviewerProfiles.some((row) => promptAppendTooLarge(row.promptAppend))) {
    return "A reviewer profile's appended prompt is over 64 KiB.";
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
  const rulers = validateRulers(form.rulers);
  if (rulers !== '') return rulers;
  return validateReviewerProfiles(form.reviewerProfiles);
}

/**
 * FORGE_LOGIN is the shape of a forge login, mirroring pkg/sdlc/ruling.go:
 * letters, digits, ".", "_" and "-", starting and ending with a letter or
 * digit, at most 40 characters.
 */
const FORGE_LOGIN = /^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,38}[A-Za-z0-9])?$/;

/**
 * validateRulers returns the first reason the rulers cannot be saved, or the
 * empty string. Whether each has access to the repository is the server's
 * check at bind time; this refuses what could never be a login.
 */
export function validateRulers(rulers: string[]): string {
  const seen = new Set<string>();
  for (const ruler of rulers) {
    if (!FORGE_LOGIN.test(ruler)) return `"${ruler}" is not a forge login.`;
    const key = ruler.toLowerCase();
    if (seen.has(key)) return `The ruler "${ruler}" is listed twice.`;
    seen.add(key);
  }
  return '';
}

/**
 * rulerCandidates are the collaborators a ruler can still be picked from: not
 * the owner, who always rules, and not one already picked.
 */
export function rulerCandidates(collaborators: ForgeUser[], owner: string, chosen: string[]): ForgeUser[] {
  const taken = new Set([owner.toLowerCase(), ...chosen.map((login) => login.toLowerCase())]);
  return collaborators.filter((user) => !taken.has(user.login.toLowerCase()));
}

/**
 * rulerHasAccess reports whether a saved ruler is still the owner or a
 * collaborator. A ruler who lost access is shown flagged: the next bind
 * refuses it, and the operator should see why before saving.
 */
export function rulerHasAccess(login: string, owner: string, collaborators: ForgeUser[]): boolean {
  const key = login.toLowerCase();
  return key === owner.toLowerCase() || collaborators.some((user) => user.login.toLowerCase() === key);
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

/**
 * bindingFrom builds the binding the module is asked to store.
 *
 * A BIND REPLACES THE WHOLE RECORD, so `base` — the binding as it was read —
 * is carried through underneath the edit: every field this section does not
 * edit (discovery, triage, readiness, onboarding) goes
 * back exactly as it came. Without it, saving the dialog erased them, which
 * is the bug class that once silently removed a project's reviewer.
 */
export function bindingFrom(projectID: string, form: BindingFormState, base?: ProjectBinding): ProjectBinding {
  const declaredDependents = form.rankingDeclaredDependents
    .split(',')
    .map((name) => name.trim())
    .filter((name) => name !== '');
  const [defaultRow, ...alternateRows] = form.reviewerProfiles;
  return {
    ...base,
    reviewer: defaultRow ? profileFrom(defaultRow, true) : undefined,
    reviewers:
      alternateRows.length > 0 ? alternateRows.map((row) => profileFrom(row, false)) : undefined,
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
    worker: parseAgentRef(form.worker),
    worker_prompt_append: form.workerPromptAppend.trim() !== '' ? form.workerPromptAppend : undefined,
    triage_prompt_append: form.triagePromptAppend.trim() !== '' ? form.triagePromptAppend : undefined,
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
    rulers: form.rulers.length > 0 ? [...form.rulers] : undefined,
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
