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
 * The Projects create/edit form: one shape, one validation, one pair of
 * request builders.
 *
 * The create and edit dialogs render the SAME fields, so the mapping between
 * a `Project` record and the controls that edit it lives here rather than
 * twice in the view. Keeping it out of the component is also what makes the
 * "does this field round-trip?" question answerable by a plain unit test.
 *
 * ONE SHAPE, THE SERVER'S. The record and the requests are both flat
 * (`git_author_name`, `git_signing_key_scope`, …) because that is what every
 * transport serializes. The form used to hydrate from a nested mirror
 * (`git.author.name`) the API has never produced, so an existing project's Git
 * settings opened blank while the write path sent the flat keys correctly —
 * go-quicbaas#797. Read and write now name the same keys.
 */

import type {
  ContextRepo,
  CreateProjectRequest,
  Project,
  UpdateProjectRequest,
} from '@quicbaas/sdk';

/** SIGNING_ALGORITHMS are the algorithms the KMS serves for commit signing. */
export const SIGNING_ALGORITHMS = ['ED25519', 'ECDSA-P256'] as const;

/** KEY_SCOPES are the key store scopes a KMS key reference may name. */
export const KEY_SCOPES = ['', 'user', 'tenant', 'service'] as const;

/** ContextRepoRow is one editable context-repository row. */
export interface ContextRepoRow {
  name: string;
  repo_url: string;
  ref: string;
}

/**
 * ProjectFormState is every project field the dialogs edit. Numbers use 0 for
 * "unlimited" because that is what the server means by an absent quota.
 */
export interface ProjectFormState {
  name: string;
  description: string;
  workspacePath: string;
  gitRepoUrl: string;
  gitBranch: string;
  gitAuthorName: string;
  gitAuthorEmail: string;
  gitSigningEnabled: boolean;
  gitSigningAlgorithm: string;
  gitSigningKeyName: string;
  gitSigningKeyScope: string;
  gitSigningKeyOwner: string;
  gitSshKeyName: string;
  gitSshKeyScope: string;
  gitSshKeyOwner: string;
  defaultWorkflow: string;
  hostAffinity: string;
  tokenBudget: number;
  gpuMinutes: number;
  maxConcurrency: number;
  autonomous: boolean;
  contextRepos: ContextRepoRow[];
}

/** emptyProjectForm is the state a fresh create dialog opens with. */
export function emptyProjectForm(): ProjectFormState {
  return {
    name: '',
    description: '',
    workspacePath: '',
    gitRepoUrl: '',
    gitBranch: '',
    gitAuthorName: '',
    gitAuthorEmail: '',
    gitSigningEnabled: false,
    gitSigningAlgorithm: '',
    gitSigningKeyName: '',
    gitSigningKeyScope: '',
    gitSigningKeyOwner: '',
    gitSshKeyName: '',
    gitSshKeyScope: '',
    gitSshKeyOwner: '',
    defaultWorkflow: '',
    hostAffinity: '',
    tokenBudget: 0,
    gpuMinutes: 0,
    maxConcurrency: 0,
    autonomous: false,
    contextRepos: [],
  };
}

/** projectFormFrom fills the form from an existing project record. */
export function projectFormFrom(project: Project): ProjectFormState {
  return {
    name: project.name,
    description: project.description ?? '',
    workspacePath: project.workspace_path ?? '',
    gitRepoUrl: project.git_repo_url ?? '',
    gitBranch: project.git_default_branch ?? '',
    gitAuthorName: project.git_author_name ?? '',
    gitAuthorEmail: project.git_author_email ?? '',
    gitSigningEnabled: project.git_signing_enabled ?? false,
    gitSigningAlgorithm: project.git_signing_algorithm ?? '',
    gitSigningKeyName: project.git_signing_key_name ?? '',
    gitSigningKeyScope: project.git_signing_key_scope ?? '',
    gitSigningKeyOwner: project.git_signing_key_owner ?? '',
    gitSshKeyName: project.git_ssh_key_name ?? '',
    gitSshKeyScope: project.git_ssh_key_scope ?? '',
    gitSshKeyOwner: project.git_ssh_key_owner ?? '',
    defaultWorkflow: project.default_workflow ?? '',
    hostAffinity: project.host_affinity ?? '',
    tokenBudget: project.token_budget ?? 0,
    gpuMinutes: project.gpu_minutes ?? 0,
    maxConcurrency: project.max_concurrency ?? 0,
    autonomous: project.autonomous ?? false,
    contextRepos: (project.context_repos ?? []).map((repo) => ({
      name: repo.name,
      repo_url: repo.repo_url,
      ref: repo.ref,
    })),
  };
}

