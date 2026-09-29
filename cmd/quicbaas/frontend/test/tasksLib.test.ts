/**
 * tasksLib.test.ts
 *
 * The pure Task helpers (src/lib/tasks.ts): attempt accounting, provenance,
 * the parked-on-a-person state, and the sentence a failed read turns into.
 * Pinned directly because every Tasks view renders through them and a wrong
 * number here reads as a fact about the task.
 */

import { describe, it, expect } from 'vitest';
import {
  artifactUrl,
  blockedReason,
  budgetLabel,
  chargedAttempts,
  dispatchedAttempts,
  isWaitingOnPerson,
  issueRef,
  loadErrorMessage,
  NO_REASON_RECORDED,
  artifactBaseName,
  pullRequestArtifacts,
  taskAgentID,
  taskStopKind,
  taskStopPresentation,
} from '../src/lib/tasks.ts';

describe('issueRef', () => {
  it('resolves a forge issue with a bare number and a URL', () => {
    const ref = issueRef({ source: { kind: 'forge_issue', ref: '42', url: 'https://forge/o/r/issues/42' } });
    expect(ref).toEqual({ number: '42', label: '#42', href: 'https://forge/o/r/issues/42' });
  });

  it('takes the number after the hash of an owner/repo#N ref', () => {
    const ref = issueRef({ source: { kind: 'forge_issue', ref: 'owner/repo#12' } });
    expect(ref?.number).toBe('12');
    expect(ref?.label).toBe('#12');
    expect(ref?.href).toBe('');
  });

  it('returns null for any other provenance, a missing source, or an empty ref', () => {
    expect(issueRef({ source: { kind: 'operator', ref: 'alice' } })).toBeNull();
    expect(issueRef({ source: { kind: 'scheduler', ref: 'nightly' } })).toBeNull();
    expect(issueRef({})).toBeNull();
    expect(issueRef({ source: null })).toBeNull();
    expect(issueRef({ source: { kind: 'forge_issue', ref: '  ' } })).toBeNull();
    expect(issueRef({ source: { kind: 'forge_issue', ref: 'owner/repo#' } })).toBeNull();
  });
});

describe('attempt accounting', () => {
  it('charges dispatched attempts less environment refunds, never below zero', () => {
    expect(dispatchedAttempts({ attempts: 3 })).toBe(3);
    expect(chargedAttempts({ attempts: 3, environment_faults: 1 })).toBe(2);
    expect(chargedAttempts({ attempts: 1, environment_faults: 5 })).toBe(0);
    expect(chargedAttempts({})).toBe(0);
    expect(dispatchedAttempts({ attempts: -2 })).toBe(0);
  });

  it('renders "charged / budget (N dispatched)" only when a refund makes them differ', () => {
    expect(budgetLabel({ attempts: 3, attempt_budget: 5, environment_faults: 1 })).toBe('2 / 5 (3 dispatched)');
    expect(budgetLabel({ attempts: 2, attempt_budget: 5 })).toBe('2 / 5');
    expect(budgetLabel({ attempts: 0, attempt_budget: 5 })).toBe('0 / 5');
  });

  it('renders a bare count when there is no budget, and an em-dash when there is no accounting', () => {
    expect(budgetLabel({ attempts: 4 })).toBe('4');
    expect(budgetLabel({ attempts: 4, attempt_budget: 0, environment_faults: 1 })).toBe('3 (4 dispatched)');
    expect(budgetLabel({})).toBe('—');
  });
});

describe('waiting on a person', () => {
  it('is true for blocked and failed, false for every working state', () => {
    expect(isWaitingOnPerson({ status: 'blocked' })).toBe(true);
    expect(isWaitingOnPerson({ status: 'failed' })).toBe(true);
    for (const status of ['pending', 'claimed', 'in_progress', 'completed', 'cancelled']) {
      expect(isWaitingOnPerson({ status })).toBe(false);
    }
  });

  it('trims the recorded reason and returns "" when none was recorded', () => {
    expect(blockedReason({ blocked_reason: '  needs a decision  ' })).toBe('needs a decision');
    expect(blockedReason({ blocked_reason: '   ' })).toBe('');
    expect(blockedReason({})).toBe('');
    expect(NO_REASON_RECORDED).toBe('No reason recorded.');
  });
});

