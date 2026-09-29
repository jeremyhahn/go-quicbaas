/**
 * projectForm.test.ts
 *
 * The Projects dialogs' field mapping: a project record fills the form, the
 * form builds a create payload, and an edited form builds an update payload
 * carrying only what moved. Every field the dialogs gained is asserted to
 * round-trip, and every refusal the server would make is asserted to be made
 * here first.
 *
 * The record under test is the SERVER'S OWN BYTES — REST_PROJECT_BODY below is
 * a verbatim `GET /api/v1/projects/{id}` body. Hydrating from a hand-written
 * object is what let go-quicbaas#797 live: the form read `project.git.repo_url`
 * and the fixture obligingly had one, while the daemon has only ever sent
 * `git_repo_url`.
 */

import { describe, it, expect } from 'vitest';
import type { Project } from '@quicbaas/sdk';
import {
  createProjectRequestFrom,
  emptyContextRepoRow,
  emptyProjectForm,
  positiveOrUndefined,
  projectFormFrom,
  updateProjectRequestFrom,
  validateProjectForm,
} from '../src/lib/projectForm.ts';

/**
 * REST_PROJECT_BODY is the `project` member of a real
 * `GET /api/v1/projects/{id}` response: flat `git_*`, `workspace_*`,
 * `token_budget` and `agent_roles` keys, exactly as
 * pkg/api/rest.ProjectRESTResponse serializes them.
 */
const REST_PROJECT_BODY = `{
  "id": "proj-1",
  "tenant_id": "default",
  "name": "go-truststrap",
  "description": "the bootstrap library",
  "workspace_path": "/work/go-truststrap",
  "host_affinity": "node-a",
  "default_workflow": "delivery",
  "autonomous": true,
  "context_repos": [
    { "name": "handbook", "repo_url": "https://git.example/org/handbook", "ref": "main" }
  ],
  "git_repo_url": "ssh://git@forge.example:2222/jeremyhahn/go-truststrap.git",
  "git_default_branch": "main",
  "git_author_name": "agent-go-truststrap",
  "git_author_email": "agent@example.internal",
  "git_signing_enabled": true,
  "git_signing_algorithm": "ED25519",
  "git_signing_key_name": "commit-signing",
  "git_signing_key_scope": "tenant",
  "git_signing_key_owner": "default",
  "git_ssh_key_name": "deploy-key",
  "git_ssh_key_scope": "service",
  "git_ssh_key_owner": "ci",
  "agent_roles": { "agent-1": "lead" },
  "spend": { "windows": [{ "name": "weekly", "size": 604800000000000, "max_equivalent_micros": 250000000 }] },
  "gpu_minutes": 30,
  "max_concurrency": 2,
  "created_at": "2026-09-01T00:00:00Z",
  "updated_at": "2026-09-02T00:00:00Z"
}`;

/** fullProject is the server's body, decoded — every field the dialogs edit. */
function fullProject(): Project {
  return JSON.parse(REST_PROJECT_BODY) as Project;
}

describe('projectFormFrom', () => {
  it('reads every git identity, autonomy and quota field off the record', () => {
    const form = projectFormFrom(fullProject());
    expect(form.name).toBe('go-truststrap');
    expect(form.gitAuthorName).toBe('agent-go-truststrap');
    expect(form.gitAuthorEmail).toBe('agent@example.internal');
    expect(form.gitSigningEnabled).toBe(true);
    expect(form.gitSigningAlgorithm).toBe('ED25519');
    expect(form.gitSigningKeyName).toBe('commit-signing');
    expect(form.gitSigningKeyScope).toBe('tenant');
    expect(form.gitSigningKeyOwner).toBe('default');
    expect(form.gitSshKeyName).toBe('deploy-key');
    expect(form.gitSshKeyScope).toBe('service');
    expect(form.gitSshKeyOwner).toBe('ci');
    expect(form.defaultWorkflow).toBe('delivery');
    expect(form.autonomous).toBe(true);
    expect(form.maxConcurrency).toBe(2);
    expect(form.contextRepos).toEqual([
      { name: 'handbook', repo_url: 'https://git.example/org/handbook', ref: 'main' },
    ]);
  });

  it('populates every git field from the REST body the daemon actually sends', () => {
    // go-quicbaas#797: the edit dialog showed an empty Git repository URL on
    // every existing project because the form read a nested `git` member the
    // API has never produced. Every git control is asserted here, from the
    // wire bytes, so a re-nesting of the type fails this test rather than the
    // operator's next edit.
    const form = projectFormFrom(fullProject());
    expect(form.gitRepoUrl).toBe('ssh://git@forge.example:2222/jeremyhahn/go-truststrap.git');
    expect(form.gitBranch).toBe('main');
    expect(form.gitAuthorName).toBe('agent-go-truststrap');
    expect(form.gitAuthorEmail).toBe('agent@example.internal');
    expect(form.gitSigningEnabled).toBe(true);
    expect(form.gitSigningAlgorithm).toBe('ED25519');
    expect(form.gitSigningKeyName).toBe('commit-signing');
    expect(form.gitSigningKeyScope).toBe('tenant');
    expect(form.gitSigningKeyOwner).toBe('default');
    expect(form.gitSshKeyName).toBe('deploy-key');
    expect(form.gitSshKeyScope).toBe('service');
    expect(form.gitSshKeyOwner).toBe('ci');
    expect(form.workspacePath).toBe('/work/go-truststrap');
    expect(form.spend).toEqual([{ name: 'weekly', hours: 168, tokens: 0, usd: 250 }]);
    expect(form.gpuMinutes).toBe(30);
    expect(form.maxConcurrency).toBe(2);
  });

  it('leaves every field empty for a project that carries none of them', () => {
    const bare: Project = {
      id: 'p',
      tenant_id: 'default',
      name: 'bare',
      created_at: '',
      updated_at: '',
    };
    const form = projectFormFrom(bare);
    expect(form).toEqual({ ...emptyProjectForm(), name: 'bare' });
  });
});

