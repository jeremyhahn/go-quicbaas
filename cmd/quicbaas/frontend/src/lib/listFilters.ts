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
 * The facets each orchestrator listing can be narrowed by — declared ONCE,
 * here, rather than in the view that happens to render the table.
 *
 * Three readers need the same vocabulary and would otherwise each invent one:
 * the list view, the detail view that links back into a narrowed list, and the
 * test that asserts a chip reaches the loader. The vocabularies below are the
 * server's own enumerations (pkg/orchestration), so a state added there is
 * added here and every surface grows the chip at once.
 *
 * THE VOCABULARY IS NOT THE COUNTS. What is behind each value comes from the
 * answer's `facets`, and a value nobody declared here still appears as a chip
 * when the server counts one (see fieldOptions in lib/filters.ts). These lists
 * decide what is OFFERED before anybody has counted anything, and the order it
 * is offered in.
 */

import { humanLabel, type FilterField, type FilterOption } from './filters.ts';

// ---------------------------------------------------------------------------
// Vocabularies
// ---------------------------------------------------------------------------

/** options turns a list of wire values into chips labelled for a human. */
function options(values: readonly string[]): FilterOption[] {
  return values.map((value) => ({ value, label: humanLabel(value) }));
}

/** The task lifecycle (pkg/orchestration/task.go). */
export const TASK_STATUSES: readonly string[] = [
  'pending',
  'claimed',
  'in_progress',
  'blocked',
  'completed',
  'failed',
  'cancelled',
];

/**
 * The job lifecycle, active states first — which is the order an operator
 * scans, because the question is nearly always about what is still running.
 *
 * It is SPELLED OUT rather than composed from the store's ACTIVE_JOB_STATES,
 * so this module stays free of the orchestrator store: every view test that
 * mocks that store would otherwise have to re-export a constant it has no
 * opinion about. `filters.test.ts` asserts the two agree, which is the check
 * the import was standing in for.
 */
export const JOB_STATES: readonly string[] = [
  'initializing',
  'decomposing',
  'scheduling',
  'assigning',
  'executing',
  'validating',
  'reviewing',
  're-delegating',
  'completing',
  'cleanup',
  'escalating',
  'completed',
  'failed',
];

/** The session lifecycle (pkg/orchestration/backend.go). */
export const SESSION_STATES: readonly string[] = ['active', 'idle', 'closed', 'error'];

/** The fleet's agent states. */
export const FLEET_NODE_STATUSES: readonly string[] = [
  'registered',
  'running',
  'idle',
  'offline',
  'error',
];

/** The job runners, labelled by the role they play. */
export const RUNNER_OPTIONS: readonly FilterOption[] = [
  { value: 'sdlc', label: 'worker' },
  { value: 'review', label: 'reviewer' },
  { value: 'triage', label: 'triage' },
];

/** A workflow's operational mode (WorkflowMode in the SDK's types). */
export const WORKFLOW_MODES: readonly string[] = ['canned', 'constrained', 'autonomous'];

/** Whether a task asked for immediate dispatch or waits to be claimed. */
export const DISPATCH_OPTIONS: readonly FilterOption[] = [
  { value: 'true', label: 'auto-spawned' },
  { value: 'false', label: 'queued' },
];

/** Whether a project dispatches its own work. */
export const AUTONOMY_OPTIONS: readonly FilterOption[] = [
  { value: 'true', label: 'autonomous' },
  { value: 'false', label: 'manual' },
];

/** A pooled provider account's state (AccountStatus in the SDK's types). */
export const ACCOUNT_STATUSES: readonly string[] = ['active', 'revoked', 'error'];

// ---------------------------------------------------------------------------
// The fields, per listing
// ---------------------------------------------------------------------------

/**
 * idField is a reference to another record — a project, an agent, a job.
 *
 * It is single-valued and rendered as a PICKER rather than chips, because the
 * vocabulary is the tenant's data and not an enumeration: a chip per project
 * is a thousand chips. The options are whatever the caller could resolve; a
 * caller that resolved none still gets a working field as soon as the server
 * counts some, because an uncounted, undeclared value is still selectable from
 * a link.
 */
export function idField<T>(
  key: string,
  label: string,
  param: string,
  choices: readonly FilterOption[],
  rowValue?: (row: T) => string | null | undefined,
): FilterField<T> {
  const field: FilterField<T> = {
    key,
    label,
    param,
    // THE SERVER COUNTS UNDER THE PARAMETER, NOT THE KEY. `project` is what
    // this field is called in a link; `project_id` is what the endpoint takes
    // and what its `facets` block is keyed by (docs/software-factory/api.md).
    // Without this the counts silently miss and every chip renders bare — the
    // field still works, which is exactly what would make it hard to notice.
    facetKey: param,
    options: choices,
    control: 'select',
  };
  if (rowValue !== undefined) field.rowValue = rowValue;
  return field;
}