describe('artifactBaseName', () => {
  it('strips the attempt scope, and leaves an unscoped name alone', () => {
    expect(artifactBaseName('pull_request@a12')).toBe('pull_request');
    expect(artifactBaseName('head')).toBe('head');
    expect(artifactBaseName('@a3')).toBe('@a3');
  });
});

describe('pull request artifacts', () => {
  it('keeps the artifact the runner actually publishes: kind url, name pull_request', () => {
    // go-quicbaas#796: the filter used to test `kind === 'pull_request'`, a
    // kind the engine's artifact validator refuses, so a merged run's page
    // linked nothing. These are the bytes pkg/sdlc publishes.
    const prs = pullRequestArtifacts({
      output_artifacts: [
        { kind: 'commit', name: 'head', ref: { sha: 'abc', branch: 'issue/57' } },
        {
          kind: 'url',
          name: 'pull_request',
          ref: { path: 'https://forge/o/r/pulls/57' },
          metadata: { number: '57', state: 'merged', branch: 'issue/57' },
        },
      ],
    });
    expect(prs.map((p) => p.name)).toEqual(['pull_request']);
    expect(artifactUrl(prs[0])).toBe('https://forge/o/r/pulls/57');
  });

  it('keeps every attempt\'s pull request, because the task page is per-task', () => {
    // The names are attempt-scoped (the store overwrites by name), so a task
    // retried twice carries two pull requests and the task page shows both.
    // The per-attempt view reads the job's own `delivery` instead.
    const prs = pullRequestArtifacts({
      output_artifacts: [
        { kind: 'commit', name: 'head@a1', ref: { sha: 'aaa' } },
        { kind: 'url', name: 'pull_request@a1', ref: { path: 'https://forge/pulls/40' } },
        { kind: 'url', name: 'pull_request@a3', ref: { path: 'https://forge/pulls/57' } },
        { kind: 'inline', name: 'reviews@a3' },
      ],
    });
    expect(prs.map((p) => p.name)).toEqual(['pull_request@a1', 'pull_request@a3']);
  });

  it('returns an empty list when there are no artifacts', () => {
    expect(pullRequestArtifacts({})).toEqual([]);
    expect(pullRequestArtifacts({ output_artifacts: null })).toEqual([]);
    expect(
      pullRequestArtifacts({ output_artifacts: [{ kind: 'commit', name: 'head' }] }),
    ).toEqual([]);
  });

  it('links through metadata.url, then a URL-shaped ref path, else nothing', () => {
    expect(artifactUrl({ kind: 'url', name: 'a', metadata: { url: ' https://forge/pulls/1 ' } })).toBe('https://forge/pulls/1');
    expect(artifactUrl({ kind: 'url', name: 'b', ref: { path: 'https://forge/pulls/2' } })).toBe('https://forge/pulls/2');
    expect(artifactUrl({ kind: 'file', name: 'c', ref: { path: '/work/out.txt' } })).toBe('');
    expect(artifactUrl({ kind: 'inline', name: 'd' })).toBe('');
  });
});

describe('loadErrorMessage', () => {
  it('names the missing permission on a 403', () => {
    const msg = loadErrorMessage({ status: 403, message: 'forbidden' }, 'tasks', 'orchestrator:status');
    expect(msg).toBe('You do not have permission to view tasks: this account lacks orchestrator:status.');
  });

  it('says the subject is gone on a 404 and the session is gone on a 401', () => {
    expect(loadErrorMessage({ status: 404 }, 'this task', 'orchestrator:status')).toBe('this task not found on the server.');
    expect(loadErrorMessage({ status: 401 }, 'tasks', 'orchestrator:status')).toBe(
      'Your session is no longer valid. Sign in again to view tasks.',
    );
  });

  it("carries the server's own message for anything else, including non-Error throws", () => {
    expect(loadErrorMessage(new Error('connection refused'), 'tasks', 'p')).toBe('Could not load tasks: connection refused');
    expect(loadErrorMessage('boom', 'tasks', 'p')).toBe('Could not load tasks: boom');
    expect(loadErrorMessage({ status: '403' }, 'tasks', 'p')).toBe('Could not load tasks: [object Object]');
    expect(loadErrorMessage(null, 'tasks', 'p')).toBe('Could not load tasks: null');
  });
});

