/**
 * sdlcReviewerProfiles.test.ts
 *
 * The reviewer profiles on the binding form: the default and the alternates
 * read off the wire, written back WITHOUT changing what any of them means, and
 * refused where two profiles could not be told apart. Plus the rule that a
 * save writes the edit on top of the binding as it was read, so a field the
 * dialog does not edit is never erased.
 */

import { describe, it, expect } from 'vitest';
import type { ProjectBinding } from '@quicbaas/sdk';
import {
  bindingFormFrom,
  bindingFrom,
  emptyReviewerProfile,
  profileStatusContext,
  validateReviewerProfiles,
  type ReviewerProfileRow,
} from '../src/lib/sdlcBinding.ts';
import { PROJECT_DEFAULT, reviewerOptions, selectedValue } from '../src/lib/sdlcReviewer.ts';

function profiledBinding(): ProjectBinding {
  return {
    project_id: 'go-quicbaas',
    forge: { type: 'gitea', base_url: 'u', owner: 'o', repo: 'r', trigger_label: 'task:ready', token_secret_ref: 'tok' },
    strategy: 'github-flow',
    worker: { agent_id: 'claude-coder' },
    triage: { pool_id: 'triagers' },
    review: { required_approvals: 1, require_independent_reviewer: true, require_green_checks: true },
    ranking: {},
    reviewer: { agent_id: 'codex-reviewer', login: 'codex', token_secret_ref: 'codex-tok' },
    reviewers: [
      {
        name: 'qwen', agent_id: 'qwen-reviewer', login: 'qwen-reviewer', token_secret_ref: 'qwen-tok',
        endpoint: 'http://gpu-host.internal:11434', host_affinity: 'gpu-host',
      },
    ],
  };
}

function row(overrides: Partial<ReviewerProfileRow>): ReviewerProfileRow {
  return { ...emptyReviewerProfile(), ...overrides };
}

describe('reviewer profiles on the binding form', () => {
  it('reads the default first, resolving each role', () => {
    const form = bindingFormFrom(profiledBinding());
    expect(form.reviewerProfiles.map((p) => p.name || 'default')).toEqual(['default', 'qwen']);
    expect(form.reviewerProfiles[0]?.countsTowardMerge).toBe(true);
    expect(form.reviewerProfiles[1]?.countsTowardMerge).toBe(false);
  });

  it('writes the profiles back unchanged, leaving unset flags unset', () => {
    const wire = bindingFrom('go-quicbaas', bindingFormFrom(profiledBinding()));
    expect(wire.reviewer).toEqual(profiledBinding().reviewer);
    expect(wire.reviewers).toEqual(profiledBinding().reviewers);
  });

  it('carries the endpoint and the host pin through a save', () => {
    const form = bindingFormFrom(profiledBinding());
    expect(form.reviewerProfiles[1]?.endpoint).toBe('http://gpu-host.internal:11434');
    expect(form.reviewerProfiles[1]?.hostAffinity).toBe('gpu-host');
    form.reviewerProfiles[1]!.endpoint = '  ';
    const wire = bindingFrom('go-quicbaas', form);
    expect(wire.reviewers?.[0]?.endpoint).toBeUndefined();
    expect(wire.reviewers?.[0]?.host_affinity).toBe('gpu-host');
  });

  it('keeps an advisory profile advisory when it becomes the default', () => {
    const form = bindingFormFrom(profiledBinding());
    form.reviewerProfiles.reverse();
    const wire = bindingFrom('go-quicbaas', form);
    expect(wire.reviewer?.name).toBe('qwen');
    expect(wire.reviewer?.counts_toward_merge).toBe(false);
    expect(wire.reviewers?.[0]?.counts_toward_merge).toBe(true);
  });

  it('writes no reviewer at all for a project that is not self-reviewed', () => {
    const form = bindingFormFrom({ ...profiledBinding(), reviewer: undefined, reviewers: undefined });
    expect(form.reviewerProfiles).toEqual([]);
    const wire = bindingFrom('go-quicbaas', form);
    expect(wire.reviewer).toBeUndefined();
    expect(wire.reviewers).toBeUndefined();
  });

  it('carries every field the dialog does not edit through a save', () => {
    const read = { ...profiledBinding(), discovery: 'poll', triage_may_close: true } as ProjectBinding & Record<string, unknown>;
    const wire = bindingFrom('go-quicbaas', bindingFormFrom(read), read) as ProjectBinding & Record<string, unknown>;
    expect(wire.discovery).toBe('poll');
    expect(wire.triage_may_close).toBe(true);
    expect(wire.triage).toEqual({ pool_id: 'triagers' });
  });
});