describe('validateProjectForm', () => {
  it('accepts a fully populated form', () => {
    expect(validateProjectForm(projectFormFrom(fullProject()), true)).toBe('');
  });

  it('refuses a create with no name', () => {
    const form = emptyProjectForm();
    expect(validateProjectForm(form, true)).toMatch(/name is required/i);
    // An edit does not rename, so the same form is fine there.
    expect(validateProjectForm(form, false)).toBe('');
  });

  it('refuses a half-set commit author in either direction', () => {
    const withName = { ...emptyProjectForm(), name: 'p', gitAuthorName: 'agent' };
    expect(validateProjectForm(withName, true)).toMatch(/email/i);
    const withEmail = { ...emptyProjectForm(), name: 'p', gitAuthorEmail: 'a@example' };
    expect(validateProjectForm(withEmail, true)).toMatch(/name/i);
  });

  it('refuses signing that is enabled but names no key', () => {
    const form = { ...emptyProjectForm(), name: 'p', gitSigningEnabled: true };
    expect(validateProjectForm(form, true)).toMatch(/never degrade to unsigned/i);
  });

  it('refuses a context repository that is empty, half-filled, nested or duplicated', () => {
    const base = { ...emptyProjectForm(), name: 'p' };
    expect(validateProjectForm({ ...base, contextRepos: [emptyContextRepoRow()] }, true)).toMatch(
      /row is empty/i,
    );
    expect(
      validateProjectForm({ ...base, contextRepos: [{ name: 'hb', repo_url: '', ref: 'main' }] }, true),
    ).toMatch(/repository URL/i);
    expect(
      validateProjectForm({ ...base, contextRepos: [{ name: 'hb', repo_url: 'u', ref: '' }] }, true),
    ).toMatch(/ref/i);
    expect(
      validateProjectForm({ ...base, contextRepos: [{ name: 'a/b', repo_url: 'u', ref: 'main' }] }, true),
    ).toMatch(/single path segment/i);
    expect(
      validateProjectForm(
        {
          ...base,
          contextRepos: [
            { name: 'hb', repo_url: 'u', ref: 'main' },
            { name: 'hb', repo_url: 'v', ref: 'main' },
          ],
        },
        true,
      ),
    ).toMatch(/listed twice/i);
  });
});

