// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Reviewer profiles on the wire: the binding carries the default reviewer and
// the alternates (pkg/orchestration/sdlc/binding.go `reviewer` / `reviewers`), and the
// per-pull-request override is three verbs on one route
// (pkg/api/rest/handlers_sdlc.go).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import type { ProjectBinding } from '../src/sdlc';

function makeResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

const selection = {
  project_id: 'p1',
  pull_request: 41,
  profile: 'qwen',
  overridden: true,
  counts_toward_merge: false,
  status_context: 'quicbaas/review/qwen',
};

describe('RestSdlcClient reviewer override', () => {
  it('reads the selection from the pull request route', async () => {
    fetchMock.mockResolvedValue(makeResponse(selection));
    const got = await new RestClient('http://localhost').sdlc.reviewer('t1', 'p1', 41);
    expect(fetchMock.mock.calls[0][0]).toBe('http://localhost/api/v1/sdlc/projects/p1/pulls/41/reviewer?tenant_id=t1');
    expect(got.profile).toBe('qwen');
    expect(got.counts_toward_merge).toBe(false);
  });

  it('sets the override with a PUT carrying the profile alone', async () => {
    fetchMock.mockResolvedValue(makeResponse(selection));
    await new RestClient('http://localhost').sdlc.setReviewer('t1', 'p1', 41, 'qwen');
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/sdlc/projects/p1/pulls/41/reviewer?tenant_id=t1');
    expect(init.method).toBe('PUT');
    expect(JSON.parse(init.body)).toEqual({ profile: 'qwen' });
  });

  it('clears the override with a DELETE and returns the selection that now holds', async () => {
    fetchMock.mockResolvedValue(makeResponse({ ...selection, profile: 'default', overridden: false, counts_toward_merge: true }));
    const got = await new RestClient('http://localhost').sdlc.clearReviewer('t1', 'p1', 41);
    expect(fetchMock.mock.calls[0][1].method).toBe('DELETE');
    expect(got.overridden).toBe(false);
  });

  it('surfaces a refusal as an error rather than an empty selection', async () => {
    fetchMock.mockResolvedValue(makeResponse({ error: 'no such profile' }, 400));
    await expect(new RestClient('http://localhost').sdlc.setReviewer('t1', 'p1', 41, 'gone')).rejects.toThrow();
  });
});

describe('RestSdlcClient bind carries the reviewer profiles', () => {
  it('writes the default and every alternate, keeping an unset counts_toward_merge unset', async () => {
    const binding: ProjectBinding = {
      project_id: 'p1',
      forge: { type: 'gitea', base_url: 'u', owner: 'o', repo: 'r', trigger_label: 'task:ready' },
      strategy: 'github-flow',
      worker: { pool_id: 'coders' },
      triage: { agent_id: 'openvino-triage' },
      review: { required_approvals: 1, require_independent_reviewer: true, require_green_checks: true },
      ranking: {},
      reviewer: { agent_id: 'codex-reviewer', login: 'codex', token_secret_ref: 'codex-tok' },
      reviewers: [{ name: 'qwen', agent_id: 'qwen-reviewer', login: 'qwen-reviewer', token_secret_ref: 'qwen-tok' }],
    };
    fetchMock.mockResolvedValue(makeResponse(binding));
    await new RestClient('http://localhost').sdlc.bind('t1', 'p1', binding);
    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.reviewer.login).toBe('codex');
    expect(body.reviewer.agent_id).toBe('codex-reviewer');
    expect(body.worker).toEqual({ pool_id: 'coders' });
    expect(body.triage).toEqual({ agent_id: 'openvino-triage' });
    expect(body.reviewers).toHaveLength(1);
    expect(body.reviewers[0].name).toBe('qwen');
    expect('counts_toward_merge' in body.reviewers[0]).toBe(false);
  });
});