/** emptyContextRepoRow is the row an "Add repository" click appends. */
export function emptyContextRepoRow(): ContextRepoRow {
  return { name: '', repo_url: '', ref: '' };
}

/**
 * validateProjectForm returns the first reason the form cannot be submitted,
 * or the empty string when it can. The rules mirror the server's own
 * fail-closed validation (pkg/orchestration/project_git.go,
 * ValidateContextRepos) so a refusal is shown before the round trip rather
 * than after it.
 */
export function validateProjectForm(form: ProjectFormState, requireName: boolean): string {
  if (requireName && form.name.trim() === '') {
    return 'Project name is required.';
  }
  const authorName = form.gitAuthorName.trim();
  const authorEmail = form.gitAuthorEmail.trim();
  if (authorName !== '' && authorEmail === '') {
    return 'A commit author needs an email as well as a name.';
  }
  if (authorEmail !== '' && authorName === '') {
    return 'A commit author needs a name as well as an email.';
  }
  if (form.gitSigningEnabled && form.gitSigningKeyName.trim() === '') {
    return 'Signing is enabled but names no key; an enabled policy must never degrade to unsigned.';
  }
  return validateContextRepos(form.contextRepos);
}

/**
 * validateContextRepos refuses a half-filled or duplicated row. A row with a
 * name and no URL would have the worker clone nothing under a directory the
 * charter told it to read.
 */
function validateContextRepos(rows: ContextRepoRow[]): string {
  const seen = new Set<string>();
  for (const row of rows) {
    const name = row.name.trim();
    const url = row.repo_url.trim();
    const ref = row.ref.trim();
    if (name === '' && url === '' && ref === '') {
      return 'A context repository row is empty; remove it or fill it in.';
    }
    if (name === '') {
      return 'Every context repository needs a directory name.';
    }
    if (name.includes('/')) {
      return `Context repository "${name}" must be a single path segment.`;
    }
    if (url === '') {
      return `Context repository "${name}" needs a repository URL.`;
    }
    if (ref === '') {
      return `Context repository "${name}" needs a ref to check out.`;
    }
    if (seen.has(name)) {
      return `Context repository "${name}" is listed twice.`;
    }
    seen.add(name);
  }
  return '';
}

/** trimmedContextRepos is the wire form of the rows. */
function trimmedContextRepos(rows: ContextRepoRow[]): ContextRepo[] {
  return rows.map((row) => ({
    name: row.name.trim(),
    repo_url: row.repo_url.trim(),
    ref: row.ref.trim(),
  }));
}

/**
 * positiveOrUndefined treats 0 (and any non-positive value) as "unlimited" —
 * the field is omitted from the request entirely rather than sent as an
 * explicit 0.
 */
export function positiveOrUndefined(value: number): number | undefined {
  return value > 0 ? value : undefined;
}

/** textOrUndefined omits an empty field rather than sending an empty string. */
function textOrUndefined(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed === '' ? undefined : trimmed;
}

