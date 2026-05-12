<!--
  SessionsView renders a table of active agent sessions with state badges.
  Row actions: Prompt (opens prompt modal), Close (confirm + DELETE /sessions/{id}).
-->
<script lang="ts">
  import { orchestratorStore, refresh, type SessionInfo } from '../../stores/orchestrator.svelte.ts';
  import { routeSessionDetail } from '../../stores/router.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import { statusToVariant } from '../../components/core/statusVariant.ts';
  import SpawnDialog from '../../components/orchestrator/SpawnDialog.svelte';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { MessageSquare, Send, X } from 'lucide-svelte';
  import type { PromptSessionRequest } from '@quicbaas/sdk';
  import { formatDateTime } from '../../lib/time.ts';

  let spawnOpen = $state(false);

  // ---------------------------------------------------------------------------
  // Prompt modal state
  // ---------------------------------------------------------------------------

  let promptTarget = $state<SessionInfo | null>(null);
  let promptText = $state('');
  let prompting = $state(false);

  function openPrompt(session: SessionInfo): void {
    promptTarget = session;
    promptText = '';
  }

  function dismissPrompt(): void {
    promptTarget = null;
    promptText = '';
  }

  async function submitPrompt(): Promise<void> {
    if (!promptTarget || !promptText.trim()) return;
    prompting = true;
    const sessionID = promptTarget.session_id;
    const req: PromptSessionRequest = {
      session_id: sessionID,
      prompt: promptText.trim(),
    };
    try {
      const client = await getSDKClient();
      await client.orchestrator.promptSession(req);
      showToast('success', `Prompt sent to session ${sessionID.slice(0, 8)}`);
      dismissPrompt();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Prompt failed: ${msg}`);
    } finally {
      prompting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Close session confirm modal state
  // ---------------------------------------------------------------------------

  let closeTarget = $state<SessionInfo | null>(null);
  let closing = $state(false);

  function requestClose(session: SessionInfo): void {
    closeTarget = session;
  }

  function dismissClose(): void {
    closeTarget = null;
  }

  async function confirmClose(): Promise<void> {
    if (!closeTarget) return;
    closing = true;
    const sessionID = closeTarget.session_id;
    try {
      const client = await getSDKClient();
      await client.orchestrator.closeSession({ session_id: sessionID });
      showToast('success', `Session ${sessionID.slice(0, 8)} closed`);
      closeTarget = null;
      void refresh('sessions');
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Close failed: ${msg}`);
    } finally {
      closing = false;
    }
  }

  function stateVariant(state: string): 'success' | 'error' | 'warning' | 'info' | 'ghost' | 'neutral' | 'primary' {
    return statusToVariant(state);
  }

  function isClosable(state: string): boolean {
    return state !== 'closed' && state !== 'terminated';
  }

  const formatTime = (ts: number): string => formatDateTime(ts);
</script>