describe('createProjectRequestFrom', () => {
  it('carries every new field onto the wire shape', () => {
    const req = createProjectRequestFrom('default', projectFormFrom(fullProject()));
    expect(req.tenant_id).toBe('default');
    expect(req.name).toBe('go-truststrap');
    expect(req.git_author_name).toBe('agent-go-truststrap');
    expect(req.git_author_email).toBe('agent@example.internal');
    expect(req.git_signing_enabled).toBe(true);
    expect(req.git_signing_algorithm).toBe('ED25519');
    expect(req.git_signing_key_name).toBe('commit-signing');
    expect(req.git_signing_key_scope).toBe('tenant');
    expect(req.git_signing_key_owner).toBe('default');
    expect(req.git_ssh_key_name).toBe('deploy-key');
    expect(req.git_ssh_key_scope).toBe('service');
    expect(req.git_ssh_key_owner).toBe('ci');
    expect(req.default_workflow).toBe('delivery');
    expect(req.autonomous).toBe(true);
    expect(req.max_concurrency).toBe(2);
    expect(req.context_repos).toEqual([
      { name: 'handbook', repo_url: 'https://git.example/org/handbook', ref: 'main' },
    ]);
  });

  it('omits every unset field rather than sending an empty string, a zero or a false', () => {
    const req = createProjectRequestFrom('default', { ...emptyProjectForm(), name: 'p' });
    expect(req.git_author_name).toBeUndefined();
    expect(req.git_signing_enabled).toBeUndefined();
    expect(req.git_signing_key_scope).toBeUndefined();
    expect(req.git_ssh_key_name).toBeUndefined();
    expect(req.default_workflow).toBeUndefined();
    expect(req.autonomous).toBeUndefined();
    expect(req.context_repos).toBeUndefined();
    expect(req.max_concurrency).toBeUndefined();
    expect(req.spend).toBeUndefined();
  });

  it('trims whitespace off every text field it sends', () => {
    const req = createProjectRequestFrom('default', {
      ...emptyProjectForm(),
      name: '  p  ',
      gitAuthorName: '  agent  ',
      gitAuthorEmail: '  a@example  ',
      contextRepos: [{ name: ' hb ', repo_url: ' u ', ref: ' main ' }],
    });
    expect(req.name).toBe('p');
    expect(req.git_author_name).toBe('agent');
    expect(req.git_author_email).toBe('a@example');
    expect(req.context_repos).toEqual([{ name: 'hb', repo_url: 'u', ref: 'main' }]);
  });
});

describe('updateProjectRequestFrom', () => {
  it('sends nothing but the identifiers when nothing moved', () => {
    const project = fullProject();
    const req = updateProjectRequestFrom('default', project, projectFormFrom(project));
    expect(req).toEqual({ tenant_id: 'default', project_id: 'proj-1' });
  });

  it('sends no change at all when the REST body is loaded and saved untouched', () => {
    // The round trip that go-quicbaas#797 broke silently: hydration dropped
    // every git field, so a save of an untouched form looked like a set of
    // deliberate clears to the reader of the request (and, for the fields the
    // server does clear, would have been).
    const project = fullProject();
    const req = updateProjectRequestFrom('default', project, projectFormFrom(project));
    expect(req).toEqual({ tenant_id: 'default', project_id: 'proj-1' });
    expect(Object.keys(req).sort()).toEqual(['project_id', 'tenant_id']);
  });

  it('sends only the git identity field that changed', () => {
    const project = fullProject();
    const form = projectFormFrom(project);
    form.gitAuthorEmail = 'other@example.internal';
    const req = updateProjectRequestFrom('default', project, form);
    expect(req.git_author_email).toBe('other@example.internal');
    expect(req.git_author_name).toBeUndefined();
    expect(req.git_signing_key_name).toBeUndefined();
  });

  it('sends a quota of zero, because zero means unlimited', () => {
    // The dialog, the docs and the project record all define 0 as
    // unlimited. Dropping it as "unset" meant an operator could lift a cap,
    // watch the save succeed, and stay capped.
    const project = fullProject();
    const form = projectFormFrom(project);
    form.maxConcurrency = 0;
    form.gpuMinutes = 0;
    const req = updateProjectRequestFrom('default', project, form);
    expect(req.max_concurrency).toBe(0);
    expect(req.gpu_minutes).toBe(0);
  });

  it('omits a quota that did not move, including one already at zero', () => {
    const project: Project = { ...fullProject(), max_concurrency: 0 };
    const form = projectFormFrom(project);
    const req = updateProjectRequestFrom('default', project, form);
    expect(req.spend).toBeUndefined();
    expect(req.max_concurrency).toBeUndefined();
  });

  // go-quicbaas#1058: spend limits replaced the unenforced token budget.
  it('sends the spend limits whole when they changed, and an explicit clear', () => {
    const project = fullProject();
    const form = projectFormFrom(project);
    form.spend[0]!.usd = 100;
    form.spend.push({ name: 'five-hour', hours: 5, tokens: 2_000_000, usd: 0 });
    const req = updateProjectRequestFrom('default', project, form);
    expect(req.spend).toEqual({
      windows: [
        { name: 'weekly', size: 604_800_000_000_000, max_equivalent_micros: 100_000_000 },
        { name: 'five-hour', size: 18_000_000_000_000, max_tokens: 2_000_000 },
      ],
    });

    const cleared = projectFormFrom(project);
    cleared.spend = [];
    expect(updateProjectRequestFrom('default', project, cleared).spend).toEqual({ windows: [] });
  });

  it('refuses a spend window that caps nothing', () => {
    const form = projectFormFrom(fullProject());
    form.spend.push({ name: 'daily', hours: 24, tokens: 0, usd: 0 });
    expect(validateProjectForm(form, false)).toMatch(/caps nothing/);
  });

  it('refuses a negative quota rather than sending it', () => {
    const project = fullProject();
    const form = projectFormFrom(project);
    form.maxConcurrency = -1;
    expect(updateProjectRequestFrom('default', project, form).max_concurrency).toBeUndefined();
  });

  it('sends the signing switch when it is turned OFF, so off is expressible', () => {
    const project = fullProject();
    const form = projectFormFrom(project);
    form.gitSigningEnabled = false;
    expect(updateProjectRequestFrom('default', project, form).git_signing_enabled).toBe(false);
  });

  it('sends the autonomy switch in both directions and omits it when unmoved', () => {
    const project = fullProject();
    const off = projectFormFrom(project);
    off.autonomous = false;
    expect(updateProjectRequestFrom('default', project, off).autonomous).toBe(false);

    const unchanged = projectFormFrom(project);
    expect(updateProjectRequestFrom('default', project, unchanged).autonomous).toBeUndefined();

    const wasOff: Project = { ...project, autonomous: false };
    const on = projectFormFrom(wasOff);
    on.autonomous = true;
    expect(updateProjectRequestFrom('default', wasOff, on).autonomous).toBe(true);
  });

  it('clears the default workflow when it is emptied and sets it when it is named', () => {
    const project = fullProject();
    const cleared = projectFormFrom(project);
    cleared.defaultWorkflow = '';
    const clearReq = updateProjectRequestFrom('default', project, cleared);
    expect(clearReq.clear_default_workflow).toBe(true);
    expect(clearReq.default_workflow).toBeUndefined();

    const renamed = projectFormFrom(project);
    renamed.defaultWorkflow = 'sdlc';
    const setReq = updateProjectRequestFrom('default', project, renamed);
    expect(setReq.default_workflow).toBe('sdlc');
    expect(setReq.clear_default_workflow).toBeUndefined();
  });

  it('replaces the context repositories when a row changes and clears them when all go', () => {
    const project = fullProject();
    const added = projectFormFrom(project);
    added.contextRepos.push({ name: 'conventions', repo_url: 'https://git/x', ref: 'v1' });
    const addReq = updateProjectRequestFrom('default', project, added);
    expect(addReq.context_repos).toHaveLength(2);
    expect(addReq.clear_context_repos).toBeUndefined();

    const emptied = projectFormFrom(project);
    emptied.contextRepos = [];
    const clearReq = updateProjectRequestFrom('default', project, emptied);
    expect(clearReq.clear_context_repos).toBe(true);
    expect(clearReq.context_repos).toBeUndefined();
  });
});