/** createProjectRequestFrom builds the create payload from the form. */
export function createProjectRequestFrom(
  tenantID: string,
  form: ProjectFormState,
): CreateProjectRequest {
  const req: CreateProjectRequest = {
    tenant_id: tenantID,
    name: form.name.trim(),
    description: textOrUndefined(form.description),
    workspace_path: textOrUndefined(form.workspacePath),
    git_repo_url: textOrUndefined(form.gitRepoUrl),
    git_default_branch: textOrUndefined(form.gitBranch),
    git_author_name: textOrUndefined(form.gitAuthorName),
    git_author_email: textOrUndefined(form.gitAuthorEmail),
    git_signing_algorithm: textOrUndefined(form.gitSigningAlgorithm),
    git_signing_key_name: textOrUndefined(form.gitSigningKeyName),
    git_signing_key_scope: textOrUndefined(form.gitSigningKeyScope),
    git_signing_key_owner: textOrUndefined(form.gitSigningKeyOwner),
    git_ssh_key_name: textOrUndefined(form.gitSshKeyName),
    git_ssh_key_scope: textOrUndefined(form.gitSshKeyScope),
    git_ssh_key_owner: textOrUndefined(form.gitSshKeyOwner),
    default_workflow: textOrUndefined(form.defaultWorkflow),
    host_affinity: textOrUndefined(form.hostAffinity),
    token_budget: positiveOrUndefined(form.tokenBudget),
    gpu_minutes: positiveOrUndefined(form.gpuMinutes),
    max_concurrency: positiveOrUndefined(form.maxConcurrency),
  };
  // Both switches are omitted when off: the server's zero value is already
  // "off", and sending it would be indistinguishable from asking for it.
  if (form.gitSigningEnabled) req.git_signing_enabled = true;
  if (form.autonomous) req.autonomous = true;
  if (form.contextRepos.length > 0) req.context_repos = trimmedContextRepos(form.contextRepos);
  return req;
}

/** contextReposEqual compares a row list against a project's recorded list. */
function contextReposEqual(rows: ContextRepoRow[], recorded: ContextRepo[]): boolean {
  if (rows.length !== recorded.length) return false;
  return rows.every((row, i) => {
    const was = recorded[i];
    return (
      was !== undefined &&
      row.name.trim() === was.name &&
      row.repo_url.trim() === was.repo_url &&
      row.ref.trim() === was.ref
    );
  });
}

/**
 * updateProjectRequestFrom builds the update payload, sending only what the
 * operator actually changed.
 *
 * Update is presence-based: an absent field is "leave it alone". A field the
 * operator EMPTIED is therefore only clearable where the server offers an
 * explicit clear_* flag (description, default_workflow, context_repos); for
 * the rest, emptying a box leaves the stored value in place, which is what
 * the server's own semantics allow and what the dialog's help text says.
 */