describe('profileStatusContext', () => {
  it('puts every counting profile on the required context and each advisory on its own', () => {
    const rows = [row({ agent: 'codex-reviewer', countsTowardMerge: true }), row({ name: 'qwen' }), row({ name: 'gpt', countsTowardMerge: true })];
    expect(profileStatusContext(rows, 0)).toBe('quicbaas/review');
    expect(profileStatusContext(rows, 1)).toBe('quicbaas/review/qwen');
    expect(profileStatusContext(rows, 2)).toBe('quicbaas/review');
  });
});

describe('validateReviewerProfiles', () => {
  const good = (): ReviewerProfileRow[] => [
    row({ agent: 'codex-reviewer', login: 'codex', tokenSecretRef: 'codex-tok', countsTowardMerge: true }),
    row({ name: 'qwen', agent: 'qwen-reviewer', login: 'qwen-reviewer', tokenSecretRef: 'qwen-tok' }),
  ];

  it('accepts a default and a distinct advisory alternate', () => {
    expect(validateReviewerProfiles(good())).toBe('');
    expect(validateReviewerProfiles([])).toBe('');
  });

  it.each([
    ['an alternate with no name', (rows: ReviewerProfileRow[]) => { rows[1]!.name = ''; }, 'needs a name'],
    ['a name outside the vocabulary', (rows: ReviewerProfileRow[]) => { rows[1]!.name = 'Qwen Big'; }, 'lowercase'],
    ['no Agent', (rows: ReviewerProfileRow[]) => { rows[1]!.agent = ''; }, 'needs an Agent'],
    ['no login', (rows: ReviewerProfileRow[]) => { rows[1]!.login = ''; }, 'forge login'],
    ['no token', (rows: ReviewerProfileRow[]) => { rows[1]!.tokenSecretRef = ''; }, 'token secret'],
    ['a shared login', (rows: ReviewerProfileRow[]) => { rows[1]!.login = 'CODEX'; }, 'own login'],
    ['a shared token', (rows: ReviewerProfileRow[]) => { rows[1]!.tokenSecretRef = 'codex-tok'; }, 'share the token'],
    ['a name the default holds', (rows: ReviewerProfileRow[]) => { rows[1]!.name = 'default'; }, 'named "default"'],
    ['an advisory profile on the required context', (rows: ReviewerProfileRow[]) => { rows[1]!.statusContext = 'quicbaas/review'; }, 'required context'],
    ['a counting alternate off the required context', (rows: ReviewerProfileRow[]) => {
      rows[1]!.countsTowardMerge = true;
      rows[1]!.statusContext = 'elsewhere';
    }, 'clear its status context'],
  ])('refuses %s', (_name, mutate, reason) => {
    const rows = good();
    mutate(rows);
    expect(validateReviewerProfiles(rows)).toContain(reason);
  });
});

describe('reviewerOptions', () => {
  it('offers the project default, then every profile with its merge role', () => {
    const options = reviewerOptions(profiledBinding());
    expect(options.map((o) => o.value)).toEqual([PROJECT_DEFAULT, 'default', 'qwen']);
    expect(options[0]?.label).toBe('Project default (default)');
    expect(options[2]?.label).toBe('qwen (advisory)');
    expect(options[2]?.countsTowardMerge).toBe(false);
  });

  it('offers nothing for a project that is not self-reviewed', () => {
    expect(reviewerOptions({ ...profiledBinding(), reviewer: undefined })).toEqual([]);
  });

  it('maps a selection onto the select value', () => {
    const base = { project_id: 'p', pull_request: 1, profile: 'qwen', counts_toward_merge: false, status_context: '' };
    expect(selectedValue(null)).toBe(PROJECT_DEFAULT);
    expect(selectedValue({ ...base, overridden: false })).toBe(PROJECT_DEFAULT);
    expect(selectedValue({ ...base, overridden: true })).toBe('qwen');
  });
});
