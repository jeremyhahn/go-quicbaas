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
  SdlcBindingFields edits a project's SDLC binding in place, inside the
  Projects edit dialog. It is only ever mounted where the SDLC module answers
  — the caller probes the binding endpoint and hides the whole section when
  the module is not running on this node.

  An SCM whose adapter this build does not carry is offered DISABLED with the
  reason, never hidden: an operator who cannot find GitHub in the list learns
  nothing, and one who sees it greyed out with "no adapter in this build"
  learns exactly what would have to change.
-->
<script lang="ts">
  import {
    LABEL_KEYS,
    SCM_TYPES,
    SDLC_STRATEGIES,
    type BindingFormState,
  } from '../../lib/sdlcBinding.ts';
  import { Plus, Trash2 } from 'lucide-svelte';
  import RefPicker from '../core/RefPicker.svelte';
  import SdlcReviewerProfiles from './SdlcReviewerProfiles.svelte';
  import SdlcRulerPicker from './SdlcRulerPicker.svelte';
  import type { ForgeUser } from '@quicbaas/sdk';

  // `binding` is $bindable for the same reason ProjectFormFields' form is: the
  // dialog owns the state and this component edits rows within it.
  let {
    idPrefix,
    binding = $bindable(),
    availableForges,
    collaborators = [],
    collaboratorsStatus = 'unbound',
    collaboratorsError = '',
    tenantID,
  }: {
    idPrefix: string;
    binding: BindingFormState;
    availableForges: string[];
    collaborators?: ForgeUser[];
    collaboratorsStatus?: 'loading' | 'ready' | 'unbound' | 'error';
    collaboratorsError?: string;
    tenantID?: string;
  } = $props();

  /**
   * forgeAvailable reports whether this build carries an adapter. An empty
   * availability list means the server did not say, in which case nothing is
   * disabled — a UI that greys out every option because it could not ask is
   * worse than one that lets the server refuse.
   */
  function forgeAvailable(type: string): boolean {
    return availableForges.length === 0 || availableForges.includes(type);
  }

  function addVerification(): void {
    binding.verification.push('');
  }

  function removeVerification(index: number): void {
    binding.verification.splice(index, 1);
  }

  function addReviewer(): void {
    binding.reviewers.push({ login: '', family: '' });
  }

  function removeReviewer(index: number): void {
    binding.reviewers.splice(index, 1);
  }

  function addLabel(): void {
    const used = new Set(binding.labels.map((row) => row.key));
    const next = LABEL_KEYS.find((key) => !used.has(key)) ?? '';
    binding.labels.push({ key: next, label: '' });
  }

  function removeLabel(index: number): void {
    binding.labels.splice(index, 1);
  }
</script>