export function updateProjectRequestFrom(
  tenantID: string,
  project: Project,
  form: ProjectFormState,
): UpdateProjectRequest {
  const was = projectFormFrom(project);
  const req: UpdateProjectRequest = { tenant_id: tenantID, project_id: project.id };

  const description = form.description.trim();
  if (description !== was.description) {
    if (description === '') {
      req.clear_description = true;
    } else {
      req.description = description;
    }
  }

  const defaultWorkflow = form.defaultWorkflow.trim();
  if (defaultWorkflow !== was.defaultWorkflow) {
    if (defaultWorkflow === '') {
      req.clear_default_workflow = true;
    } else {
      req.default_workflow = defaultWorkflow;
    }
  }

  setIfChanged(req, 'workspace_path', form.workspacePath, was.workspacePath);
  setIfChanged(req, 'git_repo_url', form.gitRepoUrl, was.gitRepoUrl);
  setIfChanged(req, 'git_default_branch', form.gitBranch, was.gitBranch);
  setIfChanged(req, 'git_author_name', form.gitAuthorName, was.gitAuthorName);
  setIfChanged(req, 'git_author_email', form.gitAuthorEmail, was.gitAuthorEmail);
  setIfChanged(req, 'git_signing_algorithm', form.gitSigningAlgorithm, was.gitSigningAlgorithm);
  setIfChanged(req, 'git_signing_key_name', form.gitSigningKeyName, was.gitSigningKeyName);
  setIfChanged(req, 'git_signing_key_scope', form.gitSigningKeyScope, was.gitSigningKeyScope);
  setIfChanged(req, 'git_signing_key_owner', form.gitSigningKeyOwner, was.gitSigningKeyOwner);
  setIfChanged(req, 'git_ssh_key_name', form.gitSshKeyName, was.gitSshKeyName);
  setIfChanged(req, 'git_ssh_key_scope', form.gitSshKeyScope, was.gitSshKeyScope);
  setIfChanged(req, 'git_ssh_key_owner', form.gitSshKeyOwner, was.gitSshKeyOwner);
  setIfChanged(req, 'host_affinity', form.hostAffinity, was.hostAffinity);

  numberIfChanged(req, 'token_budget', form.tokenBudget, was.tokenBudget);
  numberIfChanged(req, 'gpu_minutes', form.gpuMinutes, was.gpuMinutes);
  numberIfChanged(req, 'max_concurrency', form.maxConcurrency, was.maxConcurrency);

  // Both switches are sent whenever they moved, ON OR OFF: the server reads
  // them as pointers precisely so "turn it off" is expressible.
  if (form.gitSigningEnabled !== was.gitSigningEnabled) {
    req.git_signing_enabled = form.gitSigningEnabled;
  }
  if (form.autonomous !== was.autonomous) {
    req.autonomous = form.autonomous;
  }

  if (!contextReposEqual(form.contextRepos, project.context_repos ?? [])) {
    if (form.contextRepos.length === 0) {
      req.clear_context_repos = true;
    } else {
      req.context_repos = trimmedContextRepos(form.contextRepos);
    }
  }

  return req;
}

/** StringUpdateField names an UpdateProjectRequest field carrying a string. */
type StringUpdateField = {
  [K in keyof UpdateProjectRequest]-?: UpdateProjectRequest[K] extends string | undefined ? K : never;
}[keyof UpdateProjectRequest];

/** NumberUpdateField names an UpdateProjectRequest field carrying a number. */
type NumberUpdateField = {
  [K in keyof UpdateProjectRequest]-?: UpdateProjectRequest[K] extends number | undefined ? K : never;
}[keyof UpdateProjectRequest];

/**
 * setIfChanged sets a string field only when the operator edited it to a
 * NON-EMPTY value.
 *
 * An emptied box is dropped, not sent as a clear, because the server has no
 * clear for these fields: update is presence-based and offers explicit
 * clear_* flags only for description, default_workflow and context_repos.
 * Sending "" would either be ignored or, worse, stored as an empty string.
 *
 * The dialog says so in as many words (ProjectFormFields' editing note); it
 * used to imply the opposite for the SSH access key, so an operator could
 * clear a stale key, save without error, and keep using the old one.
 */
function setIfChanged(
  req: UpdateProjectRequest,
  field: StringUpdateField,
  next: string,
  original: string,
): void {
  const trimmed = next.trim();
  if (trimmed === '' || trimmed === original) return;
  req[field] = trimmed;
}

/**
 * numberIfChanged sets a quota only when the operator edited it — INCLUDING
 * to zero.
 *
 * Zero is a real value here: the dialog, the docs and the project record all
 * define a quota of 0 as UNLIMITED. Dropping it as "unset" meant an operator
 * could lift a cap, watch the save succeed, and stay capped. The server takes
 * these three as presence-based pointers for exactly this reason, so sending
 * 0 says "unlimited" and sending nothing says "leave it alone".
 *
 * A negative value is refused rather than sent: it is not a quota, and the
 * only way to reach one is a spinner the operator dragged below zero.
 */
function numberIfChanged(
  req: UpdateProjectRequest,
  field: NumberUpdateField,
  next: number,
  original: number,
): void {
  if (!Number.isFinite(next) || next < 0) return;
  const value = Math.floor(next);
  if (value === original) return;
  req[field] = value;
}
