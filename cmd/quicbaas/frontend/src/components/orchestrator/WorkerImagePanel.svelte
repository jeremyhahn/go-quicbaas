<!--
  WorkerImagePanel — the Software Factory worker image, as the refresh loop
  last saw it (go-quicbaas#1021).

  Shows each bound image with the Claude Code and Codex versions it carries,
  the latest versions npm published when the loop last checked, when that was
  and how it ended — and "Check for updates now", which asks the daemon for a
  pass and follows it to its outcome. The daemon checks once a day by itself;
  this button is for the day a CLI ships.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import StatusBadge from '../core/StatusBadge.svelte';
  import { formatDateTime, formatRelative } from '../../lib/time.ts';
  import { agentsLabel, loopBadge, outcomeBadge, triggerLabel } from '../../lib/workerImage.ts';
  import {
    workerImageStore,
    watchWorkerImageStatus,
    checkForUpdates,
    DEFAULT_POLL_MS,
    DEFAULT_IDLE_POLL_MS,
  } from '../../stores/workerImage.svelte.ts';

  // pollMs is how often a running pass is polled, idlePollMs how often the
  // status is re-read while none runs; tests shorten both.
  let { pollMs = DEFAULT_POLL_MS, idlePollMs = DEFAULT_IDLE_POLL_MS }: { pollMs?: number; idlePollMs?: number } = $props();

  // Watch for as long as the panel is mounted, so a pass started anywhere —
  // including one already running when the panel opened — is followed to its
  // outcome without the button. The returned stop runs on unmount.
  onMount(() => watchWorkerImageStatus({ pollMs, idlePollMs }));

  const status = $derived(workerImageStore.status);
  const last = $derived(status?.last);
  const loop = $derived(loopBadge(status));
  const outcome = $derived(outcomeBadge(last?.outcome));
  const busy = $derived(workerImageStore.checking || !!status?.running || !!status?.queued);
  const canCheck = $derived(!!status?.serving && !busy);
</script>

<section class="card bg-base-200 rounded-box" data-testid="worker-image-panel" aria-labelledby="worker-image-title">
  <div class="card-body p-4 gap-3">
    <div class="flex items-center justify-between gap-3 flex-wrap">
      <div class="flex items-center gap-2">
        <h2 id="worker-image-title" class="text-base font-semibold">Worker image</h2>
        <StatusBadge text={loop.text} variant={loop.variant} pulse={!!status?.running} />
      </div>
      <button
        class="btn btn-sm btn-primary"
        onclick={() => { void checkForUpdates({ pollMs }); }}
        disabled={!canCheck}
        data-testid="worker-image-check"
      >
        {#if busy}
          <span class="loading loading-spinner loading-xs"></span>
          Checking…
        {:else}
          Check for updates now
        {/if}
      </button>
    </div>

    {#if workerImageStore.error}
      <p class="text-sm text-error" role="alert" data-testid="worker-image-error">
        Could not read the worker image status: {workerImageStore.error}
      </p>
    {:else if workerImageStore.loading && !status}
      <p class="text-sm text-base-content/60">Loading the worker image status…</p>
    {:else if !status?.serving}
      <p class="text-sm text-base-content/60" data-testid="worker-image-not-serving">
        The worker image refresh is not running on this node. It needs the SDLC module and a container engine.
      </p>
    {:else if !last}
      <p class="text-sm text-base-content/60" data-testid="worker-image-no-pass">
        No check has run since the daemon started. It checks once a day; check now to see where the bound images stand.
      </p>
    {:else}
      <dl class="grid grid-cols-1 md:grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
        <dt class="text-base-content/60">Last check</dt>
        <dd class="flex items-center gap-2 flex-wrap" data-testid="worker-image-last">
          <StatusBadge text={outcome.text} variant={outcome.variant} />
          <span title={formatDateTime(last.finished_at)}>{formatRelative(last.finished_at)}</span>
          <span class="text-base-content/60">({triggerLabel(last.trigger)})</span>
        </dd>

        <dt class="text-base-content/60">Latest on npm</dt>
        <dd data-testid="worker-image-latest">{agentsLabel(last.latest)}</dd>

        {#if last.reference}
          <dt class="text-base-content/60">Bound to</dt>
          <dd class="font-mono text-xs break-all" data-testid="worker-image-reference">{last.reference}</dd>
        {/if}
      </dl>

      {#if last.bound && last.bound.length > 0}
        <ul class="flex flex-col gap-1" data-testid="worker-image-bound">
          {#each last.bound as bound (bound.image)}
            <li class="flex items-center justify-between gap-2 flex-wrap text-sm bg-base-100 rounded-box px-3 py-2">
              <span class="font-mono text-xs break-all">{bound.image}</span>
              <span class="flex items-center gap-2">
                <span class="text-base-content/70">{agentsLabel(bound.agents)}</span>
                <span class="text-base-content/50 text-xs">{bound.uses} {bound.uses === 1 ? 'use' : 'uses'}</span>
                <StatusBadge text={bound.stale ? 'Behind' : 'Current'} variant={bound.stale ? 'warning' : 'success'} />
              </span>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="text-sm text-base-content/60">No project binding names a managed worker image.</p>
      {/if}

      {#if last.error}
        <p class="text-sm text-error" data-testid="worker-image-pass-error">{last.error}</p>
      {/if}
    {/if}

    {#if workerImageStore.checking}
      <p class="text-sm text-base-content/70" aria-live="polite" data-testid="worker-image-progress">
        Checking npm for newer CLIs. If there are any, a new image is built and tested before any project moves to it —
        this can take several minutes.
      </p>
    {/if}
    {#if workerImageStore.checkError}
      <p class="text-sm text-warning" role="status" data-testid="worker-image-check-error">{workerImageStore.checkError}</p>
    {/if}
  </div>
</section>
