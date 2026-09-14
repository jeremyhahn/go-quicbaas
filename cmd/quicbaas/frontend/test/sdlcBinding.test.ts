/**
 * sdlcBinding.test.ts
 *
 * The SDLC binding section's field mapping and its fail-closed validation,
 * plus the probe that decides whether the section is shown at all: the
 * module is not running on this node, it is running and the project is not
 * bound, or it is bound.
 */

import { describe, it, expect } from 'vitest';
import type { ProjectBinding } from '@quicbaas/sdk';
import {
  bindingAvailabilityFor,
  bindingFormFrom,
  bindingFrom,
  emptyBindingForm,
  validateBindingForm,
} from '../src/lib/sdlcBinding.ts';

/** boundProject is a binding with every field the section edits populated. */
function fullBinding(): ProjectBinding {
  return {
    project_id: 'proj-1',
    forge: {
      type: 'gitea',
      base_url: 'https://git.example/api/v1',
      owner: 'jeremyhahn',
      repo: 'go-truststrap',
      trigger_label: 'task:ready',
      token_secret_ref: 'forge-token',
      webhook_secret_ref: 'forge-webhook',
      ssh_host_key: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFAKEfakeFAKEfakeFAKEfakeFAKEfakeFAKEfake',
      labels: { in_progress: 'status/working', merged: 'status/landed' },
    },
    strategy: 'github-flow',
    worker: { backend: 'claude', model: 'claude-opus-5' },
    verification: ['make build', 'make test'],
    review: {
      required_approvals: 2,
      require_independent_reviewer: true,
      require_green_checks: true,
      checks_settle_window: 90_000_000_000,
      reviewers: { codex: 'gpt' },
    },
    max_review_rounds: 4,
    ranking: {
      default_priority: 5,
      analyzer_name: 'declared',
      declared_dependents: ['go-xkms', 'go-quicbaas'],
    },
  };
}

describe('bindingFormFrom', () => {
  it('reads every forge, strategy, worker, verification and review field', () => {
    const form = bindingFormFrom(fullBinding());
    expect(form.forgeType).toBe('gitea');
    expect(form.baseUrl).toBe('https://git.example/api/v1');
    expect(form.owner).toBe('jeremyhahn');
    expect(form.repo).toBe('go-truststrap');
    expect(form.triggerLabel).toBe('task:ready');
    expect(form.tokenSecretRef).toBe('forge-token');
    expect(form.webhookSecretRef).toBe('forge-webhook');
    expect(form.sshHostKey).toBe(fullBinding().forge.ssh_host_key);
    expect(form.labels).toEqual([
      { key: 'in_progress', label: 'status/working' },
      { key: 'merged', label: 'status/landed' },
    ]);
    expect(form.strategy).toBe('github-flow');
    expect(form.workerBackend).toBe('claude');
    expect(form.workerModel).toBe('claude-opus-5');
    expect(form.verification).toEqual(['make build', 'make test']);
    expect(form.requiredApprovals).toBe(2);
    expect(form.requireIndependentReviewer).toBe(true);
    expect(form.requireGreenChecks).toBe(true);
    expect(form.reviewers).toEqual([{ login: 'codex', family: 'gpt' }]);
    expect(form.maxReviewRounds).toBe(4);
    expect(form.rankingDefaultPriority).toBe(5);
    expect(form.rankingAnalyzerName).toBe('declared');
    expect(form.rankingDeclaredDependents).toBe('go-xkms, go-quicbaas');
  });

  it('renders the settle window in seconds from the wire nanoseconds', () => {
    expect(bindingFormFrom(fullBinding()).checksSettleWindowSeconds).toBe(90);
  });
});

describe('bindingFrom', () => {
  it('round-trips a full binding back onto the wire shape', () => {
    const wire = bindingFrom('proj-1', bindingFormFrom(fullBinding()));
    expect(wire).toEqual(fullBinding());
  });

  it('omits the optional halves rather than sending empty values', () => {
    const form = emptyBindingForm();
    form.baseUrl = 'https://git.example/api/v1';
    form.owner = 'o';
    form.repo = 'r';
    form.tokenSecretRef = 'forge-token';
    form.workerBackend = 'claude';
    const wire = bindingFrom('proj-1', form);
    expect(wire.forge.webhook_secret_ref).toBeUndefined();
    expect(wire.forge.ssh_host_key).toBeUndefined();
    expect(wire.forge.labels).toBeUndefined();
    expect(wire.review.checks_settle_window).toBeUndefined();
    expect(wire.review.reviewers).toBeUndefined();
    expect(wire.ranking.analyzer_name).toBeUndefined();
    expect(wire.ranking.declared_dependents).toBeUndefined();
    expect(wire.verification).toEqual([]);
  });

  it('splits the declared dependents on commas and drops the blanks', () => {
    const form = emptyBindingForm();
    form.rankingDeclaredDependents = ' go-xkms , , go-quicbaas ';
    expect(bindingFrom('p', form).ranking.declared_dependents).toEqual(['go-xkms', 'go-quicbaas']);
  });
});