/**
 * taskFilters is what the Tasks list narrows by. Status is multi-select
 * because the endpoint takes `statuses` and "pending or blocked" is the
 * question an operator actually asks; dispatch and project are single, because
 * the endpoint takes one of each.
 */
export function taskFilters<T>(
  projects: readonly FilterOption[],
  nodes: readonly FilterOption[],
): FilterField<T>[] {
  return [
    {
      key: 'status',
      label: 'Status',
      param: 'statuses',
      multi: true,
      options: options(TASK_STATUSES),
    },
    {
      key: 'dispatch',
      label: 'Dispatch',
      param: 'auto_spawn',
      options: DISPATCH_OPTIONS,
    },
    idField<T>('project', 'Project', 'project_id', projects),
    idField<T>('node', 'Fleet node', 'agent_id', nodes),
  ];
}

/** jobFilters is what the Jobs list narrows by. */
export function jobFilters<T>(
  projects: readonly FilterOption[],
  nodes: readonly FilterOption[],
): FilterField<T>[] {
  return [
    {
      key: 'state',
      label: 'State',
      param: 'statuses',
      multi: true,
      options: options(JOB_STATES),
    },
    {
      key: 'runner',
      label: 'Runner',
      param: 'runner',
      options: [...RUNNER_OPTIONS],
    },
    idField<T>('project', 'Project', 'project_id', projects),
    idField<T>('node', 'Fleet node', 'agent_id', nodes),
  ];
}

/** sessionFilters is what the Sessions list narrows by. */
export function sessionFilters<T>(
  backends: readonly FilterOption[],
  projects: readonly FilterOption[],
  nodes: readonly FilterOption[],
): FilterField<T>[] {
  return [
    {
      key: 'status',
      label: 'Status',
      param: 'status',
      // The sessions listing TAKES `status` and COUNTS under `state` — the
      // name the record itself uses. The two differ only here, which is what
      // facetKey exists for.
      facetKey: 'state',
      options: options(SESSION_STATES),
    },
    {
      key: 'backend',
      label: 'Backend',
      param: 'backend',
      options: backends,
    },
    {
      key: 'runner',
      label: 'Role',
      param: 'runner',
      options: [...RUNNER_OPTIONS],
    },
    idField<T>('project', 'Project', 'project_id', projects),
    idField<T>('node', 'Fleet node', 'agent_id', nodes),
  ];
}

/** workflowFilters is what the Workflows list narrows by. */
export function workflowFilters<T>(
  projects: readonly FilterOption[],
  mode: (row: T) => string | null | undefined,
): FilterField<T>[] {
  return [
    {
      key: 'mode',
      label: 'Mode',
      param: 'mode',
      options: options(WORKFLOW_MODES),
      rowValue: mode,
    },
    idField<T>('project', 'Project', 'project_id', projects),
  ];
}

/** fleetNodeFilters is what the fleet list narrows by. */
export function fleetNodeFilters<T>(
  status: (row: T) => string | null | undefined,
  backend: (row: T) => string | null | undefined,
  backends: readonly FilterOption[],
): FilterField<T>[] {
  return [
    {
      key: 'status',
      label: 'Status',
      param: 'status',
      options: options(FLEET_NODE_STATUSES),
      rowValue: status,
    },
    {
      key: 'backend',
      label: 'Backend',
      param: 'backend',
      options: backends,
      rowValue: backend,
    },
  ];
}

/** projectFilters is what the Projects list narrows by. */
export function projectFilters<T>(
  autonomous: (row: T) => string | null | undefined,
): FilterField<T>[] {
  return [
    {
      key: 'autonomous',
      label: 'Dispatch',
      param: 'autonomous',
      options: AUTONOMY_OPTIONS,
      rowValue: autonomous,
    },
  ];
}

/** accountFilters is what the Accounts list narrows by. */
export function accountFilters<T>(
  providers: readonly FilterOption[],
  provider: (row: T) => string | null | undefined,
  status: (row: T) => string | null | undefined,
): FilterField<T>[] {
  return [
    {
      key: 'status',
      label: 'Status',
      param: 'status',
      options: options(ACCOUNT_STATUSES),
      rowValue: status,
    },
    {
      key: 'provider',
      label: 'Provider',
      param: 'provider',
      options: providers,
      rowValue: provider,
    },
  ];
}