<!-- Prompt modal -->
{#if promptTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Prompt session">
    <div class="modal-box max-w-lg">
      <h3 class="font-bold text-base mb-1">Prompt Session</h3>
      <p class="text-xs text-base-content/50 mb-3 font-mono">{promptTarget.session_id}</p>
      <textarea
        class="textarea textarea-bordered w-full h-32 text-sm"
        placeholder="Enter your prompt…"
        bind:value={promptText}
        onkeydown={(e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { void submitPrompt(); } }}
      ></textarea>
      <div class="modal-action">
        <button class="btn btn-sm btn-ghost" onclick={dismissPrompt} disabled={prompting}>Cancel</button>
        <button
          class="btn btn-sm btn-primary gap-1"
          onclick={() => { void submitPrompt(); }}
          disabled={prompting || !promptText.trim()}
        >
          {#if prompting}
            <span class="loading loading-spinner loading-xs"></span>
          {:else}
            <Send class="h-3 w-3" />
          {/if}
          Send
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={dismissPrompt} aria-label="Close"></button>
  </div>
{/if}

<!-- Close session confirm modal -->
{#if closeTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Confirm close session">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Close session?</h3>
      <p class="text-sm text-base-content/70 mb-4">
        Close session <span class="font-mono font-semibold">{closeTarget.session_id.slice(0, 16)}…</span>?
        This terminates the agent process.
      </p>
      <div class="modal-action">
        <button class="btn btn-sm btn-ghost" onclick={dismissClose} disabled={closing}>Cancel</button>
        <button
          class="btn btn-sm btn-error"
          onclick={() => { void confirmClose(); }}
          disabled={closing}
        >
          {#if closing}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Close Session
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={dismissClose} aria-label="Close"></button>
  </div>
{/if}

<SpawnDialog open={spawnOpen} onClose={() => { spawnOpen = false; }} />

<div class="p-6 space-y-6">
  <PageHeader
    title="Sessions"
    subtitle="Conversational sessions across persistent backends"
    icon={MessageSquare}
  >
    {#snippet children()}
      <button class="btn btn-primary btn-sm" onclick={() => { spawnOpen = true; }}>
        + Spawn
      </button>
    {/snippet}
  </PageHeader>

  <div class="overflow-x-auto rounded-box border border-base-300/50">
    <table class="table table-sm table-zebra w-full">
      <thead>
        <tr class="bg-base-200/80 text-xs uppercase tracking-wider text-base-content/50">
          <th>Session ID</th>
          <th>Project</th>
          <th>Backend</th>
          <th>Model</th>
          <th>State</th>
          <th>Created</th>
          <th>Last Activity</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        {#if orchestratorStore.sessionsLoading}
          <tr>
            <td colspan="8" class="text-center py-8">
              <span class="loading loading-spinner loading-sm text-primary"></span>
            </td>
          </tr>
        {:else if orchestratorStore.sessions.length === 0}
          <tr>
            <td colspan="8">
              <EmptyState
                icon={MessageSquare}
                title="No sessions yet — sessions are how you converse with persistent backends."
                description="Spawn a session to start a conversation with Claude, Codex, or any registered backend."
                actionLabel="+ Spawn"
                onaction={() => { spawnOpen = true; }}
              />
            </td>
          </tr>
        {:else}
          {#each orchestratorStore.sessions as session (session.session_id)}
            <tr
              class="hover:bg-base-300/40 cursor-pointer"
              onclick={() => routeSessionDetail(session.session_id)}
              onkeydown={(e) => { if (e.key === 'Enter') routeSessionDetail(session.session_id); }}
              tabindex="0"
              role="button"
              aria-label="Open session {session.session_id.slice(0, 8)}"
            >
              <td class="font-mono text-xs">{session.session_id.slice(0, 12)}...</td>
              <td class="font-mono text-xs text-base-content/60">{session.project_id || '—'}</td>
              <td><span class="badge badge-sm badge-primary">{session.backend}</span></td>
              <td class="text-xs">{session.model || '—'}</td>
              <td>
                <StatusBadge
                  text={session.state}
                  variant={stateVariant(session.state)}
                  pulse={session.state === 'active'}
                />
              </td>
              <td class="text-xs text-base-content/50">{formatTime(session.created_at)}</td>
              <td class="text-xs text-base-content/50">{formatTime(session.last_activity)}</td>
              <td>
                <div class="flex items-center gap-1">
                  <button
                    class="btn btn-xs btn-ghost gap-1"
                    onclick={() => openPrompt(session)}
                    aria-label="Prompt session {session.session_id}"
                    title="Prompt"
                  >
                    <Send class="h-3 w-3" />
                  </button>
                  {#if isClosable(session.state)}
                    <button
                      class="btn btn-xs btn-ghost text-error gap-1"
                      onclick={() => requestClose(session)}
                      aria-label="Close session {session.session_id}"
                      title="Close"
                    >
                      <X class="h-3 w-3" />
                    </button>
                  {/if}
                </div>
              </td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
</div>