describe('taskAgentID', () => {
  it('names the claimant first, then the agent the server resolved onto the row', () => {
    expect(taskAgentID({ assigned_to: 'claimant', agent_id: 'fleet-02', spawned_job_id: 'j1' })).toBe('claimant');
    expect(taskAgentID({ agent_id: ' fleet-02 ', spawned_job_id: 'j1' })).toBe('fleet-02');
  });

  it('falls back to the latest attempt among the jobs the caller holds', () => {
    const jobs = [{ job_id: 'j0', agent_id: 'old' }, { job_id: 'j1', agent_id: 'fleet-03' }];
    expect(taskAgentID({ spawned_job_id: 'j1' }, jobs)).toBe('fleet-03');
  });

  it('answers empty when nothing records an agent', () => {
    expect(taskAgentID({ spawned_job_id: 'j1' })).toBe('');
    expect(taskAgentID({ spawned_job_id: 'j1' }, [{ job_id: 'j0', agent_id: 'old' }])).toBe('');
    expect(taskAgentID({})).toBe('');
  });
});

describe('server-derived attempt accounting (go-quicbaas#947)', () => {
  it('prefers the server\'s charged count and effective budget over re-deriving them', () => {
    // The server refunds questions too; a local re-derivation from
    // environment_faults alone would say 3.
    const task = { attempts: 4, environment_faults: 1, question_attempts: 2, charged_attempts: 1, effective_budget: 3 };
    expect(chargedAttempts(task)).toBe(1);
    expect(budgetLabel(task)).toBe('1 / 3 (4 dispatched)');
  });

  it('refunds questions as well as environment faults when the server sent no charged count', () => {
    expect(chargedAttempts({ attempts: 4, environment_faults: 1, question_attempts: 2 })).toBe(1);
    expect(chargedAttempts({ attempts: 1, charged_attempts: -1 })).toBe(0);
  });
});

describe('taskStopKind / taskStopPresentation (go-quicbaas#947)', () => {
  it('tells an environment fault, a work failure and a lost node apart', () => {
    expect(taskStopKind({ status: 'failed', fault_origin: 'environment' })).toBe('environment');
    expect(taskStopKind({ status: 'failed', fault_origin: 'work' })).toBe('work');
    expect(taskStopKind({ status: 'blocked', block_kind: 'escalated', fault_origin: 'undiagnosed' })).toBe('undiagnosed');
    const env = taskStopPresentation({ status: 'blocked', fault_origin: 'environment', fault_class: 'forge_unreachable' });
    const work = taskStopPresentation({ status: 'failed', fault_origin: 'work', fault_class: 'stalled' });
    expect(env?.label).toBe('Environment fault: forge_unreachable');
    expect(work?.label).toBe('Failed on the work');
    expect(env?.variant).not.toBe(work?.variant);
  });

  it('tells a park on the owner apart from a hold and an escalation', () => {
    expect(taskStopKind({ status: 'blocked', block_kind: 'needs_info' })).toBe('owner');
    expect(taskStopPresentation({ status: 'blocked', block_kind: 'needs_info' })?.label).toBe('Awaiting owner ruling');
    expect(taskStopKind({ status: 'blocked', block_kind: 'blocked' })).toBe('held');
    expect(taskStopKind({ status: 'blocked', block_kind: 'escalated' })).toBe('escalated');
    expect(taskStopKind({ status: 'blocked' })).toBe('held');
  });

  it('reports nothing for a task that is not stopped, and never reads the prose', () => {
    expect(taskStopKind({ status: 'completed' })).toBe('none');
    expect(taskStopKind({ status: 'pending', fault_origin: '' })).toBe('none');
    expect(taskStopPresentation({ status: 'in_progress' })).toBeNull();
    // A failed task the server did not classify is not guessed at.
    expect(taskStopPresentation({ status: 'failed' })).toBeNull();
  });

  it('labels an environment fault with no class without a trailing colon', () => {
    expect(taskStopPresentation({ status: 'blocked', fault_origin: 'environment' })?.label).toBe('Environment fault');
  });
});
