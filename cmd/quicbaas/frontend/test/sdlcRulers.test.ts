/**
 * sdlcRulers.test.ts
 *
 * A binding's rulers: read off the wire, written back on save, refused where
 * they could never be a forge login, and picked from the repository's
 * collaborators rather than typed.
 */

import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import type { ProjectBinding } from '@quicbaas/sdk';
import {
  bindingFormFrom,
  bindingFrom,
  emptyBindingForm,
  rulerCandidates,
  rulerHasAccess,
  validateBindingForm,
  validateRulers,
} from '../src/lib/sdlcBinding.ts';
import SdlcRulerPicker from '../src/components/orchestrator/SdlcRulerPicker.svelte';

const binding: ProjectBinding = {
  project_id: 'go-quicbaas',
  forge: { type: 'gitea', base_url: 'u', owner: 'acme', repo: 'r', trigger_label: 'task:ready', token_secret_ref: 'tok' },
  strategy: 'github-flow',
  worker: { agent_id: 'claude-coder' },
  review: { required_approvals: 1, require_independent_reviewer: true, require_green_checks: true },
  ranking: {},
  rulers: ['lead-agent'],
};

describe('binding rulers', () => {
  it('round-trips through the form', () => {
    const form = bindingFormFrom(binding);
    expect(form.rulers).toEqual(['lead-agent']);
    form.rulers.push('ops');
    expect(bindingFrom('go-quicbaas', form, binding).rulers).toEqual(['lead-agent', 'ops']);
    form.rulers = [];
    expect(bindingFrom('go-quicbaas', form, binding).rulers).toBeUndefined();
  });

  it('refuses a value that is not a forge login, or one listed twice', () => {
    expect(validateRulers(['lead-agent', 'ops.bot'])).toBe('');
    expect(validateRulers(['lead agent'])).toContain('not a forge login');
    expect(validateRulers(['-lead'])).toContain('not a forge login');
    expect(validateRulers(['lead', 'Lead'])).toContain('listed twice');
    const form = { ...bindingFormFrom(binding), rulers: ['a/b'] };
    expect(validateBindingForm(form, [])).toContain('not a forge login');
  });

  it('offers only collaborators that are neither the owner nor already chosen', () => {
    const users = [{ login: 'acme' }, { login: 'Lead-Agent' }, { login: 'ops' }];
    expect(rulerCandidates(users, 'acme', ['lead-agent']).map((u) => u.login)).toEqual(['ops']);
    expect(rulerHasAccess('ACME', 'acme', [])).toBe(true);
    expect(rulerHasAccess('ops', 'acme', users)).toBe(true);
    expect(rulerHasAccess('stranger', 'acme', users)).toBe(false);
  });

  it('starts empty on an unbound project', () => {
    expect(emptyBindingForm().rulers).toEqual([]);
  });
});

describe('SdlcRulerPicker', () => {
  it('adds a collaborator and flags a saved ruler without access', async () => {
    const rulers = ['stranger'];
    render(SdlcRulerPicker, {
      props: {
        idPrefix: 't',
        rulers,
        owner: 'acme',
        collaborators: [{ login: 'acme' }, { login: 'lead-agent', full_name: 'Lead' }],
        status: 'ready',
      },
    });
    const chip = screen.getByText('stranger').closest('[data-testid="sdlc-ruler-chip"]');
    expect(chip?.className).toContain('badge-warning');
    const select = screen.getByLabelText('Add a ruler') as HTMLSelectElement;
    expect(Array.from(select.options).map((o) => o.value)).toEqual(['', 'lead-agent']);
    await fireEvent.change(select, { target: { value: 'lead-agent' } });
    await fireEvent.click(screen.getByText('Add'));
    expect(await screen.findByText('lead-agent')).toBeTruthy();
  });

  it('explains why nothing can be picked before the project is bound', () => {
    render(SdlcRulerPicker, {
      props: { idPrefix: 't', rulers: [], owner: 'acme', collaborators: [], status: 'unbound' },
    });
    expect(screen.getByText(/Bind the project first/)).toBeTruthy();
    expect(screen.queryByLabelText('Add a ruler')).toBeNull();
  });

  it('shows why the collaborators could not be read', () => {
    render(SdlcRulerPicker, {
      props: { idPrefix: 't', rulers: [], owner: 'acme', collaborators: [], status: 'error', error: 'forge down' },
    });
    expect(screen.getByText('forge down')).toBeTruthy();
  });
});
