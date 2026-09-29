<!--
  Copyright (c) 2026 Jeremy Hahn
  Copyright (c) 2026 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  SdlcReviewerProfiles edits a binding's reviewer profiles: the default that
  reviews every pull request, and the alternates an operator may pick for one.

  The merge role is the thing this list exists to make legible, so every row
  says it in words — "Counts toward merge" or "Advisory" — beside the status
  context the profile publishes under. An advisory reviewer that looked like a
  counting one is how a change would be believed reviewed when it was not.
-->
<script lang="ts">
  import {
    emptyReviewerProfile,
    profileName,
    profileStatusContext,
    type ReviewerProfileRow,
  } from '../../lib/sdlcBinding.ts';
  import { Pencil, Plus, Star, Trash2 } from 'lucide-svelte';
  import RefPicker from '../core/RefPicker.svelte';

  let {
    idPrefix,
    profiles = $bindable(),
    tenantID,
  }: { idPrefix: string; profiles: ReviewerProfileRow[]; tenantID?: string } = $props();

  // The one profile open for editing. A new profile opens itself: a row of
  // empty fields collapsed into a summary would read as a finished profile.
  let open = $state<number | null>(null);

  function addProfile(): void {
    const row = emptyReviewerProfile();
    // The first profile IS the default, and a default counts unless told
    // otherwise — exactly what a binding with one reviewer always meant.
    if (profiles.length === 0) row.countsTowardMerge = true;
    profiles = [...profiles, row];
    open = profiles.length - 1;
  }

  function removeProfile(index: number): void {
    profiles = profiles.filter((_, i) => i !== index);
    open = null;
  }

  /**
   * makeDefault moves a profile to the front. Its merge role moves with it:
   * an advisory alternate stays advisory as the default until someone turns
   * it on, because a reorder is not a decision about who may land code.
   */
  function makeDefault(index: number): void {
    const row = profiles[index];
    if (!row) return;
    profiles = [row, ...profiles.filter((_, i) => i !== index)];
    open = null;
  }

  function summary(row: ReviewerProfileRow): string {
    const agent = row.agent.trim();
    return row.login.trim() ? `${agent} as ${row.login}` : agent;
  }
</script>