describe('validateBindingForm', () => {
  function valid(): ReturnType<typeof emptyBindingForm> {
    return bindingFormFrom(fullBinding());
  }

  it('accepts a complete binding whose forge this build carries', () => {
    expect(validateBindingForm(valid(), ['gitea'])).toBe('');
  });

  it('refuses a forge whose adapter this build does not carry', () => {
    const form = valid();
    form.forgeType = 'github';
    expect(validateBindingForm(form, ['gitea'])).toMatch(/no github adapter is registered/i);
  });

  it('does not disable anything when the capability list is unknown', () => {
    const form = valid();
    form.forgeType = 'gitlab';
    expect(validateBindingForm(form, [])).toBe('');
  });

  it('refuses a missing forge root, repository, trigger label or token', () => {
    const noURL = valid();
    noURL.baseUrl = '';
    expect(validateBindingForm(noURL, ['gitea'])).toMatch(/API root/i);

    const noRepo = valid();
    noRepo.repo = '';
    expect(validateBindingForm(noRepo, ['gitea'])).toMatch(/repository owner and name/i);

    const noTrigger = valid();
    noTrigger.triggerLabel = '';
    expect(validateBindingForm(noTrigger, ['gitea'])).toMatch(/trigger label is required/i);

    const noToken = valid();
    noToken.tokenSecretRef = '';
    expect(validateBindingForm(noToken, ['gitea'])).toMatch(/token secret reference/i);
  });

  it('refuses a display label that collides with the trigger label', () => {
    const form = valid();
    form.labels = [{ key: 'in_progress', label: 'task:ready' }];
    expect(validateBindingForm(form, ['gitea'])).toMatch(/collides with the trigger label/i);
  });

  it('refuses a worker with no backend and a blank verification step', () => {
    const noBackend = valid();
    noBackend.workerBackend = '';
    expect(validateBindingForm(noBackend, ['gitea'])).toMatch(/worker backend is required/i);

    const blankStep = valid();
    blankStep.verification = ['make test', '  '];
    expect(validateBindingForm(blankStep, ['gitea'])).toMatch(/without running anything/i);
  });

  it('refuses a review policy that would merge unreviewed code', () => {
    const form = valid();
    form.requiredApprovals = 0;
    expect(validateBindingForm(form, ['gitea'])).toMatch(/at least one/i);
  });

  it('refuses a reviewer entry with a missing half', () => {
    const form = valid();
    form.reviewers = [{ login: 'codex', family: '' }];
    expect(validateBindingForm(form, ['gitea'])).toMatch(/model family/i);
  });

  it('refuses negative windows and rounds', () => {
    const window = valid();
    window.checksSettleWindowSeconds = -1;
    expect(validateBindingForm(window, ['gitea'])).toMatch(/must not be negative/i);

    const rounds = valid();
    rounds.maxReviewRounds = -1;
    expect(validateBindingForm(rounds, ['gitea'])).toMatch(/must not be negative/i);
  });
});

describe('bindingAvailabilityFor', () => {
  it('reads a 503 as the module not running on this node', () => {
    expect(bindingAvailabilityFor({ status: 503 })).toBe('absent');
    expect(bindingAvailabilityFor({ status: 501 })).toBe('absent');
  });

  it('reads a 404 as the module running with this project unbound', () => {
    expect(bindingAvailabilityFor({ status: 404 })).toBe('unbound');
  });

  it('reports anything else rather than swallowing it', () => {
    expect(bindingAvailabilityFor({ status: 500 })).toBe('error');
    expect(bindingAvailabilityFor(new Error('socket closed'))).toBe('error');
    expect(bindingAvailabilityFor(null)).toBe('error');
  });
});
