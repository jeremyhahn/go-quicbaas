<!--
  PromptsView — the prompts jobs render from, one set at one scope.

  The list shows every prompt of a set as the chosen scope sees it: whether
  the scope holds its own edited record and which version a job would run.
  Opening a prompt shows its effective body, the resolution chain that chose
  it and the scope's revision history. From there an operator can diff the
  body against the parent scope's, write a new body, roll back to an earlier
  revision or reset the scope to follow its parent again.

  Every write carries expected_version, the scope record's version when the
  page read it, so two operators editing the same prompt cannot overwrite each
  other: the server answers 409 and the page keeps the edit in front of the
  operator with the server's message.

  A link (`#/prompts?set=…&scope=…&project=…&name=…&version=…`, built by
  src/lib/prompts.ts; the job page links each prompt version a job ran with)
  opens the page on that set and scope, opens the prompt, marks the linked
  revision in the history and shows its body.

  All calls go through the SDK's PromptsClient. A transport that does not
  carry prompts refuses with TransportNotSupportedError; the page says so
  instead of rendering an empty list.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { formatRelative } from '../../lib/time.ts';
  import { routerStore } from '../../stores/router.svelte.ts';
  import {
    PROMPT_SCOPES,
    PROMPT_SETS,
    missingPlaceholders,
    parsePromptLink,
    promptAvailability,
    promptOrigin,
    promptPlaceholders,
    type PromptLink,
    type PromptsUnavailable,
  } from '../../lib/prompts.ts';
  import type {
    EffectivePrompt,
    PromptAddress,
    PromptDiff,
    PromptDiffLine,
    PromptHistory,
    PromptListEntry,
    PromptRevisionSummary,
    PromptScope,
    PromptView,
  } from '@quicbaas/sdk';
  import { APIError, TransportNotSupportedError } from '@quicbaas/sdk';
  import { FileText, RefreshCw, Pencil, GitCompare, RotateCcw, Undo2 } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import PageHeader from '../../components/core/PageHeader.svelte';

  const HTTP_CONFLICT = 409;
  const HTTP_BAD_REQUEST = 400;

  /** DIFF_LINE_CLASS styles a diff line by what it does to the parent's body. */
  const DIFF_LINE_CLASS: Record<PromptDiffLine['op'], string> = {
    equal: 'diff-equal',
    delete: 'diff-delete text-error line-through',
    insert: 'diff-insert text-success',
  };

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  // Another page — the job page, for a prompt version a job rendered — may
  // link here. The link is read once: it picks the set, scope and project the
  // page opens on, and the prompt the first list read opens.
  const linked: PromptLink | null = parsePromptLink(untrack(() => routerStore.params));
  let pendingLink: PromptLink | null = linked;

  let promptSet = $state(linked?.set ?? 'workflow');
  let scope = $state(linked?.scope ?? 'tenant');
  let projectID = $state(linked?.project ?? '');

  /** linkedVersion is the revision the link named, while its prompt is open. */
  let linkedVersion = $state(0);
  /** linkedRecord is that revision's body, read from the scope's history. */
  let linkedRecord: PromptView | null = $state(null);

  let entries: PromptListEntry[] = $state([]);
  let loading = $state(false);
  let unavailable: TransportNotSupportedError | null = $state(null);
  /** blocked is a 403 or 503 on the list read: the page shows why instead of the set. */
  let blocked: PromptsUnavailable | null = $state(null);
  /**
   * canWrite is false once a write was refused 403: the caller can read
   * prompts here but not change them, so the write verbs are hidden.
   */
  let canWrite = $state(true);

  let selected: PromptListEntry | null = $state(null);
  let effective: EffectivePrompt | null = $state(null);
  let history: PromptHistory | null = $state(null);
  let diff: PromptDiff | null = $state(null);

  let editing = $state(false);
  let draft = $state('');
  /**
   * draftVersion is the scope record's version the draft was started from.
   * A refresh that reads a newer version leaves it alone, so Save is
   * answered 409 instead of overwriting the other edit.
   */
  let draftVersion = $state(0);
  let draftNote = $state('');
  let saving = $state(false);
  let conflict = $state('');

  let rollbackTarget: PromptRevisionSummary | null = $state(null);
  let rollbackNote = $state('');
  let resetOpen = $state(false);
  let writing = $state(false);

  /** scopeVersion is the scope record's version every write expects. */
  let scopeVersion = $derived(selected?.version ?? 0);

  /** legend is the placeholders the body the edit starts from carries. */
  let legend = $derived(promptPlaceholders(effective?.content ?? ''));
  /** dropped is every placeholder of that body the draft no longer carries. */
  let dropped = $derived(editing ? missingPlaceholders(effective?.content ?? '', draft) : []);

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  /** PromptKey names one prompt of the set at the chosen scope. */
  type PromptKey = { addr: PromptAddress; set: string; name: string };

  /** scopeAddress is the chosen scope, as the client's query names it. */
  function scopeAddress(): PromptAddress {
    return {
      tenant_id: orchestratorStore.activeTenantID,
      scope: scope as PromptScope,
      project_id: scope === 'project' ? projectID.trim() : undefined,
    };
  }

  /** address names the selected prompt at the chosen scope. */
  function address(name: string): PromptKey {
    return { addr: scopeAddress(), set: promptSet, name };
  }

  /**
   * failed reports a refused call. A transport that does not carry prompts
   * switches the page to its unavailable state; any other error is a toast
   * naming the HTTP status when the server sent one.
   */
  function failed(verb: string, err: unknown): void {
    if (err instanceof TransportNotSupportedError) {
      unavailable = err;
      return;
    }
    if (err instanceof APIError) {
      showToast('error', `${verb} failed (${err.status}): ${err.message}`);
      return;
    }
    const msg = err instanceof Error ? err.message : String(err);
    showToast('error', `${verb} failed: ${msg}`);
  }

  /**
   * writeFailed reports a refused write. A 403 means the caller may read
   * prompts here but not change them: the write verbs are hidden from then
   * on and the toast names the permission.
   */
  function writeFailed(verb: string, err: unknown): void {
    if (promptAvailability(err)?.kind === 'forbidden') {
      canWrite = false;
      editing = false;
      rollbackTarget = null;
      resetOpen = false;
      showToast('error', `${verb} failed: you need the prompts:write permission`);
      return;
    }
    failed(verb, err);
  }

  // ---------------------------------------------------------------------------
  // Loading
  // ---------------------------------------------------------------------------

  /**
   * loadList reads the set at the scope. A failure leaves the list the page
   * already shows in place.
   */
  async function loadList(): Promise<void> {
    if (scope === 'project' && projectID.trim() === '') {
      entries = [];
      return;
    }
    loading = true;
    try {
      const client = await getSDKClient();
      const resp = await client.prompts.listPrompts(promptSet, scopeAddress());
      unavailable = null;
      blocked = null;
      entries = resp.prompts ?? [];
      if (selected !== null) {
        const name = selected.name;
        selected = entries.find((e) => e.name === name) ?? null;
      }
    } catch (err: unknown) {
      const why = promptAvailability(err);
      if (why !== null) {
        blocked = why;
        entries = [];
      } else {
        failed('Loading prompts', err);
      }
    } finally {
      loading = false;
    }
  }

  /**
   * selection counts the changes of the open prompt and of its address. A
   * read started under one count lands only while the count is unchanged.
   */
  let selection = 0;

  /** sameAddress reports whether two addresses name the same scope record. */
  function sameAddress(a: PromptKey, b: PromptKey): boolean {
    return (
      a.addr.tenant_id === b.addr.tenant_id &&
      a.addr.scope === b.addr.scope &&
      a.addr.project_id === b.addr.project_id &&
      a.set === b.set &&
      a.name === b.name
    );
  }

  /**
   * stillSelected reports whether a read started under the selection count
   * at the address still answers what the page shows. A slow answer for a
   * prompt the operator has since left must not fill the editor.
   */
  function stillSelected(started: number, key: PromptKey): boolean {
    return started === selection && selected?.name === key.name && sameAddress(key, address(key.name));
  }

  /** loadDetail reads the selected prompt's effective body and history. */
  async function loadDetail(name: string): Promise<void> {
    const started = selection;
    const addr = address(name);
    try {
      const client = await getSDKClient();
      const [eff, hist] = await Promise.all([
        client.prompts.getEffectivePrompt(addr.set, addr.name, addr.addr),
        client.prompts.getPromptHistory(addr.set, addr.name, addr.addr).catch((err: unknown) => {
          // Reading history is its own permission (prompts:history): without
          // it the page still shows the prompt, just not its revisions.
          if (promptAvailability(err)?.kind === 'forbidden') return null;
          throw err;
        }),
      ]);
      if (!stillSelected(started, addr)) return;
      effective = eff;
      history = hist;
    } catch (err: unknown) {
      if (!stillSelected(started, addr)) return;
      failed(`Loading ${name}`, err);
    }
  }

  $effect(() => {
    // Re-read whenever the set, scope, project or tenant changes.
    void [promptSet, scope, projectID, orchestratorStore.activeTenantID];
    untrack(() => {
      closeDetail();
      void loadList().then(openLinked);
    });
  });

  /**
   * openLinked opens the prompt a link named, once, after the first list
   * read, and reads the revision it named. A link to a prompt the set does
   * not list opens nothing; version 0 names no stored revision.
   */
  async function openLinked(): Promise<void> {
    const link = pendingLink;
    pendingLink = null;
    if (link === null) return;
    const entry = entries.find((e) => e.name === link.name);
    if (entry === undefined) return;
    await openPrompt(entry);
    if (link.version === 0) return;
    linkedVersion = link.version;
    const started = selection;
    const addr = address(link.name);
    try {
      const client = await getSDKClient();
      const rec = await client.prompts.getPrompt(addr.set, addr.name, { ...addr.addr, version: link.version });
      if (!stillSelected(started, addr)) return;
      linkedRecord = rec;
    } catch (err: unknown) {
      if (!stillSelected(started, addr)) return;
      failed(`Loading ${link.name} v${link.version}`, err);
    }
  }

  function closeDetail(): void {
    selection++;
    selected = null;
    effective = null;
    history = null;
    diff = null;
    linkedVersion = 0;
    linkedRecord = null;
    editing = false;
    conflict = '';
  }

  async function openPrompt(entry: PromptListEntry): Promise<void> {
    closeDetail();
    selected = entry;
    await loadDetail(entry.name);
  }

  /** reloadAfterWrite re-reads the list and the open prompt after a write. */
  async function reloadAfterWrite(name: string): Promise<void> {
    diff = null;
    await loadList();
    await loadDetail(name);
  }

  // ---------------------------------------------------------------------------
  // Diff
  // ---------------------------------------------------------------------------

  async function showDiff(): Promise<void> {
    if (selected === null) return;
    try {
      const client = await getSDKClient();
      const key = address(selected.name);
      diff = await client.prompts.diffPrompt(key.set, key.name, key.addr);
    } catch (err: unknown) {
      failed('Diff', err);
    }
  }

  // ---------------------------------------------------------------------------
  // Edit
  // ---------------------------------------------------------------------------

  function startEdit(): void {
    draft = effective?.content ?? '';
    draftVersion = scopeVersion;
    draftNote = '';
    conflict = '';
    editing = true;
  }

  /**
   * save writes the draft. A 409 means the scope moved on since the page
   * read it: the draft stays and the server's message says what changed.
   */
  async function save(): Promise<void> {
    if (selected === null) return;
    const name = selected.name;
    saving = true;
    conflict = '';
    try {
      const client = await getSDKClient();
      const key = address(name);
      const body = { content: draft, note: draftNote.trim(), expected_version: draftVersion };
      const rec = await client.prompts.putPrompt(key.set, key.name, body, key.addr);
      showToast('success', `Saved ${name} v${rec.version}`);
      editing = false;
      await reloadAfterWrite(name);
    } catch (err: unknown) {
      // 409: the scope moved on (the server names who changed it); 400: the
      // body fails validation (the server names the marker it misses). Both
      // keep the draft open under a banner.
      if (err instanceof APIError && (err.status === HTTP_CONFLICT || err.status === HTTP_BAD_REQUEST)) {
        conflict = err.message;
      } else {
        writeFailed(`Saving ${name}`, err);
      }
    } finally {
      saving = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Rollback and reset
  // ---------------------------------------------------------------------------

  function openRollback(rev: PromptRevisionSummary): void {
    rollbackTarget = rev;
    rollbackNote = '';
  }

  async function confirmRollback(): Promise<void> {
    if (selected === null || rollbackTarget === null) return;
    const name = selected.name;
    const toVersion = rollbackTarget.version;
    writing = true;
    try {
      const client = await getSDKClient();
      const key = address(name);
      const body = { to_version: toVersion, note: rollbackNote.trim() || undefined, expected_version: scopeVersion };
      const rec = await client.prompts.rollbackPrompt(key.set, key.name, body, key.addr);
      showToast('success', `Rolled ${name} back to v${toVersion} as v${rec.version}`);
      rollbackTarget = null;
      await reloadAfterWrite(name);
    } catch (err: unknown) {
      writeFailed(`Rolling back ${name}`, err);
    } finally {
      writing = false;
    }
  }

  async function confirmReset(): Promise<void> {
    if (selected === null) return;
    const name = selected.name;
    writing = true;
    try {
      const client = await getSDKClient();
      const key = address(name);
      await client.prompts.resetPrompt(key.set, key.name, { ...key.addr, expected_version: scopeVersion });
      showToast('success', `${name} follows its parent scope again`);
      resetOpen = false;
      await reloadAfterWrite(name);
    } catch (err: unknown) {
      writeFailed(`Resetting ${name}`, err);
    } finally {
      writing = false;
    }
  }
</script>

<!-- Rollback confirmation -->
{#if rollbackTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Confirm rollback">
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">Roll back to v{rollbackTarget.version}?</h3>
      <p class="text-sm text-base-content/60 mt-2">
        A new revision is written carrying the body of v{rollbackTarget.version}. The history is kept.
      </p>
      <label class="form-control mt-3">
        <span class="label-text text-xs">Note</span>
        <input
          class="input input-sm input-bordered"
          aria-label="Rollback note"
          bind:value={rollbackNote}
        />
      </label>
      <div class="modal-action">
        <button class="btn btn-sm btn-ghost" onclick={() => { rollbackTarget = null; }} disabled={writing}>
          Cancel
        </button>
        <button class="btn btn-sm btn-warning" onclick={() => { void confirmRollback(); }} disabled={writing}>
          Roll back
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- Reset confirmation -->
{#if resetOpen && selected}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Confirm reset">
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">Reset {selected.name}?</h3>
      <p class="text-sm text-base-content/60 mt-2">
        This scope's override is removed and jobs run the parent scope's body again. The history is kept.
      </p>
      <div class="modal-action">
        <button class="btn btn-sm btn-ghost" onclick={() => { resetOpen = false; }} disabled={writing}>
          Cancel
        </button>
        <button class="btn btn-sm btn-error" onclick={() => { void confirmReset(); }} disabled={writing}>
          Reset
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- Main content -->
<div class="page">
  <PageHeader
    title="Prompts"
    subtitle="The prompts jobs render from, per set and scope, with their revisions"
    icon={FileText}
  >
    {#snippet children()}
      <button
        class="btn btn-sm btn-ghost gap-1"
        onclick={() => { void loadList(); }}
        disabled={loading}
        aria-label="Refresh prompts"
      >
        <RefreshCw class="h-4 w-4 {loading ? 'animate-spin' : ''}" />
        Refresh
      </button>
    {/snippet}
  </PageHeader>

  <div class="flex flex-wrap gap-2 items-end">
    <label class="form-control">
      <span class="label-text text-xs">Set</span>
      <select class="select select-sm select-bordered" aria-label="Prompt set" bind:value={promptSet}>
        {#each PROMPT_SETS as s (s)}
          <option value={s}>{s}</option>
        {/each}
      </select>
    </label>
    <label class="form-control">
      <span class="label-text text-xs">Scope</span>
      <select class="select select-sm select-bordered" aria-label="Prompt scope" bind:value={scope}>
        {#each PROMPT_SCOPES as s (s)}
          <option value={s}>{s}</option>
        {/each}
      </select>
    </label>
    {#if scope === 'project'}
      <label class="form-control">
        <span class="label-text text-xs">Project ID</span>
        <input class="input input-sm input-bordered" aria-label="Project ID" bind:value={projectID} />
      </label>
    {/if}
  </div>

  {#if unavailable}
    <div class="alert alert-info text-sm" role="status" aria-label="Prompts unavailable">
      <span>
        Prompts are not available on the {unavailable.transport} transport: {unavailable.reason}.
        Connect over REST to browse and edit them.
      </span>
    </div>

  {:else if blocked}
    <EmptyState
      icon={FileText}
      title={blocked.kind === 'forbidden' ? 'No access to prompts' : 'Prompts unavailable'}
      description={blocked.message}
    />

  {:else if loading && entries.length === 0}
    <div class="space-y-3">
      {#each Array(3) as _}
        <div class="skeleton h-10 w-full rounded-box"></div>
      {/each}
    </div>

  {:else if entries.length === 0}
    <EmptyState
      icon={FileText}
      title="No prompts in this set"
      description={scope === 'project' && projectID.trim() === ''
        ? 'Enter a project ID to see the prompts at project scope.'
        : 'This set declares no prompts at this scope.'}
    />

  {:else}
    <div class="grid gap-4 lg:grid-cols-[18rem_1fr]">
      <ul class="menu bg-base-200 rounded-box p-2" aria-label="Prompts">
        {#each entries as entry (entry.name)}
          <li>
            <button
              class:active={selected?.name === entry.name}
              aria-label="Open prompt {entry.name}"
              onclick={() => { void openPrompt(entry); }}
            >
              <span class="font-mono text-sm">{entry.name}</span>
              <span class="badge badge-sm {promptOrigin(entry).badge}" title={promptOrigin(entry).label}>
                {entry.overridden ? `v${entry.version ?? 0}` : promptOrigin(entry).label}
              </span>
            </button>
          </li>
        {/each}
      </ul>

      {#if selected && effective}
        <div class="space-y-4">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="font-semibold font-mono">{selected.name}</h2>
            <span class="text-xs text-base-content/60">
              runs v{effective.origin.version} from {effective.origin.scope}
            </span>
            <div class="flex-1"></div>
            <button class="btn btn-xs btn-ghost gap-1" onclick={() => { void showDiff(); }} aria-label="Diff against parent">
              <GitCompare class="h-3 w-3" /> Diff
            </button>
            {#if canWrite && scope !== 'system'}
              <button class="btn btn-xs btn-ghost gap-1" onclick={startEdit} aria-label="Edit prompt">
                <Pencil class="h-3 w-3" /> Edit
              </button>
            {/if}
            {#if canWrite && selected.overridden}
              <button class="btn btn-xs btn-ghost gap-1" onclick={() => { resetOpen = true; }} aria-label="Reset to parent">
                <Undo2 class="h-3 w-3" /> Reset
              </button>
            {/if}
          </div>
          {#if selected.description}
            <p class="text-sm text-base-content/60">{selected.description}</p>
          {/if}

          {#if editing}
            <div class="space-y-2">
              {#if conflict}
                <div class="alert alert-warning text-sm py-2" role="alert">
                  <span>{conflict}</span>
                </div>
              {/if}
              {#if legend.length > 0}
                <div class="text-xs text-base-content/60" aria-label="Placeholder legend">
                  Placeholders:
                  {#each legend as p (p)}
                    <code class="badge badge-sm badge-ghost font-mono ml-1">{'${' + p + '}'}</code>
                  {/each}
                  <span class="ml-1">and the markers the set declares are checked on save.</span>
                </div>
              {/if}
              <textarea
                class="textarea textarea-bordered w-full font-mono text-xs h-64"
                aria-label="Prompt body"
                bind:value={draft}
              ></textarea>
              {#if dropped.length > 0}
                <div class="alert alert-warning text-sm py-2" role="status" aria-label="Missing placeholders">
                  <span>The draft no longer carries {dropped.map((p) => '${' + p + '}').join(', ')}.</span>
                </div>
              {/if}
              <input
                class="input input-sm input-bordered w-full"
                placeholder="What changed and why"
                aria-label="Edit note"
                bind:value={draftNote}
              />
              <div class="flex gap-2 justify-end">
                <button class="btn btn-sm btn-ghost" onclick={() => { editing = false; conflict = ''; }} disabled={saving}>
                  Cancel
                </button>
                <button class="btn btn-sm btn-primary" onclick={() => { void save(); }} disabled={saving}>
                  Save
                </button>
              </div>
            </div>
          {:else}
            <pre class="bg-base-200 rounded-box p-3 text-xs whitespace-pre-wrap">{effective.content}</pre>
          {/if}

          {#if diff}
            <section class="space-y-1" aria-label="Prompt diff">
              <h3 class="text-sm font-semibold">
                <span>v{diff.origin.version} ({diff.origin.scope}) against v{diff.parent_origin.version} ({diff.parent_origin.scope})</span>
              </h3>
              {#if diff.same}
                <p class="text-sm text-base-content/60">This scope runs the same body as its parent.</p>
              {:else}
                <div class="bg-base-200 rounded-box p-3 font-mono text-xs">
                  {#each diff.lines as line, i (i)}
                    <div class="whitespace-pre-wrap">
                      {line.op === 'insert' ? '+' : line.op === 'delete' ? '-' : ' '}
                      <span class={DIFF_LINE_CLASS[line.op]}>{line.text}</span>
                    </div>
                  {/each}
                </div>
              {/if}
            </section>
          {/if}

          {#if linkedRecord}
            <section class="space-y-1" aria-label="Linked version">
              <h3 class="text-sm font-semibold">
                Linked version v{linkedRecord.version} ({linkedRecord.scope})
              </h3>
              <pre class="bg-base-200 rounded-box p-3 text-xs whitespace-pre-wrap">{linkedRecord.content}</pre>
            </section>
          {/if}

          <div class="text-xs text-base-content/60">
            Resolution:
            {#each effective.chain as layer, i (i)}
              <span class="badge badge-sm {layer.answered ? 'badge-primary' : 'badge-ghost'} ml-1">
                {layer.scope}{layer.present && layer.version ? ` v${layer.version}` : ''}
              </span>
            {/each}
          </div>

          {#if history}
            <table class="table table-sm" aria-label="Prompt history">
              <thead>
                <tr><th>Version</th><th>Author</th><th>Note</th><th>When</th><th></th></tr>
              </thead>
              <tbody>
                {#each history.revisions as rev (rev.version)}
                  <tr
                    class:bg-base-300={rev.version === linkedVersion}
                    aria-current={rev.version === linkedVersion ? 'true' : undefined}
                  >
                    <td class="font-mono">v{rev.version}{rev.follow_parent ? ' (reset)' : ''}</td>
                    <td>{rev.author}</td>
                    <td>{rev.note ?? ''}</td>
                    <td>{formatRelative(rev.created_at)}</td>
                    <td class="text-right">
                      {#if canWrite && rev.version !== scopeVersion && !rev.follow_parent}
                        <button
                          class="btn btn-xs btn-ghost gap-1"
                          onclick={() => { openRollback(rev); }}
                          aria-label="Roll back to v{rev.version}"
                        >
                          <RotateCcw class="h-3 w-3" /> Roll back
                        </button>
                      {/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
</div>

