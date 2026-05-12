<!--
  SpawnDialog is a daisyUI modal for creating a new agent session.
  On submit it calls spawnSession and navigates to the new session detail.
-->
<script lang="ts">
  import { SandboxMode } from '@quicbaas/sdk';
  import type { AgentBackendInfo, SandboxModeValue } from '@quicbaas/sdk';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { agentStore } from '../../stores/agentStore.svelte.ts';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { routeSessionDetail } from '../../stores/router.svelte.ts';
  import { refreshSessions } from '../../stores/sessionStore.svelte.ts';

  // ---------------------------------------------------------------------------
  // Props
  // ---------------------------------------------------------------------------

  let {
    open = false,
    onClose,
  }: {
    open?: boolean;
    onClose?: () => void;
  } = $props();

  // ---------------------------------------------------------------------------
  // Form state
  // ---------------------------------------------------------------------------

  let backends: AgentBackendInfo[] = $state([]);
  let backendsLoaded = $state(false);
  let selectedBackend = $state('');
  let model = $state('');
  let tenant = $state('default');
  let hostAffinity = $state('');
  let sandboxMode: SandboxModeValue = $state(SandboxMode.Default);
  let initialPrompt = $state('');
  let submitting = $state(false);
  let submitError = $state('');

  // Sessions go through ACP (Agent Communication Protocol). Only ACP-capable
  // backends support the spawn operation. ollama supports inference and
  // embeddings but not spawn — selecting it would yield a 400.
  const ACP_BACKENDS = new Set(['claude', 'codex', 'opencode']);

  // Load backends when dialog opens. The dropdown is filtered to ACP-capable
  // backends; the first registered ACP backend becomes the default selection.
  // Hardcoded defaults (e.g. "claude") are never used — the UI must reflect
  // what is actually registered server-side.
  $effect(() => {
    if (!open) return;
    backendsLoaded = false;
    void (async () => {
      try {
        const client = await getSDKClient();
        const raw = await client.orchestrator.listBackends();
        const seen = new Set<string>();
        backends = raw.filter((b) => {
          if (seen.has(b.name)) return false;
          if (!ACP_BACKENDS.has(b.name)) return false;
          seen.add(b.name);
          return true;
        });
        if (backends.length > 0 && backends[0] !== undefined) {
          selectedBackend = backends[0].name;
        } else {
          selectedBackend = '';
        }
      } catch {
        backends = [];
        selectedBackend = '';
      } finally {
        backendsLoaded = true;
      }
    })();
  });

  // ---------------------------------------------------------------------------
  // Submit
  // ---------------------------------------------------------------------------

  async function handleSubmit(): Promise<void> {
    if (submitting) return;
    submitting = true;
    submitError = '';

    try {
      const client = await getSDKClient();
      const resp = await client.orchestrator.spawnSession({
        tenantID: orchestratorStore.activeTenantID,
        backend: selectedBackend,
        model: model || undefined,
        host_affinity: hostAffinity || undefined,
        sandbox_mode: sandboxMode,
      });

      // If an initial prompt was provided, send it immediately.
      if (initialPrompt.trim() !== '') {
        await client.orchestrator.promptSession({
          session_id: resp.session_id,
          prompt: initialPrompt.trim(),
        });
      }

      await refreshSessions();
      onClose?.();
      routeSessionDetail(resp.session_id);
    } catch (err: unknown) {
      submitError = parseSpawnError(err);
    } finally {
      submitting = false;
    }
  }

  // parseSpawnError extracts a human-readable message from a structured error
  // envelope ({error, hint, request_id}), a JSON-string error body, or a plain
  // Error. The server returns {"error":"validation error: ..."} for 4xx/5xx.
  function parseSpawnError(err: unknown): string {
    if (err && typeof err === 'object' && 'envelope' in err) {
      const env = (err as { envelope?: { error?: string } }).envelope;
      if (env?.error) return env.error;
    }
    const raw = err instanceof Error ? err.message : String(err);
    try {
      const parsed = JSON.parse(raw) as Record<string, unknown>;
      if (typeof parsed.error === 'string') return parsed.error;
      if (typeof parsed.message === 'string') return parsed.message;
    } catch {
      // not JSON — fall through
    }
    return raw;
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape') onClose?.();
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<dialog
  class="modal"
  class:modal-open={open}
  aria-modal="true"
  aria-label="Spawn new session"
  onkeydown={handleKeydown}
>
  <div class="modal-box w-full max-w-lg">
    <h3 class="text-base font-semibold mb-4">Spawn Session</h3>

    <form
      class="flex flex-col gap-3"
      onsubmit={(e) => { e.preventDefault(); void handleSubmit(); }}
    >
      <!-- Backend -->
      <label class="form-control">
        <div class="label"><span class="label-text">Backend</span></div>
        {#if backends.length > 0}
          <select
            class="select select-bordered select-sm"
            bind:value={selectedBackend}
            required
            aria-label="Select backend"
          >
            {#each backends as b (b.name)}
              <option value={b.name}>{b.name} ({b.type})</option>
            {/each}
          </select>
        {:else if backendsLoaded}
          <div
            class="alert alert-warning py-2 px-3 text-sm"
            role="alert"
            data-testid="no-backends-banner"
          >
            No backends registered. Register an agent or start the daemon with
            <code class="font-mono">--embedded-agent</code> to enable session spawning.
          </div>
        {:else}
          <div class="text-xs text-base-content/50">Loading backends…</div>
        {/if}
      </label>

      <!-- Model -->
      <label class="form-control">
        <div class="label"><span class="label-text">Model (optional)</span></div>
        <input
          type="text"
          class="input input-bordered input-sm"
          bind:value={model}
          placeholder="e.g. claude-opus-4-5"
          aria-label="Model name"
        />
      </label>

      <!-- Host affinity -->
      <label class="form-control">
        <div class="label"><span class="label-text">Host affinity (optional)</span></div>
        <select
          class="select select-bordered select-sm"
          bind:value={hostAffinity}
          aria-label="Host affinity"
        >
          <option value="">Any daemon</option>
          {#each agentStore.onlineAgents as agent (agent.id)}
            <option value={agent.host}>{agent.name} ({agent.host})</option>
          {/each}
        </select>
      </label>

      <!-- Sandbox policy -->
      <label class="form-control">
        <div class="label"><span class="label-text">Sandbox policy</span></div>
        <select
          class="select select-bordered select-sm"
          bind:value={sandboxMode}
          aria-label="Sandbox policy"
        >
          <option value={SandboxMode.Default}>Default (server policy)</option>
          <option value={SandboxMode.Required}>Required (container)</option>
          <option value={SandboxMode.None}>None (raw process)</option>
        </select>
      </label>

      <!-- Initial prompt -->
      <label class="form-control">
        <div class="label"><span class="label-text">Initial prompt (optional)</span></div>
        <textarea
          class="textarea textarea-bordered textarea-sm resize-none"
          bind:value={initialPrompt}
          placeholder="Send an initial prompt to the session…"
          rows="3"
          aria-label="Initial prompt"
        ></textarea>
      </label>

      {#if submitError}
        <div class="alert alert-error py-2 px-3 text-sm" role="alert">{submitError}</div>
      {/if}

      <div class="modal-action mt-2">
        <button
          type="button"
          class="btn btn-ghost btn-sm"
          onclick={() => onClose?.()}
          disabled={submitting}
        >
          Cancel
        </button>
        <button
          type="submit"
          class="btn btn-primary btn-sm"
          disabled={submitting || selectedBackend === '' || backends.length === 0}
          aria-label="Spawn session"
        >
          {#if submitting}
            <span class="loading loading-spinner loading-xs"></span>
            Spawning…
          {:else}
            Spawn
          {/if}
        </button>
      </div>
    </form>
  </div>

  <!-- Backdrop click closes dialog -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onclick={() => onClose?.()} onkeydown={() => {}}></div>
</dialog>
