/**
 * The Prompts page link format (src/lib/prompts.ts) and the job page's
 * prompt-version lines built on it (src/lib/jobDetail.ts), go-quicbaas#806.
 */
import { describe, it, expect } from 'vitest';
import type { PromptVersionRef } from '@quicbaas/sdk';
import {
  jobPromptLines,
  missingPlaceholders,
  parsePromptLink,
  promptPlaceholders,
  promptAvailability,
  promptLinkHref,
  promptOrigin,
  promptRefHref,
  promptRefLabel,
  shortPromptHash,
} from '../src/lib/prompts.ts';

const HASH = 'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';

/** paramsOf reads a hash's query the way the router hands it to a page. */
function paramsOf(href: string): Record<string, string> {
  return Object.fromEntries(new URLSearchParams(href.split('?')[1] ?? ''));
}

describe('promptLinkHref and parsePromptLink', () => {
  it('round-trips a project-scope link', () => {
    const link = { set: 'sdlc', scope: 'project', project: 'proj 1', name: 'plan', version: 4 };
    const href = promptLinkHref(link);
    expect(href.startsWith('#/prompts?')).toBe(true);
    expect(parsePromptLink(paramsOf(href))).toEqual(link);
  });

  it('drops a project id from a link at any other scope', () => {
    const href = promptLinkHref({ set: 'workflow', scope: 'tenant', project: 'proj-1', name: 'lead', version: 2 });
    expect(paramsOf(href)['project']).toBeUndefined();
    expect(parsePromptLink({ ...paramsOf(href), project: 'proj-1' })?.project).toBeUndefined();
  });

  it('answers null when the params name no prompt', () => {
    expect(parsePromptLink({})).toBeNull();
    expect(parsePromptLink({ set: 'sdlc', scope: 'tenant', name: '  ' })).toBeNull();
  });

  it('answers null for a set or scope the page does not browse', () => {
    expect(parsePromptLink({ set: 'other', scope: 'tenant', name: 'plan' })).toBeNull();
    expect(parsePromptLink({ set: 'sdlc', scope: 'embedded', name: 'plan' })).toBeNull();
  });

  it('answers null for a project scope without its project', () => {
    expect(parsePromptLink({ set: 'sdlc', scope: 'project', name: 'plan' })).toBeNull();
  });

  it('reads a version that is not a positive integer as 0', () => {
    const base = { set: 'sdlc', scope: 'system', name: 'plan' };
    expect(parsePromptLink({ ...base, version: 'x' })?.version).toBe(0);
    expect(parsePromptLink({ ...base, version: '-3' })?.version).toBe(0);
    expect(parsePromptLink({ ...base, version: '1.5' })?.version).toBe(0);
    expect(parsePromptLink(base)?.version).toBe(0);
  });
});

describe('the job page prompt-version lines', () => {
  const projectRef: PromptVersionRef = {
    set: 'sdlc', name: 'plan', scope: 'project', scope_id: 'proj-1', version: 4, hash: HASH,
  };

  it('shortens a content hash to eight hex digits without its algorithm tag', () => {
    expect(shortPromptHash(HASH)).toBe('01234567');
  });

  it('shortens a hash with no algorithm tag as it is', () => {
    expect(shortPromptHash('abcdef0123')).toBe('abcdef01');
    expect(shortPromptHash('')).toBe('');
  });

  it('labels a ref as set/name · scope · vN · hash8', () => {
    expect(promptRefLabel(projectRef)).toBe('sdlc/plan · project · v4 · 01234567');
  });

  it('links a project ref to its project and an embedded ref to the system scope', () => {
    expect(parsePromptLink(paramsOf(promptRefHref(projectRef) ?? ''))).toEqual({
      set: 'sdlc', scope: 'project', project: 'proj-1', name: 'plan', version: 4,
    });
    const embedded: PromptVersionRef = { set: 'sdlc', name: 'plan', scope: 'embedded', version: 0, hash: HASH };
    expect(parsePromptLink(paramsOf(promptRefHref(embedded) ?? ''))).toEqual({
      set: 'sdlc', scope: 'system', project: undefined, name: 'plan', version: 0,
    });
  });

  it('keeps every ref, in order, with a distinct key even for a repeated prompt', () => {
    const lines = jobPromptLines({ prompts: [projectRef, projectRef] });
    expect(lines.map((l) => l.label)).toEqual([promptRefLabel(projectRef), promptRefLabel(projectRef)]);
    expect(new Set(lines.map((l) => l.key)).size).toBe(2);
  });

  it('has no lines for a job that recorded no refs', () => {
    expect(jobPromptLines({})).toEqual([]);
    expect(jobPromptLines({ prompts: [] })).toEqual([]);
  });

  it('links nowhere for a set the page does not browse or a project ref without its project', () => {
    expect(promptRefHref({ ...projectRef, set: 'other' })).toBeNull();
    expect(promptRefHref({ ...projectRef, scope_id: undefined })).toBeNull();
  });
});

describe('promptOrigin', () => {
  it('names an override at this scope', () => {
    expect(promptOrigin({ overridden: true, effective: { scope: 'project' } }).label).toBe('overridden here');
  });

  it('names the layer an inherited prompt comes from, the embedded body as system', () => {
    expect(promptOrigin({ overridden: false, effective: { scope: 'tenant' } }).label).toBe('inherited from tenant');
    expect(promptOrigin({ overridden: false, effective: { scope: 'embedded' } }).label).toBe('inherited from system');
  });
});

describe('promptPlaceholders and missingPlaceholders', () => {
  it('lists each placeholder once, in order', () => {
    expect(promptPlaceholders('Hi ${user}, see ${repo.url} and ${user} $notone')).toEqual(['user', 'repo.url']);
  });

  it('names the placeholders a draft dropped', () => {
    expect(missingPlaceholders('${a} ${b} ${c}', '${c} ${a}')).toEqual(['b']);
    expect(missingPlaceholders('${a}', '${a} ${new}')).toEqual([]);
  });
});

describe('promptAvailability', () => {
  it('reads 403 as the permission the caller lacks', () => {
    const got = promptAvailability({ status: 403 });
    expect(got?.kind).toBe('forbidden');
    expect(got?.message).toContain('prompts:read');
  });

  it('reads 503 as the prompt store not running on this node', () => {
    expect(promptAvailability({ status: 503 })).toEqual({
      kind: 'not-running',
      message: 'The prompt store is not running on this node.',
    });
  });

  it('answers null for any other failure', () => {
    expect(promptAvailability({ status: 409 })).toBeNull();
    expect(promptAvailability(new Error('boom'))).toBeNull();
    expect(promptAvailability(null)).toBeNull();
  });
});