describe('positiveOrUndefined', () => {
  it('keeps a positive value and drops a non-positive one', () => {
    expect(positiveOrUndefined(3)).toBe(3);
    expect(positiveOrUndefined(0)).toBeUndefined();
    expect(positiveOrUndefined(-1)).toBeUndefined();
  });
});

describe('session rotation', () => {
  it('opens a project that never chose a policy on the default, resume', () => {
    // REST_PROJECT_BODY carries no session_rotation: the server omits the key
    // for a project that never set one.
    expect(projectFormFrom(fullProject()).sessionRotation).toBe('resume');
    expect(emptyProjectForm().sessionRotation).toBe('resume');
  });

  it('reads a chosen policy off the record', () => {
    const project: Project = { ...fullProject(), session_rotation: 'fresh' };
    expect(projectFormFrom(project).sessionRotation).toBe('fresh');
  });

  it('sends a departure from the default on create, and nothing for the default', () => {
    const fresh = createProjectRequestFrom('default', { ...emptyProjectForm(), name: 'p', sessionRotation: 'fresh' });
    expect(fresh.session_rotation).toBe('fresh');
    const resume = createProjectRequestFrom('default', { ...emptyProjectForm(), name: 'p' });
    expect(resume.session_rotation).toBeUndefined();
  });

  it('sends the policy on update whenever it moved, either way, and omits it when unmoved', () => {
    const project = fullProject();
    const unchanged = projectFormFrom(project);
    expect(updateProjectRequestFrom('default', project, unchanged).session_rotation).toBeUndefined();

    const toFresh = projectFormFrom(project);
    toFresh.sessionRotation = 'fresh';
    expect(updateProjectRequestFrom('default', project, toFresh).session_rotation).toBe('fresh');

    const wasFresh: Project = { ...project, session_rotation: 'fresh' };
    const back = projectFormFrom(wasFresh);
    back.sessionRotation = 'resume';
    expect(updateProjectRequestFrom('default', wasFresh, back).session_rotation).toBe('resume');
  });
});
