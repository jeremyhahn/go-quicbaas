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
  SdlcFileIssue files an issue on a bound project's repository through the
  binding's own forge token — how the lead splits an issue into follow-ups
  without leaving QuicBaaS. Every filing is audited server-side.
-->
<script lang="ts">
  import type { FileIssueRequest, ForgeIssue } from '@quicbaas/sdk';
  import {
    emptyFileIssueForm,
    fileIssueRequestFrom,
    validateFileIssueForm,
    MAX_TITLE,
  } from '../../lib/sdlcIssue.ts';
  import { FilePlus } from 'lucide-svelte';

  let {
    idPrefix,
    triggerLabel,
    onFile,
  }: {
    idPrefix: string;
    triggerLabel: string;
    onFile: (req: FileIssueRequest) => Promise<ForgeIssue>;
  } = $props();

  let open = $state(false);
  let busy = $state(false);
  let error = $state('');
  let filed = $state<ForgeIssue | null>(null);
  let form = $state(emptyFileIssueForm());

  async function submit(): Promise<void> {
    const invalid = validateFileIssueForm(form);
    if (invalid !== '') {
      error = invalid;
      return;
    }
    busy = true;
    error = '';
    try {
      filed = await onFile(fileIssueRequestFrom(form, triggerLabel));
      form = emptyFileIssueForm();
      open = false;
    } catch (err: unknown) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<div class="rounded-box bg-base-100 p-3 flex flex-col gap-2" data-testid="sdlc-file-issue">
  <div class="flex items-center gap-2">
    <span class="text-xs font-semibold">Issues</span>
    {#if filed}
      <span class="text-xs" data-testid="sdlc-filed-issue">
        Filed
        {#if filed.url}
          <a class="link" href={filed.url} target="_blank" rel="noreferrer">#{filed.number}</a>
        {:else}
          #{filed.number}
        {/if}
      </span>
    {/if}
    <span class="grow"></span>
    {#if !open}
      <button class="btn btn-xs btn-ghost" type="button" onclick={() => { open = true; filed = null; }}>
        <FilePlus class="w-3 h-3" /> File issue
      </button>
    {/if}
  </div>

  {#if open}
    <input
      id="{idPrefix}-sdlc-issue-title"
      type="text"
      class="input input-bordered input-sm w-full"
      placeholder="Title"
      maxlength={MAX_TITLE}
      aria-label="Issue title"
      bind:value={form.title}
    />
    <textarea
      id="{idPrefix}-sdlc-issue-body"
      class="textarea textarea-bordered textarea-sm w-full"
      rows="4"
      placeholder="Description (optional)"
      aria-label="Issue body"
      bind:value={form.body}
    ></textarea>
    <div class="flex items-center gap-3 flex-wrap">
      <label class="label cursor-pointer gap-2 py-0" for="{idPrefix}-sdlc-issue-ready">
        <input
          id="{idPrefix}-sdlc-issue-ready"
          type="checkbox"
          class="checkbox checkbox-xs"
          bind:checked={form.handToFactory}
          disabled={triggerLabel.trim() === ''}
        />
        <span class="label-text text-xs">Hand to the factory ({triggerLabel || 'no trigger label'})</span>
      </label>
      <input
        id="{idPrefix}-sdlc-issue-parent"
        type="text"
        inputmode="numeric"
        class="input input-bordered input-xs w-28"
        placeholder="Parent #"
        aria-label="Parent issue number"
        bind:value={form.parent}
      />
    </div>
    {#if error}
      <p class="text-xs text-error" data-testid="sdlc-file-issue-error">{error}</p>
    {/if}
    <div class="flex justify-end gap-2">
      <button class="btn btn-xs btn-ghost" type="button" disabled={busy} onclick={() => { open = false; error = ''; }}>Cancel</button>
      <button class="btn btn-xs btn-primary" type="button" disabled={busy} onclick={() => void submit()}>File</button>
    </div>
  {/if}
</div>