<div class="flex flex-col gap-3 pt-1">
  <p class="text-xs text-base-content/40">
    Once autonomous development is on, the module pulls issues carrying the trigger label and
    submits pull requests on its own, bounded by the project's max concurrent.
  </p>

  <!-- Forge -->
  <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
    <div>
      <label class="label py-1" for="{idPrefix}-sdlc-forge-type">
        <span class="label-text text-xs">Source Control <span class="text-error">*</span></span>
      </label>
      <select
        id="{idPrefix}-sdlc-forge-type"
        class="select select-bordered select-sm w-full"
        bind:value={binding.forgeType}
      >
        {#each SCM_TYPES as scm (scm)}
          <option
            value={scm}
            disabled={!forgeAvailable(scm)}
            title={forgeAvailable(scm) ? '' : `No ${scm} adapter is registered in this build.`}
          >
            {scm}{forgeAvailable(scm) ? '' : ' — no adapter in this build'}
          </option>
        {/each}
      </select>
    </div>
    <div>
      <label class="label py-1" for="{idPrefix}-sdlc-strategy">
        <span class="label-text text-xs">SDLC Strategy <span class="text-error">*</span></span>
      </label>
      <select
        id="{idPrefix}-sdlc-strategy"
        class="select select-bordered select-sm w-full"
        bind:value={binding.strategy}
      >
        {#each SDLC_STRATEGIES as strategy (strategy)}
          <option value={strategy}>{strategy}</option>
        {/each}
      </select>
      <p class="text-xs text-base-content/40 mt-1">
        Decides branch name, base and target, whether a pull request is required, and how it lands.
      </p>
    </div>
  </div>

  <div>
    <label class="label py-1" for="{idPrefix}-sdlc-base-url">
      <span class="label-text text-xs">Forge API Root <span class="text-error">*</span></span>
    </label>
    <input
      id="{idPrefix}-sdlc-base-url"
      type="text"
      class="input input-bordered input-sm w-full font-mono"
      placeholder="https://git.example/api/v1"
      bind:value={binding.baseUrl}
    />
  </div>

  <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
    <div>
      <label class="label py-1" for="{idPrefix}-sdlc-owner">
        <span class="label-text text-xs">Owner <span class="text-error">*</span></span>
      </label>
      <input
        id="{idPrefix}-sdlc-owner"
        type="text"
        class="input input-bordered input-sm w-full font-mono"
        placeholder="jeremyhahn"
        bind:value={binding.owner}
      />
    </div>
    <div>
      <label class="label py-1" for="{idPrefix}-sdlc-repo">
        <span class="label-text text-xs">Repository <span class="text-error">*</span></span>
      </label>
      <input
        id="{idPrefix}-sdlc-repo"
        type="text"
        class="input input-bordered input-sm w-full font-mono"
        placeholder="go-quicbaas"
        bind:value={binding.repo}
      />
    </div>
  </div>

  <div>
    <label class="label py-1" for="{idPrefix}-sdlc-trigger-label">
      <span class="label-text text-xs">Trigger Label <span class="text-error">*</span></span>
    </label>
    <input
      id="{idPrefix}-sdlc-trigger-label"
      type="text"
      class="input input-bordered input-sm w-full font-mono"
      placeholder="task:ready"
      bind:value={binding.triggerLabel}
    />
    <p class="text-xs text-base-content/40 mt-1">
      The label an open issue must carry before the module adopts it. Compared exactly, and
      case-sensitively.
    </p>
  </div>

  <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
    <div>
      <label class="label py-1" for="{idPrefix}-sdlc-token-secret-ref">
        <span class="label-text text-xs">Token Secret Ref <span class="text-error">*</span></span>
      </label>
      <RefPicker
        id="{idPrefix}-sdlc-token-secret-ref"
        kind="secret"
        {tenantID}
        placeholder="forge-token"
        bind:value={binding.tokenSecretRef}
      />
    </div>
    <div>
      <label class="label py-1" for="{idPrefix}-sdlc-webhook-secret-ref">
        <span class="label-text text-xs">Webhook Secret Ref</span>
      </label>
      <RefPicker
        id="{idPrefix}-sdlc-webhook-secret-ref"
        kind="secret"
        {tenantID}
        placeholder="Empty = polled only"
        bind:value={binding.webhookSecretRef}
      />
    </div>
  </div>
  <p class="text-xs text-base-content/40 -mt-2">
    Names of tenant secrets. The values stay in the secret store and never reach this record.
  </p>

  <div>
    <label class="label py-1" for="{idPrefix}-sdlc-ssh-host-key">
      <span class="label-text text-xs">SSH Host Key</span>
    </label>
    <input
      id="{idPrefix}-sdlc-ssh-host-key"
      type="text"
      class="input input-bordered input-sm w-full font-mono"
      placeholder="ssh-ed25519 AAAAC3Nza..."
      bind:value={binding.sshHostKey}
    />
    <p class="text-xs text-base-content/40 mt-1">
      The forge's own host public key, one OpenSSH line as <code>ssh-keyscan</code> prints it after
      the host field. Required when the project is cloned over ssh: the worker verifies the forge
      against this key and never accepts an unknown one. Public data, not a secret.
    </p>
  </div>

  <!-- Worker -->
  <div>
    <label class="label py-1" for="{idPrefix}-sdlc-worker">
      <span class="label-text text-xs">Worker Agent or pool <span class="text-error">*</span></span>
    </label>
    <input
      id="{idPrefix}-sdlc-worker"
      type="text"
      class="input input-bordered input-sm w-full font-mono"
      placeholder="agent:claude-coder or pool:coders"
      bind:value={binding.worker}
    />
  </div>

  <!-- Run-time prompt material appended after each slot's charter -->
  <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
    <div>
      <label class="label py-1" for="{idPrefix}-sdlc-worker-append">
        <span class="label-text text-xs">Append to the worker's prompt</span>
      </label>
      <textarea
        id="{idPrefix}-sdlc-worker-append"
        class="textarea textarea-bordered textarea-sm w-full font-mono"
        rows="3"
        placeholder="This project's own conventions, read after the worker charter"
        bind:value={binding.workerPromptAppend}
      ></textarea>
    </div>
    <div>
      <label class="label py-1" for="{idPrefix}-sdlc-triage-append">
        <span class="label-text text-xs">Append to triage's prompt</span>
      </label>
      <textarea
        id="{idPrefix}-sdlc-triage-append"
        class="textarea textarea-bordered textarea-sm w-full font-mono"
        rows="3"
        placeholder="What triage should also know about this repository"
        bind:value={binding.triagePromptAppend}
      ></textarea>
    </div>
  </div>
  <p class="text-xs text-base-content/40 -mt-1">
    Added after the role's charter, never instead of it. At most 64 KiB each.
  </p>

  <!-- Verification -->
  <div>
    <div class="flex items-center justify-between">
      <span class="label-text text-xs">Verification Commands</span>
      <button
        class="btn btn-xs btn-ghost gap-1"
        type="button"
        onclick={addVerification}
        aria-label="Add verification command"
      >
        <Plus class="h-3 w-3" />
        Add
      </button>
    </div>
    <p class="text-xs text-base-content/40 mt-1 mb-2">
      Run in the worker's checkout, in order, before the work is offered for review. Empty means
      the forge's own checks are the only gate.
    </p>
    {#if binding.verification.length === 0}
      <p class="text-xs text-base-content/30">No verification commands.</p>
    {:else}
      <div class="flex flex-col gap-2">
        {#each binding.verification as _command, i (i)}
          <div class="flex items-center gap-2">
            <input
              id="{idPrefix}-sdlc-verification-{i}"
              type="text"
              class="input input-bordered input-xs w-full font-mono"
              placeholder="make test"
              aria-label="Verification command {i + 1}"
              bind:value={binding.verification[i]}
            />
            <button
              class="btn btn-xs btn-ghost btn-square text-error hover:bg-error/10"
              type="button"
              onclick={() => { removeVerification(i); }}
              aria-label="Remove verification command {i + 1}"
            >
              <Trash2 class="h-3 w-3" />
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Review policy -->
  <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
    <div>
      <label class="label py-1" for="{idPrefix}-sdlc-required-approvals">
        <span class="label-text text-xs">Required Approvals</span>
      </label>
      <input
        id="{idPrefix}-sdlc-required-approvals"
        type="number"
        min="1"
        class="input input-bordered input-sm w-full"
        bind:value={binding.requiredApprovals}
      />
    </div>
    <div>
      <label class="label py-1" for="{idPrefix}-sdlc-checks-settle-window">
        <span class="label-text text-xs">Checks Settle Window (s)</span>
      </label>
      <input
        id="{idPrefix}-sdlc-checks-settle-window"
        type="number"
        min="0"
        class="input input-bordered input-sm w-full"
        placeholder="0 = default"
        bind:value={binding.checksSettleWindowSeconds}
      />
    </div>
    <div>
      <label class="label py-1" for="{idPrefix}-sdlc-max-review-rounds">
        <span class="label-text text-xs">Max Review Rounds</span>
      </label>
      <input
        id="{idPrefix}-sdlc-max-review-rounds"
        type="number"
        min="0"
        class="input input-bordered input-sm w-full"
        placeholder="0 = default (3)"
        bind:value={binding.maxReviewRounds}
      />
    </div>
  </div>

  <label
    class="label cursor-pointer justify-start gap-3 py-1"
    for="{idPrefix}-sdlc-require-independent-reviewer"
  >
    <input
      id="{idPrefix}-sdlc-require-independent-reviewer"
      type="checkbox"
      class="toggle toggle-sm toggle-primary"
      bind:checked={binding.requireIndependentReviewer}
    />
    <span class="label-text text-xs">Refuse approvals from the model family that wrote the code</span>
  </label>

  <label class="label cursor-pointer justify-start gap-3 py-1" for="{idPrefix}-sdlc-require-green-checks">
    <input
      id="{idPrefix}-sdlc-require-green-checks"
      type="checkbox"
      class="toggle toggle-sm toggle-primary"
      bind:checked={binding.requireGreenChecks}
    />
    <span class="label-text text-xs">Refuse a merge until every check on the head is green</span>
  </label>

  <!-- Reviewer profiles: who reviews, and whose verdict counts -->
  <SdlcReviewerProfiles {idPrefix} {tenantID} bind:profiles={binding.reviewerProfiles} />

  <!-- Reviewer model families -->
  <div>
    <div class="flex items-center justify-between">
      <span class="label-text text-xs">Reviewer model families</span>
      <button
        class="btn btn-xs btn-ghost gap-1"
        type="button"
        onclick={addReviewer}
        aria-label="Add reviewer"
      >
        <Plus class="h-3 w-3" />
        Add
      </button>
    </div>
    <p class="text-xs text-base-content/40 mt-1 mb-2">
      Maps a forge login to the model family behind it, for the independence rule. A reviewer
      profile's family is derived from its backend and model; add a row only to state it outright.
    </p>
    {#if binding.reviewers.length === 0}
      <p class="text-xs text-base-content/30">No reviewers mapped.</p>
    {:else}
      <div class="flex flex-col gap-2">
        {#each binding.reviewers as reviewer, i (reviewer)}
          <div class="flex items-center gap-2">
            <input
              id="{idPrefix}-sdlc-reviewer-login-{i}"
              type="text"
              class="input input-bordered input-xs w-full font-mono"
              placeholder="codex"
              aria-label="Reviewer {i + 1} login"
              bind:value={reviewer.login}
            />
            <input
              id="{idPrefix}-sdlc-reviewer-family-{i}"
              type="text"
              class="input input-bordered input-xs w-full font-mono"
              placeholder="gpt"
              aria-label="Reviewer {i + 1} model family"
              bind:value={reviewer.family}
            />
            <button
              class="btn btn-xs btn-ghost btn-square text-error hover:bg-error/10"
              type="button"
              onclick={() => { removeReviewer(i); }}
              aria-label="Remove reviewer {i + 1}"
            >
              <Trash2 class="h-3 w-3" />
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Rulers -->
  <SdlcRulerPicker
    {idPrefix}
    bind:rulers={binding.rulers}
    owner={binding.owner}
    {collaborators}
    status={collaboratorsStatus}
    error={collaboratorsError}
  />

  <!-- Display labels -->
  <div>
    <div class="flex items-center justify-between">
      <span class="label-text text-xs">Display Labels</span>
      <button
        class="btn btn-xs btn-ghost gap-1"
        type="button"
        onclick={addLabel}
        aria-label="Add display label"
      >
        <Plus class="h-3 w-3" />
        Add
      </button>
    </div>
    <p class="text-xs text-base-content/40 mt-1 mb-2">
      The forge labels that show where an issue's work stands. Display output, never a lock.
    </p>
    {#if binding.labels.length === 0}
      <p class="text-xs text-base-content/30">No display labels.</p>
    {:else}
      <div class="flex flex-col gap-2">
        {#each binding.labels as row, i (row)}
          <div class="flex items-center gap-2">
            <select
              id="{idPrefix}-sdlc-label-key-{i}"
              class="select select-bordered select-xs w-full"
              aria-label="Label {i + 1} key"
              bind:value={row.key}
            >
              {#each LABEL_KEYS as key (key)}
                <option value={key}>{key}</option>
              {/each}
            </select>
            <input
              id="{idPrefix}-sdlc-label-value-{i}"
              type="text"
              class="input input-bordered input-xs w-full font-mono"
              placeholder="status/in-progress"
              aria-label="Label {i + 1} forge label"
              bind:value={row.label}
            />
            <button
              class="btn btn-xs btn-ghost btn-square text-error hover:bg-error/10"
              type="button"
              onclick={() => { removeLabel(i); }}
              aria-label="Remove display label {i + 1}"
            >
              <Trash2 class="h-3 w-3" />
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Ranking -->
  <details class="collapse collapse-arrow bg-base-200 rounded-box">
    <summary class="collapse-title text-xs font-semibold py-2 min-h-0">Ranking</summary>
    <div class="collapse-content flex flex-col gap-3 pt-1">
      <div>
        <label class="label py-1" for="{idPrefix}-sdlc-ranking-priority">
          <span class="label-text text-xs">Default Priority</span>
        </label>
        <input
          id="{idPrefix}-sdlc-ranking-priority"
          type="number"
          class="input input-bordered input-sm w-full"
          placeholder="0 = no shift"
          bind:value={binding.rankingDefaultPriority}
        />
      </div>
      <div>
        <label class="label py-1" for="{idPrefix}-sdlc-ranking-analyzer">
          <span class="label-text text-xs">Dependency Analyzer</span>
        </label>
        <input
          id="{idPrefix}-sdlc-ranking-analyzer"
          type="text"
          class="input input-bordered input-sm w-full font-mono"
          placeholder="Empty = registered precedence"
          bind:value={binding.rankingAnalyzerName}
        />
      </div>
      <div>
        <label class="label py-1" for="{idPrefix}-sdlc-ranking-dependents">
          <span class="label-text text-xs">Declared Dependents</span>
        </label>
        <input
          id="{idPrefix}-sdlc-ranking-dependents"
          type="text"
          class="input input-bordered input-sm w-full font-mono"
          placeholder="go-xkms, go-quicbaas"
          bind:value={binding.rankingDeclaredDependents}
        />
        <p class="text-xs text-base-content/40 mt-1">Comma-separated project names.</p>
      </div>
    </div>
  </details>
</div>
