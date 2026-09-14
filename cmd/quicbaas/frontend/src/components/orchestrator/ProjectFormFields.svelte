<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  ProjectFormFields is the project record's editable surface, rendered
  identically by the create and the edit dialog. The two dialogs differ only
  in whether the name is editable and in what they do on submit, so the
  fields live here once rather than drifting apart in two copies.

  `idPrefix` keys every control's id so both dialogs can be mounted without
  colliding, and so a test can address a field by name.
-->
<script lang="ts">
  import {
    KEY_SCOPES,
    SIGNING_ALGORITHMS,
    emptyContextRepoRow,
    type ProjectFormState,
  } from '../../lib/projectForm.ts';
  import { Plus, Trash2 } from 'lucide-svelte';

  // `form` is $bindable so the dialog that owns the state stays its owner:
  // this component writes into it (a context-repository row added or removed)
  // and Svelte's ownership check is satisfied by the binding rather than
  // silenced.
  let {
    idPrefix,
    form = $bindable(),
    nameEditable = false,
    editing = false,
  }: {
    idPrefix: string;
    form: ProjectFormState;
    nameEditable?: boolean;
    // editing switches the help text from what CREATE does with an empty
    // field to what UPDATE does with one. They are not the same, and saying
    // the create rule on the edit dialog promised an operation the API does
    // not offer — see the note rendered below.
    editing?: boolean;
  } = $props();

  function addContextRepo(): void {
    form.contextRepos.push(emptyContextRepoRow());
  }

  function removeContextRepo(index: number): void {
    form.contextRepos.splice(index, 1);
  }
</script>