<div data-testid="reviewer-profiles">
  <div class="flex items-center justify-between">
    <span class="label-text text-xs">Reviewer profiles</span>
    <button class="btn btn-xs btn-ghost gap-1" type="button" onclick={addProfile}>
      <Plus class="h-3 w-3" />
      Add profile
    </button>
  </div>
  <p class="text-xs text-base-content/40 mt-1 mb-2">
    The first profile reviews every pull request unless an operator picks another one for it. Only a
    profile that counts toward merge can land or hold back a change; an advisory profile's verdict
    is posted and shown, never counted.
  </p>

  {#if profiles.length === 0}
    <p class="text-xs text-base-content/30">
      Not self-reviewed. Add a profile to have an agent review this project's pull requests.
    </p>
  {:else}
    <ul class="flex flex-col gap-2">
      {#each profiles as profile, i (profile)}
        {@const name = profileName(profile, i) || 'unnamed'}
        <li class="rounded-box border border-base-300 p-3" data-testid="reviewer-profile">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0 space-y-1">
              <div class="flex flex-wrap items-center gap-2">
                <span class="font-mono text-sm font-semibold">{name}</span>
                {#if i === 0}
                  <span class="badge badge-xs badge-primary">Default</span>
                {/if}
                {#if profile.countsTowardMerge}
                  <span class="badge badge-xs badge-success">Counts toward merge</span>
                {:else}
                  <span class="badge badge-xs badge-ghost">Advisory</span>
                {/if}
              </div>
              <div class="text-xs text-base-content/60 truncate">
                {summary(profile) || 'No agent yet'}
              </div>
              <div class="text-xs text-base-content/40">
                Publishes <code class="font-mono">{profileStatusContext(profiles, i)}</code>
              </div>
            </div>
            <div class="flex shrink-0 items-center gap-1">
              {#if i > 0}
                <button
                  class="btn btn-xs btn-ghost gap-1"
                  type="button"
                  onclick={() => { makeDefault(i); }}
                  aria-label="Make {name} the default reviewer"
                >
                  <Star class="h-3 w-3" />
                  Make default
                </button>
              {/if}
              <button
                class="btn btn-xs btn-ghost btn-square"
                type="button"
                onclick={() => { open = open === i ? null : i; }}
                aria-label="{open === i ? 'Close' : 'Edit'} {name}"
                aria-expanded={open === i}
              >
                <Pencil class="h-3 w-3" />
              </button>
              <button
                class="btn btn-xs btn-ghost btn-square text-error hover:bg-error/10"
                type="button"
                onclick={() => { removeProfile(i); }}
                aria-label="Remove {name}"
              >
                <Trash2 class="h-3 w-3" />
              </button>
            </div>
          </div>

          {#if open === i}
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-3">
              <div>
                <label class="label py-1" for="{idPrefix}-reviewer-{i}-name">
                  <span class="label-text text-xs">Name {#if i > 0}<span class="text-error">*</span>{/if}</span>
                </label>
                <input
                  id="{idPrefix}-reviewer-{i}-name"
                  type="text"
                  class="input input-bordered input-sm w-full font-mono"
                  placeholder={i === 0 ? 'default' : 'qwen'}
                  bind:value={profile.name}
                />
              </div>
              <div>
                <label class="label py-1" for="{idPrefix}-reviewer-{i}-agent">
                  <span class="label-text text-xs">Agent <span class="text-error">*</span></span>
                </label>
                <input
                  id="{idPrefix}-reviewer-{i}-agent"
                  type="text"
                  class="input input-bordered input-sm w-full font-mono"
                  placeholder="codex-reviewer"
                  bind:value={profile.agent}
                />
              </div>
              <div>
                <label class="label py-1" for="{idPrefix}-reviewer-{i}-login">
                  <span class="label-text text-xs">Forge login <span class="text-error">*</span></span>
                </label>
                <input
                  id="{idPrefix}-reviewer-{i}-login"
                  type="text"
                  class="input input-bordered input-sm w-full font-mono"
                  placeholder="codex"
                  bind:value={profile.login}
                />
              </div>
              <div>
                <label class="label py-1" for="{idPrefix}-reviewer-{i}-token">
                  <span class="label-text text-xs">Token secret ref <span class="text-error">*</span></span>
                </label>
                <RefPicker
                  id="{idPrefix}-reviewer-{i}-token"
                  kind="secret"
                  {tenantID}
                  placeholder="sdlc/reviewer/repo/token"
                  bind:value={profile.tokenSecretRef}
                />
              </div>
              <div>
                <label class="label py-1" for="{idPrefix}-reviewer-{i}-endpoint">
                  <span class="label-text text-xs">Model server</span>
                </label>
                <input
                  id="{idPrefix}-reviewer-{i}-endpoint"
                  type="url"
                  class="input input-bordered input-sm w-full font-mono"
                  placeholder="http://gpu-host:11434 (opencode on ollama/… only)"
                  bind:value={profile.endpoint}
                />
              </div>
              <div class="sm:col-span-2">
                <label class="label py-1" for="{idPrefix}-reviewer-{i}-append">
                  <span class="label-text text-xs">Append to the reviewer's prompt</span>
                </label>
                <textarea
                  id="{idPrefix}-reviewer-{i}-append"
                  class="textarea textarea-bordered textarea-sm w-full font-mono"
                  rows="2"
                  placeholder="What this project's reviews must also check, after the reviewer charter"
                  bind:value={profile.promptAppend}
                ></textarea>
              </div>
              <div>
                <label class="label py-1" for="{idPrefix}-reviewer-{i}-context">
                  <span class="label-text text-xs">Status context</span>
                </label>
                <input
                  id="{idPrefix}-reviewer-{i}-context"
                  type="text"
                  class="input input-bordered input-sm w-full font-mono"
                  placeholder={profileStatusContext(profiles, i)}
                  disabled={i > 0 && profile.countsTowardMerge}
                  title={i > 0 && profile.countsTowardMerge
                    ? 'A profile that counts toward merge publishes under the required context.'
                    : ''}
                  bind:value={profile.statusContext}
                />
              </div>
              <label
                class="label cursor-pointer justify-start gap-3 py-1 sm:col-span-2"
                for="{idPrefix}-reviewer-{i}-counts"
              >
                <input
                  id="{idPrefix}-reviewer-{i}-counts"
                  type="checkbox"
                  class="toggle toggle-sm toggle-primary"
                  bind:checked={profile.countsTowardMerge}
                />
                <span class="label-text text-xs">
                  Counts toward merge: this profile's approval can land a change and its refusal can
                  hold one back
                </span>
              </label>
            </div>
            <p class="text-xs text-base-content/40 pt-2">
              Each profile posts as its own forge account with its own token, so its verdicts can be
              told apart from every other reviewer's.
            </p>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>
