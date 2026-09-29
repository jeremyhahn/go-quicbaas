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
 * The two halves of every filter field have to agree with the SERVER, and
 * they are different names: the field is SENT as `param` and COUNTED under
 * `facetKey`. The endpoint contract is fixed in
 * docs/software-factory/api.md — `?project_id=` narrows and
 * `facets.project_id` counts — while the field's own `key` is a UI name used
 * in links, and defaults to standing in for both.
 *
 * A DEFAULT THAT IS WRONG IS SILENT. A field whose facetKey misses renders
 * every chip bare: the filter still narrows, the table still shows the right
 * rows, and the only symptom is a missing number nobody can tell from a zero
 * that was never counted. That is what this file exists to catch, so the
 * expected pairs are written out rather than derived.
 */

import { describe, it, expect } from 'vitest';
import { apiParam, facetKey, type FilterField } from '../src/lib/filters.ts';
import {
  accountFilters,
  fleetNodeFilters,
  jobFilters,
  projectFilters,
  sessionFilters,
  taskFilters,
  workflowFilters,
} from '../src/lib/listFilters.ts';

/** pairs maps each field's key to what it sends and what it reads. */
function pairs(fields: FilterField<unknown>[]): Record<string, [string, string]> {
  const out: Record<string, [string, string]> = {};
  for (const field of fields) {
    out[field.key] = [apiParam(field), facetKey(field)];
  }
  return out;
}

const NONE: never[] = [];
const noValue = (): string | null => null;

describe('list filter fields against the server contract', () => {
  it('sends and counts the tasks listing under the endpoint\'s names', () => {
    expect(pairs(taskFilters(NONE, NONE))).toEqual({
      status: ['statuses', 'status'],
      dispatch: ['auto_spawn', 'dispatch'],
      project: ['project_id', 'project_id'],
      node: ['agent_id', 'agent_id'],
    });
  });

  it('sends and counts the jobs listing under the endpoint\'s names', () => {
    // The jobs listing counts its lifecycle under `state`, which is what the
    // record calls it — and the field is keyed `state` to match.
    expect(pairs(jobFilters(NONE, NONE))).toEqual({
      state: ['statuses', 'state'],
      runner: ['runner', 'runner'],
      project: ['project_id', 'project_id'],
      node: ['agent_id', 'agent_id'],
    });
  });

  it('sends the sessions status as `status` and reads it back as `state`', () => {
    // The one endpoint where the two names differ: it TAKES `?status=` and
    // COUNTS under `facets.state`.
    expect(pairs(sessionFilters(NONE, NONE, NONE))).toEqual({
      status: ['status', 'state'],
      backend: ['backend', 'backend'],
      runner: ['runner', 'runner'],
      project: ['project_id', 'project_id'],
      node: ['agent_id', 'agent_id'],
    });
  });

  it('sends and counts the fleet, workflows, projects and accounts listings', () => {
    expect(pairs(fleetNodeFilters(noValue, noValue, NONE))).toEqual({
      status: ['status', 'status'],
      backend: ['backend', 'backend'],
    });
    expect(pairs(workflowFilters(NONE, noValue))).toEqual({
      mode: ['mode', 'mode'],
      project: ['project_id', 'project_id'],
    });
    expect(pairs(projectFilters(noValue))).toEqual({
      autonomous: ['autonomous', 'autonomous'],
    });
    expect(pairs(accountFilters(NONE, noValue, noValue))).toEqual({
      status: ['status', 'status'],
      provider: ['provider', 'provider'],
    });
  });

  it('counts a reference field under its parameter, not its key', () => {
    // The regression this file was written for: `project` and `agent` are
    // link names, `project_id` and `agent_id` are what the answer counts.
    for (const fields of [taskFilters(NONE, NONE), jobFilters(NONE, NONE), sessionFilters(NONE, NONE, NONE)]) {
      for (const field of fields) {
        if (field.key !== 'project' && field.key !== 'node') continue;
        expect(facetKey(field)).toBe(apiParam(field));
      }
    }
  });
});