<div class="form-control mt-4 gap-3">
  <!--
    SAY WHAT UPDATE ACTUALLY DOES, ONCE, AT THE TOP.

    The server's update is presence-based: an absent field means "leave it
    alone", and it offers an explicit clear only for description, default
    workflow and context repositories. Every other text box here therefore
    CANNOT be emptied through this dialog — the request simply omits it and
    the stored value stays. The dialog used to imply otherwise (the SSH help
    text invited leaving the box empty to fall back to the signing key, which
    is true on create and false on edit), so an operator could clear a stale
    key, save, see no error, and still be using the old key.

    Stated rather than worked around: adding clear_git_* flags is an API
    change across every transport and the store, which is not this change's
    to make. What is this change's is to stop the UI promising it.
  -->
  {#if editing}
    <div class="alert alert-info py-2 text-xs" role="note">
      <span>
        Emptying a box leaves the stored value unchanged — type a new value to replace it.
        Only the description, default workflow and context repositories can be cleared here.
      </span>
    </div>
  {/if}

  {#if nameEditable}
    <div>
      <label class="label py-1" for="{idPrefix}-name">
        <span class="label-text text-xs">Name <span class="text-error">*</span></span>
      </label>
      <input
        id="{idPrefix}-name"
        type="text"
        class="input input-bordered input-sm w-full"
        placeholder="my-project"
        bind:value={form.name}
      />
    </div>
  {/if}

  <div>
    <label class="label py-1" for="{idPrefix}-desc">
      <span class="label-text text-xs">Description</span>
    </label>
    <input
      id="{idPrefix}-desc"
      type="text"
      class="input input-bordered input-sm w-full"
      placeholder="Optional description"
      bind:value={form.description}
    />
  </div>

  <div>
    <label class="label py-1" for="{idPrefix}-workspace-path">
      <span class="label-text text-xs">Workspace Path</span>
    </label>
    <input
      id="{idPrefix}-workspace-path"
      type="text"
      class="input input-bordered input-sm w-full font-mono"
      placeholder="/home/agent/workspace"
      bind:value={form.workspacePath}
    />
    <p class="text-xs text-base-content/40 mt-1">
      Becomes the working directory for sessions spawned under this project.
    </p>
  </div>

  <div>
    <label class="label py-1" for="{idPrefix}-git-url">
      <span class="label-text text-xs">Git Repo URL</span>
    </label>
    <input
      id="{idPrefix}-git-url"
      type="text"
      class="input input-bordered input-sm w-full"
      placeholder="https://github.com/org/repo.git"
      bind:value={form.gitRepoUrl}
    />
  </div>

  <div>
    <label class="label py-1" for="{idPrefix}-git-branch">
      <span class="label-text text-xs">Git Branch</span>
    </label>
    <input
      id="{idPrefix}-git-branch"
      type="text"
      class="input input-bordered input-sm w-full"
      placeholder="main"
      bind:value={form.gitBranch}
    />
  </div>

  <!-- Commit identity, signing policy and access key of the git workspace. -->
  <details class="collapse collapse-arrow bg-base-100 rounded-box">
    <summary class="collapse-title text-xs font-semibold py-2 min-h-0">Git identity &amp; signing</summary>
    <div class="collapse-content flex flex-col gap-3 pt-1">
      <p class="text-xs text-base-content/40">
        Supplied explicitly rather than read from the host's git config: a commit attributed to the
        wrong identity is a provenance failure that survives in history. Private key material never
        leaves the KMS — only the key's name, scope and owner are recorded here.
      </p>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label class="label py-1" for="{idPrefix}-git-author-name">
            <span class="label-text text-xs">Commit Author Name</span>
          </label>
          <input
            id="{idPrefix}-git-author-name"
            type="text"
            class="input input-bordered input-sm w-full"
            placeholder="agent"
            bind:value={form.gitAuthorName}
          />
        </div>
        <div>
          <label class="label py-1" for="{idPrefix}-git-author-email">
            <span class="label-text text-xs">Commit Author Email</span>
          </label>
          <input
            id="{idPrefix}-git-author-email"
            type="text"
            class="input input-bordered input-sm w-full"
            placeholder="agent@example.internal"
            bind:value={form.gitAuthorEmail}
          />
        </div>
      </div>

      <label class="label cursor-pointer justify-start gap-3 py-1" for="{idPrefix}-git-signing-enabled">
        <input
          id="{idPrefix}-git-signing-enabled"
          type="checkbox"
          class="toggle toggle-sm toggle-primary"
          bind:checked={form.gitSigningEnabled}
        />
        <span class="label-text text-xs">Sign commits made from this workspace</span>
      </label>
      <p class="text-xs text-base-content/40 -mt-2">
        Off means commits are explicitly unsigned. An enabled policy must name a key — it never
        degrades to unsigned.
      </p>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label class="label py-1" for="{idPrefix}-git-signing-algorithm">
            <span class="label-text text-xs">Signing Algorithm</span>
          </label>
          <select
            id="{idPrefix}-git-signing-algorithm"
            class="select select-bordered select-sm w-full"
            bind:value={form.gitSigningAlgorithm}
          >
            <option value="">Default (ED25519)</option>
            {#each SIGNING_ALGORITHMS as algorithm (algorithm)}
              <option value={algorithm}>{algorithm}</option>
            {/each}
          </select>
        </div>
        <div>
          <label class="label py-1" for="{idPrefix}-git-signing-key-name">
            <span class="label-text text-xs">Signing Key Name</span>
          </label>
          <input
            id="{idPrefix}-git-signing-key-name"
            type="text"
            class="input input-bordered input-sm w-full font-mono"
            placeholder="commit-signing"
            bind:value={form.gitSigningKeyName}
          />
        </div>
        <div>
          <label class="label py-1" for="{idPrefix}-git-signing-key-scope">
            <span class="label-text text-xs">Signing Key Scope</span>
          </label>
          <select
            id="{idPrefix}-git-signing-key-scope"
            class="select select-bordered select-sm w-full"
            bind:value={form.gitSigningKeyScope}
          >
            {#each KEY_SCOPES as scope (scope)}
              <option value={scope}>{scope === '' ? 'Resolver default' : scope}</option>
            {/each}
          </select>
        </div>
        <div>
          <label class="label py-1" for="{idPrefix}-git-signing-key-owner">
            <span class="label-text text-xs">Signing Key Owner</span>
          </label>
          <input
            id="{idPrefix}-git-signing-key-owner"
            type="text"
            class="input input-bordered input-sm w-full font-mono"
            placeholder="Resolver default"
            bind:value={form.gitSigningKeyOwner}
          />
        </div>
      </div>

      <p class="text-xs text-base-content/40">
        SSH access key for an <span class="font-mono">ssh://</span> repository URL.
        {#if editing}
          Emptying this box does not remove the stored key — see the note above.
        {:else}
          Leave empty and the signing key doubles as the access key.
        {/if}
      </p>
      <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <div>
          <label class="label py-1" for="{idPrefix}-git-ssh-key-name">
            <span class="label-text text-xs">SSH Key Name</span>
          </label>
          <input
            id="{idPrefix}-git-ssh-key-name"
            type="text"
            class="input input-bordered input-sm w-full font-mono"
            placeholder="deploy-key"
            bind:value={form.gitSshKeyName}
          />
        </div>
        <div>
          <label class="label py-1" for="{idPrefix}-git-ssh-key-scope">
            <span class="label-text text-xs">SSH Key Scope</span>
          </label>
          <select
            id="{idPrefix}-git-ssh-key-scope"
            class="select select-bordered select-sm w-full"
            bind:value={form.gitSshKeyScope}
          >
            {#each KEY_SCOPES as scope (scope)}
              <option value={scope}>{scope === '' ? 'Resolver default' : scope}</option>
            {/each}
          </select>
        </div>
        <div>
          <label class="label py-1" for="{idPrefix}-git-ssh-key-owner">
            <span class="label-text text-xs">SSH Key Owner</span>
          </label>
          <input
            id="{idPrefix}-git-ssh-key-owner"
            type="text"
            class="input input-bordered input-sm w-full font-mono"
            placeholder="Resolver default"
            bind:value={form.gitSshKeyOwner}
          />
        </div>
      </div>
    </div>
  </details>

  <!-- Autonomous development: the switch, the one concurrency number, and the
       repositories the worker clones beside the project. -->
  <details class="collapse collapse-arrow bg-base-100 rounded-box">
    <summary class="collapse-title text-xs font-semibold py-2 min-h-0">Autonomous development</summary>
    <div class="collapse-content flex flex-col gap-3 pt-1">
      <label class="label cursor-pointer justify-start gap-3 py-1" for="{idPrefix}-autonomous">
        <input
          id="{idPrefix}-autonomous"
          type="checkbox"
          class="toggle toggle-sm toggle-primary"
          bind:checked={form.autonomous}
        />
        <span class="label-text text-xs">Pull issues and submit pull requests on their own</span>
      </label>
      <p class="text-xs text-base-content/40 -mt-2">
        Off means nothing is adopted or dispatched for this project, whatever else is configured.
      </p>

      <div>
        <label class="label py-1" for="{idPrefix}-max-concurrency">
          <span class="label-text text-xs">Max Concurrent</span>
        </label>
        <input
          id="{idPrefix}-max-concurrency"
          type="number"
          min="0"
          class="input input-bordered input-sm w-full"
          placeholder="0 = unlimited"
          bind:value={form.maxConcurrency}
        />
        <p class="text-xs text-base-content/40 mt-1">
          How many jobs may run for this project at once. It is the one concurrency number.
        </p>
      </div>

      <div>
        <div class="flex items-center justify-between">
          <span class="label-text text-xs">Context Repositories</span>
          <button
            class="btn btn-xs btn-ghost gap-1"
            type="button"
            onclick={addContextRepo}
            aria-label="Add context repository"
          >
            <Plus class="h-3 w-3" />
            Add
          </button>
        </div>
        <p class="text-xs text-base-content/40 mt-1 mb-2">
          Cloned by the worker beside the project checkout — the handbook it reads while it works.
          The daemon never clones them.
        </p>

        {#if form.contextRepos.length === 0}
          <p class="text-xs text-base-content/30">No context repositories.</p>
        {:else}
          <div class="flex flex-col gap-2">
            {#each form.contextRepos as repo, i (repo)}
              <div class="flex items-end gap-2">
                <div class="flex-1 min-w-0">
                  <label class="label py-0.5" for="{idPrefix}-context-repo-name-{i}">
                    <span class="label-text text-[0.65rem]">Directory</span>
                  </label>
                  <input
                    id="{idPrefix}-context-repo-name-{i}"
                    type="text"
                    class="input input-bordered input-xs w-full font-mono"
                    placeholder="handbook"
                    bind:value={repo.name}
                  />
                </div>
                <div class="flex-[2] min-w-0">
                  <label class="label py-0.5" for="{idPrefix}-context-repo-url-{i}">
                    <span class="label-text text-[0.65rem]">Repository URL</span>
                  </label>
                  <input
                    id="{idPrefix}-context-repo-url-{i}"
                    type="text"
                    class="input input-bordered input-xs w-full font-mono"
                    placeholder="https://git.example/org/handbook"
                    bind:value={repo.repo_url}
                  />
                </div>
                <div class="flex-1 min-w-0">
                  <label class="label py-0.5" for="{idPrefix}-context-repo-ref-{i}">
                    <span class="label-text text-[0.65rem]">Ref</span>
                  </label>
                  <input
                    id="{idPrefix}-context-repo-ref-{i}"
                    type="text"
                    class="input input-bordered input-xs w-full font-mono"
                    placeholder="main"
                    bind:value={repo.ref}
                  />
                </div>
                <button
                  class="btn btn-xs btn-ghost btn-square text-error hover:bg-error/10"
                  type="button"
                  onclick={() => { removeContextRepo(i); }}
                  aria-label="Remove context repository {i + 1}"
                >
                  <Trash2 class="h-3 w-3" />
                </button>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  </details>

  <details class="collapse collapse-arrow bg-base-100 rounded-box">
    <summary class="collapse-title text-xs font-semibold py-2 min-h-0">Advanced</summary>
    <div class="collapse-content flex flex-col gap-3 pt-1">
      <div>
        <label class="label py-1" for="{idPrefix}-default-workflow">
          <span class="label-text text-xs">Default Workflow</span>
        </label>
        <input
          id="{idPrefix}-default-workflow"
          type="text"
          class="input input-bordered input-sm w-full font-mono"
          placeholder="Registered workflow name"
          bind:value={form.defaultWorkflow}
        />
        <p class="text-xs text-base-content/40 mt-1">
          The tenant-scoped workflow a Task created under this project is auto-spawned into. Empty
          means a Task is never auto-spawned into a workflow.
        </p>
      </div>
      <div>
        <label class="label py-1" for="{idPrefix}-host-affinity">
          <span class="label-text text-xs">Host Affinity</span>
        </label>
        <input
          id="{idPrefix}-host-affinity"
          type="text"
          class="input input-bordered input-sm w-full"
          placeholder="Optional daemon host/name"
          bind:value={form.hostAffinity}
        />
      </div>
      <div>
        <label class="label py-1" for="{idPrefix}-token-budget">
          <span class="label-text text-xs">Token Budget</span>
        </label>
        <input
          id="{idPrefix}-token-budget"
          type="number"
          min="0"
          class="input input-bordered input-sm w-full"
          placeholder="0 = unlimited"
          bind:value={form.tokenBudget}
        />
      </div>
      <div>
        <label class="label py-1" for="{idPrefix}-gpu-minutes">
          <span class="label-text text-xs">GPU Minutes</span>
        </label>
        <input
          id="{idPrefix}-gpu-minutes"
          type="number"
          min="0"
          class="input input-bordered input-sm w-full"
          placeholder="0 = unlimited"
          bind:value={form.gpuMinutes}
        />
      </div>
    </div>
  </details>
</div>
