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
  SdlcReviewerPicker chooses which of the project's reviewer profiles reviews
  one pull request. Picking a profile records an operator override on the SDLC
  module; picking "Project default" clears it. Nothing here touches the pull
  request itself: a reviewer chosen by label is exactly what the module refuses
  to read.

  It renders nothing where there is nothing to choose: no SDLC module on this
  node, a project that is not bound, or a binding with no reviewer.
-->
<script lang="ts">
  import type { ReviewerSelection } from '@quicbaas/sdk';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import {
    PROJECT_DEFAULT,
    reviewerOptions,
    selectedValue,
    type ReviewerOption,
  } from '../../lib/sdlcReviewer.ts';

  let {
    tenantId,
    projectId,
    pullRequest,
  }: { tenantId: string; projectId: string; pullRequest: number } = $props();

  let options = $state<ReviewerOption[]>([]);
  let selection = $state<ReviewerSelection | null>(null);
  let value = $state(PROJECT_DEFAULT);
  let busy = $state(false);
  let error = $state('');

  function message(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
  }

  async function load(): Promise<void> {
    error = '';
    try {
      const client = await getSDKClient();
      const sdlc = client.sdlc;
      if (!sdlc) return;
      const binding = await sdlc.binding(tenantId, projectId);
      options = reviewerOptions(binding);
      if (options.length === 0) return;
      selection = await sdlc.reviewer(tenantId, projectId, pullRequest);
      value = selectedValue(selection);
    } catch (err: unknown) {
      // A project that is not bound, or a node without the module, has no
      // reviewer to choose; anything else is reported, not swallowed.
      const status = (err as { status?: unknown } | null)?.status;
      if (status === 404 || status === 501 || status === 503) {
        options = [];
        return;
      }
      error = `Could not read the reviewer: ${message(err)}`;
    }
  }

  async function choose(next: string): Promise<void> {
    busy = true;
    error = '';
    try {
      const client = await getSDKClient();
      const sdlc = client.sdlc;
      if (!sdlc) return;
      selection =
        next === PROJECT_DEFAULT
          ? await sdlc.clearReviewer(tenantId, projectId, pullRequest)
          : await sdlc.setReviewer(tenantId, projectId, pullRequest, next);
      value = selectedValue(selection);
      // Switching reviewer never cancels a running review: it finishes, and
      // every later review of this pull request is by the chosen profile.
      showToast(
        'success',
        selection.redispatched
          ? `#${pullRequest} is being reviewed by ${selection.profile} now`
          : `#${pullRequest} will be reviewed by ${selection.profile}; any review already running finishes first`,
      );
    } catch (err: unknown) {
      value = selectedValue(selection);
      error = `Could not change the reviewer: ${message(err)}`;
    } finally {
      busy = false;
    }
  }

  $effect(() => {
    void [tenantId, projectId, pullRequest];
    void load();
  });
</script>

{#if options.length > 0}
  <div class="space-y-1" data-testid="reviewer-picker">
    <label class="text-xs font-medium text-base-content/60" for="reviewer-picker-{pullRequest}">
      Reviewer
    </label>
    <div class="flex flex-wrap items-center gap-2">
      <select
        id="reviewer-picker-{pullRequest}"
        class="select select-bordered select-xs"
        disabled={busy}
        bind:value
        onchange={() => void choose(value)}
      >
        {#each options as option (option.value)}
          <option value={option.value}>{option.label}</option>
        {/each}
      </select>
      {#if selection}
        {#if selection.counts_toward_merge}
          <span class="badge badge-xs badge-success">Counts toward merge</span>
        {:else}
          <span class="badge badge-xs badge-ghost">Advisory</span>
        {/if}
        <span class="text-xs text-base-content/40">
          {selection.overridden ? 'Chosen for this pull request' : 'Project default'}
        </span>
      {/if}
    </div>
    {#if selection && !selection.counts_toward_merge}
      <p class="text-xs text-base-content/40">
        An advisory verdict is posted and shown; it cannot land this change or hold it back.
      </p>
    {/if}
    {#if error}
      <p class="text-xs text-error">{error}</p>
    {/if}
  </div>
{/if}
