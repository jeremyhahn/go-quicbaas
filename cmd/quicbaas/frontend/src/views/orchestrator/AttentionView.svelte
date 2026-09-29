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
  AttentionView is the "Needs you" inbox: one card per thing only an
  operator can clear — a parked task or an environmental alert. A parked
  task's card shows its project, its linked forge issue, how long it has
  waited, the worker's question and recommendation, and the resolution
  buttons; an environmental alert's card shows its message and severity —
  it carries no resolution button, because the operator's action is outside
  the factory (repair the agent, rotate the credential, unseal the barrier).
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import type { AttentionItem } from '@quicbaas/sdk';
  import { attentionStore, refreshAttention, resolveAttentionItem } from '../../stores/attentionStore.svelte.ts';
  import { routeTaskDetail } from '../../stores/router.svelte.ts';
  import { themeStore } from '../../stores/theme.svelte.ts';
  import { STATUS_TONES } from '../../lib/theme/tokens.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import {
    itemAgeLabel,
    isEnvironmentalAlert,
    isParkedTask,
    kindLabel,
    blockKindLabel,
    conditionLabel,
    severityVariant,
    actionLabel,
    primaryAction,
    secondaryActions,
    takesText,
    submitLabel,
    hasIssueLink,
    filterAttention,
  } from '../../lib/attention.ts';
  import { Inbox, RefreshCw, ExternalLink, CheckCircle2 } from 'lucide-svelte';

  let projectFilter = $state('');
  let kindFilter = $state('');
  /** taskID whose text box is open, and the text action it will post. */
  let answering: string | null = $state(null);
  let answerAction: string = $state('answer');
  let answerText: string = $state('');

  onMount(() => {
    void load();
  });

  async function load(): Promise<void> {
    await refreshAttention();
  }

  /** The cards shown: the store's complete set, narrowed by the dropdowns. */
  const shown = $derived(filterAttention(attentionStore.items, { projectID: projectFilter, kind: kindFilter }));

  function projectOptions(): string[] {
    return Object.keys(attentionStore.facets['project_id'] ?? {}).sort();
  }

  function kindOptions(): string[] {
    return Object.keys(attentionStore.facets['kind'] ?? {}).sort();
  }

  async function act(item: AttentionItem, action: string, answer?: string): Promise<void> {
    const ok = await resolveAttentionItem(item.id, action, answer);
    if (ok && answering === item.id) {
      answering = null;
      answerText = '';
    }
  }

  function openAnswer(id: string, action: string): void {
    answering = id;
    answerAction = action;
    answerText = '';
  }

  /** choose performs an action, or opens the text box for one that needs words. */
  function choose(item: AttentionItem, action: string): void {
    if (takesText(action)) {
      openAnswer(item.id, action);
      return;
    }
    void act(item, action);
  }

  const primaryTone = $derived(STATUS_TONES[themeStore.theme === 'light' ? 'light' : 'dark'].primary);
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Needs you" subtitle="Everything the factory has stopped on and cannot start again by itself" icon={Inbox}>
    {#snippet children()}
      <button class="btn btn-ghost btn-sm gap-2" onclick={load} disabled={attentionStore.loading} data-testid="attention-refresh">
        <RefreshCw class="h-4 w-4 {attentionStore.loading ? 'animate-spin' : ''}" />
        Refresh
      </button>
    {/snippet}
  </PageHeader>

  <div class="flex flex-wrap items-center gap-3">
    <select class="select select-sm select-bordered" bind:value={projectFilter} data-testid="attention-filter-project">
      <option value="">All projects</option>
      {#each projectOptions() as p (p)}
        <option value={p}>{p}</option>
      {/each}
    </select>
    <select class="select select-sm select-bordered" bind:value={kindFilter} data-testid="attention-filter-kind">
      <option value="">All kinds</option>
      {#each kindOptions() as k (k)}
        <option value={k}>{kindLabel(k)}</option>
      {/each}
    </select>
    <span class="text-sm text-base-content/60" data-testid="attention-count">{shown.length} item{shown.length === 1 ? '' : 's'}</span>
  </div>

  {#if attentionStore.loading && attentionStore.items.length === 0}
    <div class="flex justify-center py-16">
      <span class="loading loading-spinner loading-lg"></span>
    </div>
  {:else if shown.length === 0 && attentionStore.items.length > 0}
    <EmptyState
      icon={CheckCircle2}
      title="Nothing matches this filter"
      description="Other items still need you; clear the project or kind filter to see them."
    />
  {:else if shown.length === 0}
    <EmptyState
      icon={CheckCircle2}
      title="Nothing needs you right now"
      description="Every task is moving on its own and no environmental alert is active."
    />
  {:else}
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      {#each shown as item (item.id)}
        <div class="card bg-base-200 rounded-box border border-base-300" data-testid="attention-card">
          <div class="card-body p-5 gap-3">
            <div class="flex items-start justify-between gap-2">
              <div>
                <div class="flex items-center gap-2 flex-wrap">
                  <StatusBadge text={kindLabel(item.kind)} variant={isEnvironmentalAlert(item) ? severityVariant(item.severity) : 'ghost'} />
                  {#if item.block_kind}
                    <StatusBadge text={blockKindLabel(item.block_kind)} variant="warning" />
                  {/if}
                  {#if item.ruling_posted}
                    <span data-testid="attention-ruling-posted">
                      <StatusBadge text="Ruling posted" variant="success" />
                    </span>
                  {/if}
                </div>
                <h3 class="font-semibold text-base-content mt-1">{item.subject}</h3>
                {#if item.project_name}
                  <p class="text-xs text-base-content/60">{item.project_name}</p>
                {/if}
              </div>
              <span class="text-xs text-base-content/50 whitespace-nowrap" data-testid="attention-age">
                waiting {itemAgeLabel(item)}
              </span>
            </div>

            {#if hasIssueLink(item)}
              <a href={item.issue_url} target="_blank" rel="noopener noreferrer" class="link link-primary text-sm flex items-center gap-1 w-fit">
                {item.issue_ref}
                <ExternalLink class="h-3 w-3" />
              </a>
            {/if}

            {#if item.condition}
              <p class="text-xs text-base-content/50">{conditionLabel(item.condition)}</p>
            {/if}

            {#if isParkedTask(item)}
              {#if item.question}
                <p class="text-sm text-base-content/80"><span class="font-medium">Question:</span> {item.question}</p>
              {/if}
              {#if item.recommendation}
                <div class="rounded-lg p-3" style:background-color={primaryTone.bg}>
                  <p class="text-xs font-semibold mb-1" style:color={primaryTone.fg}>Recommendation</p>
                  <p class="text-sm text-base-content">{item.recommendation}{item.truncated ? '…' : ''}</p>
                </div>
              {/if}

              {#if answering === item.id}
                <div class="space-y-2">
                  <textarea
                    class="textarea textarea-bordered textarea-sm w-full"
                    placeholder={answerAction === 'note' ? 'Your note — the task stays parked…' : 'Your ruling…'}
                    bind:value={answerText}
                    data-testid="attention-answer-text"
                  ></textarea>
                  <div class="flex gap-2">
                    <button
                      class="btn btn-primary btn-sm"
                      disabled={answerText.trim() === '' || attentionStore.resolvingID === item.id}
                      onclick={() => act(item, answerAction, answerText)}
                      data-testid="attention-answer-submit"
                    >
                      {submitLabel(answerAction)}
                    </button>
                    <button class="btn btn-ghost btn-sm" onclick={() => { answering = null; }}>Cancel</button>
                  </div>
                </div>
              {:else}
                <div class="flex flex-wrap gap-2 pt-1">
                  {#if primaryAction(item)}
                    {@const primary = primaryAction(item)!}
                    <button
                      class="btn btn-primary btn-sm"
                      disabled={attentionStore.resolvingID === item.id}
                      onclick={() => choose(item, primary)}
                      data-testid="attention-primary-action"
                    >
                      {actionLabel(primary)}
                    </button>
                  {/if}
                  {#each secondaryActions(item) as action (action)}
                    <button
                      class="btn btn-ghost btn-sm"
                      disabled={attentionStore.resolvingID === item.id}
                      onclick={() => choose(item, action)}
                      data-testid={`attention-action-${action}`}
                    >
                      {actionLabel(action)}
                    </button>
                  {/each}
                  <button
                    class="btn btn-ghost btn-sm ml-auto"
                    onclick={() => routeTaskDetail(item.id)}
                  >
                    View task
                  </button>
                </div>
              {/if}
            {:else if item.message}
              <p class="text-sm text-base-content/80">{item.message}</p>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
