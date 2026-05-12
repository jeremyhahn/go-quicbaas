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
  InferenceView — stateless fleet-routed inference playground.

  Allows operators to send a single prompt to an agent-resident backend
  (e.g. Ollama) without spawning a long-lived session. Supports streaming
  via chunked NDJSON and maintains an in-session call history.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { Sparkles, Copy, Trash2, Send, X, ChevronDown, ChevronRight } from 'lucide-svelte';
  import { formatTimeOnly } from '../../lib/time.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { navigate } from '../../stores/navigation.svelte.ts';
  import {
    inferenceHistoryStore,
    addInferenceHistory,
    clearInferenceHistory,
    type InferenceHistoryEntry,
  } from '../../stores/inferenceHistoryStore.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import type { AgentBackendInfo, AgentRegistration, InferenceChunk } from '@quicbaas/sdk';

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  // Backend selection
  let backends: AgentBackendInfo[] = $state([]);
  let backendsLoading = $state(true);
  let backendsError = $state('');

  // Agent fleet for host affinity
  let agents: AgentRegistration[] = $state([]);
  let agentsLoading = $state(true);

  // Form fields
  let selectedBackend = $state('');
  let selectedHostAffinity = $state('');
  let hostAffinityStrict = $state(false);
  let modelInput = $state('');
  let systemPromptInput = $state('');
  let promptInput = $state('');
  let streamEnabled = $state(true);
  let systemPromptOpen = $state(false);

  // Model dropdown
  let availableModels: string[] = $state([]);
  let modelsLoading = $state(false);
  let modelsError = $state('');
  let selectedModel = $state('');
  let modelOverride = $state('');

  // In-flight state
  let inFlight = $state(false);
  let abortController: AbortController | null = null;

  // Response state
  let responseText = $state('');
  let responseModel = $state('');
  let responseStopReason = $state('');
  let responseElapsedMs = $state(0);
  let responseStartTime = 0;

  // Field-level error
  let promptError = $state('');

  // responseError surfaces server-side failures (4xx/5xx typed envelopes
  // and unexpected exceptions) inside the Response panel as a destructive
  // alert so the operator can see what went wrong without having to chase
  // the toast. Cleared on a fresh submit.
  let responseError = $state('');

  // History: expanded entry IDs
  let expandedHistoryIds = $state(new Set<number>());

  // ---------------------------------------------------------------------------
  // Derived
  // ---------------------------------------------------------------------------

  // Session-based backends that require a long-lived session (not stateless).
  // ACP / agentic backends are session-only — they belong in the Sessions
  // and Workflows views, not the one-shot Inference playground.
  const SESSION_ONLY_BACKENDS = new Set(['claude', 'codex', 'opencode']);

  // isSessionOnlyBackend reports whether a backend should be greyed out in
  // the Inference view. Detection is a union of:
  //   1. Capabilities.kind === 'agent' (the canonical signal)
  //   2. Membership in the legacy SESSION_ONLY_BACKENDS name set (for
  //      daemons that have not yet populated capabilities.kind)
  function isSessionOnlyBackend(b: AgentBackendInfo): boolean {
    if (b.capabilities?.kind === 'agent') return true;
    return SESSION_ONLY_BACKENDS.has(b.name);
  }

  // Agents filtered to those compatible with the currently selected backend.
  const compatibleAgents = $derived.by((): AgentRegistration[] => {
    if (!selectedBackend) return agents;
    return agents.filter((a) => {
      // Match by backend field or capabilities list.
      if (a.backend === selectedBackend) return true;
      if (Array.isArray(a.capabilities) && a.capabilities.includes(selectedBackend)) return true;
      return false;
    });
  });

  // selectedBackendInfo is the AgentBackendInfo currently chosen, or undefined.
  const selectedBackendInfo = $derived(backends.find((b) => b.name === selectedBackend));

  // isAPIInferenceBackend reports whether the selected backend is an HTTP-based
  // frontier-model provider (Anthropic, OpenAI, xAI, ...). Used to render the
  // "no provider configured" hint when ListModels comes back empty.
  const isAPIInferenceBackend = $derived(
    selectedBackendInfo?.capabilities?.kind === 'api_inference'
  );

  const hasResponse = $derived(responseText !== '');
  const hasResponseError = $derived(responseError !== '');

  // ---------------------------------------------------------------------------
  // Lifecycle
  // ---------------------------------------------------------------------------

  onMount(() => {
    void loadBackends();
    void loadAgents();
  });

  async function loadBackends(): Promise<void> {
    backendsLoading = true;
    backendsError = '';
    try {
      const client = await getSDKClient();
      const raw = await client.orchestrator.listBackends();
      const seenB = new Set<string>();
      const list = raw.filter((b) => {
        if (seenB.has(b.name)) return false;
        seenB.add(b.name);
        return true;
      });
      backends = list;
      // Default to ollama if present, otherwise first available.
      const ollama = list.find((b) => b.name === 'ollama' || b.type === 'ollama');
      const firstStateless = list.find((b) => !isSessionOnlyBackend(b));
      const chosen = ollama ?? firstStateless ?? list[0];
      if (chosen) {
        selectedBackend = chosen.name;
      }
    } catch (err) {
      backendsError = err instanceof Error ? err.message : String(err);
    } finally {
      backendsLoading = false;
    }
  }

  async function loadAgents(): Promise<void> {
    agentsLoading = true;
    try {
      const client = await getSDKClient();
      const resp = await client.orchestrator.listAgents({
        tenant_id: orchestratorStore.activeTenantID,
      });
      agents = resp.agents ?? [];
    } catch {
      agents = [];
    } finally {
      agentsLoading = false;
    }
  }

  // When the backend or host affinity changes, reload the model list.
  $effect(() => {
    const backend = selectedBackend;
    const host = selectedHostAffinity;
    // Reset host affinity when backend changes (host is derived from backend).
    if (backend) {
      void loadModels(backend, host);
    }
  });

  async function loadModels(backendName: string, agentName: string): Promise<void> {
    modelsLoading = true;
    modelsError = '';
    availableModels = [];
    selectedModel = '';
    try {
      const client = await getSDKClient();
      const models = await client.orchestrator.listBackendModels(agentName || 'any', backendName);
      availableModels = models ?? [];
      // Default to first model returned, or the backend's static default.
      if (availableModels.length > 0) {
        selectedModel = availableModels[0];
      } else {
        const backend = backends.find((b) => b.name === backendName);
        selectedModel = backend?.default_model ?? '';
      }
    } catch (err) {
      modelsError = err instanceof Error ? err.message : String(err);
    } finally {
      modelsLoading = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Submit handler
  // ---------------------------------------------------------------------------

  async function handleSubmit(e: Event): Promise<void> {
    e.preventDefault();

    if (promptInput.trim() === '') {
      promptError = 'Prompt is required.';
      return;
    }
    promptError = '';

    inFlight = true;
    responseText = '';
    responseError = '';
    responseModel = '';
    responseStopReason = '';
    responseElapsedMs = 0;
    responseStartTime = Date.now();

    abortController = new AbortController();
    const { signal } = abortController;

    const effectiveModel = modelOverride.trim() || selectedModel || modelInput.trim() || undefined;

    const req = {
      backend: selectedBackend,
      model: effectiveModel,
      prompt: promptInput,
      system_prompt: systemPromptInput.trim() || undefined,
      host_affinity: selectedHostAffinity || undefined,
      host_affinity_strict: hostAffinityStrict || undefined,
    };

    try {
      const client = await getSDKClient();

      if (streamEnabled) {
        // Streaming mode: append deltas as they arrive.
        let finalModel = '';
        let finalStopReason = '';

        for await (const chunk of client.orchestrator.inferenceStream(req, signal)) {
          for (const block of chunk.delta ?? []) {
            if (block.type === 'text' && block.text) {
              responseText += block.text;
            }
          }
          if (chunk.final) {
            finalModel = chunk.model ?? '';
            finalStopReason = chunk.stop_reason ?? '';
          }
        }

        responseModel = finalModel;
        responseStopReason = finalStopReason;
      } else {
        // Aggregate mode: wait for the full response.
        const resp = await client.orchestrator.inference(req);
        for (const block of resp.content ?? []) {
          if (block.type === 'text' && block.text) {
            responseText += block.text;
          }
        }
        responseModel = resp.model ?? '';
        responseStopReason = resp.stop_reason ?? '';
      }

      responseElapsedMs = Date.now() - responseStartTime;

      // Add to history.
      addInferenceHistory({
        timestamp: new Date().toISOString(),
        backend: selectedBackend,
        model: responseModel || modelOverride.trim() || selectedModel || modelInput,
        hostAffinity: selectedHostAffinity,
        prompt: promptInput,
        response: responseText,
        stopReason: responseStopReason,
        elapsedMs: responseElapsedMs,
      });

    } catch (err: unknown) {
      if (err instanceof Error && err.name === 'AbortError') {
        // Stream was cancelled by the user. Keep the partial response.
        responseElapsedMs = Date.now() - responseStartTime;
        showToast('info', 'Inference stream was cancelled.');
        return;
      }

      // Classify the error for user-facing messaging.
      const statusCode = (err as { status?: number }).status;
      const message = err instanceof Error ? err.message : String(err);

      // Surface the typed-envelope error inside the Response panel so
      // operators see what happened without losing their prompt. The
      // prompt is intentionally left populated so the request can be
      // retried by clicking Send again.
      if (statusCode !== undefined && statusCode >= 400 && statusCode < 500) {
        promptError = message;
        responseError = message;
        showToast('error', `Request error: ${message}`);
      } else {
        responseError = message;
        showToast('error', `Server error: ${message}`);
      }

      // Keep partial output when mid-stream error occurs.
      if (responseText !== '') {
        showToast('warning', 'Response may be incomplete due to stream interruption.');
      }
    } finally {
      inFlight = false;
      abortController = null;
    }
  }

  function handleCancel(): void {
    if (abortController) {
      abortController.abort();
    }
  }

  function clearResponse(): void {
    responseText = '';
    responseError = '';
    responseModel = '';
    responseStopReason = '';
    responseElapsedMs = 0;
    promptError = '';
  }

  async function copyResponse(): Promise<void> {
    if (!responseText) return;
    try {
      await navigator.clipboard.writeText(responseText);
      showToast('success', 'Response copied to clipboard.');
    } catch {
      showToast('error', 'Failed to copy to clipboard.');
    }
  }

  function toggleHistoryEntry(id: number): void {
    const next = new Set(expandedHistoryIds);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    expandedHistoryIds = next;
  }

  function formatElapsed(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  }

  function promptSummary(p: string): string {
    const trimmed = p.trim();
    return trimmed.length > 80 ? trimmed.slice(0, 77) + '...' : trimmed;
  }
</script>

<div class="p-6 space-y-6 max-w-4xl mx-auto">
  <PageHeader
    title="Inference"
    subtitle="Send a stateless prompt to a fleet-resident backend"
    icon={Sparkles}
  />

  <!-- ===========================================================================
       Card 1 — Target selection
       =========================================================================== -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-5 space-y-4">
      <h2 class="text-sm font-semibold text-base-content">Target</h2>

      {#if backendsError}
        <div role="alert" class="alert alert-error text-sm py-2">
          Failed to load backends: {backendsError}
        </div>
      {/if}

      {#if !backendsLoading && backends.length === 0 && !backendsError}
        <div class="text-center py-4 text-base-content/40 text-sm">
          No backends registered.
          <button
            type="button"
            class="link link-primary"
            onclick={() => navigate('agents')}
            aria-label="Navigate to Agent Fleet to deploy an agent"
          >
            Deploy an agent
          </button>
          first.
        </div>
      {:else}
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">

          <!-- Backend select -->
          <div class="form-control">
            <label class="label pb-1" for="inference-backend">
              <span class="label-text text-xs font-medium">Backend</span>
            </label>
            {#if backendsLoading}
              <div class="skeleton h-8 w-full rounded"></div>
            {:else}
              <select
                id="inference-backend"
                class="select select-bordered select-sm"
                bind:value={selectedBackend}
                disabled={inFlight}
                aria-label="Select inference backend"
              >
                {#each backends as backend (backend.name)}
                  {@const isSessionOnly = isSessionOnlyBackend(backend)}
                  <option
                    value={backend.name}
                    disabled={isSessionOnly}
                    title={isSessionOnly ? 'Session-based backend — use Sessions view for stateful conversation' : undefined}
                  >
                    {backend.name} ({backend.type}){isSessionOnly ? ' — session only' : ''}
                  </option>
                {/each}
              </select>
            {/if}
          </div>

          <!-- Host affinity select -->
          <div class="form-control">
            <label class="label pb-1" for="inference-host">
              <span class="label-text text-xs font-medium">Host affinity</span>
            </label>
            {#if agentsLoading}
              <div class="skeleton h-8 w-full rounded"></div>
            {:else if compatibleAgents.length === 0}
              <div class="text-xs text-base-content/40 mt-2">
                No agents found for this backend.
                <button
                  type="button"
                  class="link link-primary"
                  onclick={() => navigate('agents')}
                  aria-label="Navigate to Agent Fleet"
                >
                  Deploy an agent serving {selectedBackend} first.
                </button>
              </div>
            {:else}
              <select
                id="inference-host"
                class="select select-bordered select-sm"
                bind:value={selectedHostAffinity}
                disabled={inFlight}
                aria-label="Select host affinity (optional)"
              >
                <option value="">Any</option>
                {#each compatibleAgents as agent (agent.id ?? agent.name)}
                  <option value={agent.name}>{agent.name} @ {agent.host}</option>
                {/each}
              </select>
            {/if}
            {#if selectedHostAffinity}
              <label class="label cursor-pointer mt-1 py-0" for="inference-host-strict">
                <span class="label-text-alt text-xs">Strict — fail if host offline</span>
                <input
                  id="inference-host-strict"
                  type="checkbox"
                  class="checkbox checkbox-xs"
                  bind:checked={hostAffinityStrict}
                  disabled={inFlight}
                  aria-label="Fail the request if the selected host is offline instead of falling back to any agent"
                />
              </label>
            {/if}
          </div>

          <!-- Model select + override -->
          <div class="form-control">
            <label class="label pb-1" for="inference-model">
              <span class="label-text text-xs font-medium">Model</span>
            </label>

            {#if modelsLoading}
              <div class="skeleton h-8 w-full rounded" aria-label="Loading models"></div>
            {:else if modelsError}
              <div class="text-xs text-error mb-1" role="alert">
                Failed to load models: {modelsError}
                <button
                  type="button"
                  class="link link-primary ml-1"
                  onclick={() => { void loadModels(selectedBackend, selectedHostAffinity); }}
                  aria-label="Retry loading models"
                >Retry</button>
              </div>
            {:else if availableModels.length === 0}
              {#if isAPIInferenceBackend}
                <div class="text-xs text-warning mb-1" role="alert">
                  No provider configured for this backend — see
                  <button
                    type="button"
                    class="link link-primary"
                    onclick={() => navigate('settings')}
                    aria-label="Open Settings to configure inference providers"
                  >Settings → Inference Providers</button>.
                </div>
              {:else}
                <div class="text-xs text-base-content/40 mb-1">
                  No models reachable. The agent's backend may not be configured.
                </div>
              {/if}
            {:else}
              <select
                id="inference-model"
                class="select select-bordered select-sm"
                bind:value={selectedModel}
                disabled={inFlight}
                aria-label="Select model"
              >
                {#each availableModels as model (model)}
                  <option value={model}>{model}</option>
                {/each}
              </select>
            {/if}

            <label class="label pt-2 pb-0" for="inference-model-override">
              <span class="label-text text-xs text-base-content/50">Override (optional)</span>
            </label>
            <input
              id="inference-model-override"
              type="text"
              class="input input-bordered input-xs mt-1"
              placeholder="Type a model name to override the selection above"
              bind:value={modelOverride}
              disabled={inFlight}
              aria-label="Model name override"
            />
          </div>

        </div>
      {/if}
    </div>
  </div>

  <!-- ===========================================================================
       Card 2 — Prompt
       =========================================================================== -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-5 space-y-3">
      <h2 class="text-sm font-semibold text-base-content">Prompt</h2>

      <form onsubmit={handleSubmit} class="space-y-3">

        <!-- System prompt (collapsible) -->
        <div class="border border-base-300 rounded-lg overflow-hidden">
          <button
            type="button"
            class="flex w-full items-center gap-2 px-3 py-2 text-xs text-base-content/60 hover:bg-base-300/40 transition-colors"
            onclick={() => { systemPromptOpen = !systemPromptOpen; }}
            aria-expanded={systemPromptOpen}
            aria-controls="system-prompt-panel"
          >
            {#if systemPromptOpen}
              <ChevronDown class="h-3 w-3" />
            {:else}
              <ChevronRight class="h-3 w-3" />
            {/if}
            System prompt
            {#if systemPromptInput}
              <span class="badge badge-xs badge-primary ml-auto">set</span>
            {/if}
          </button>
          {#if systemPromptOpen}
            <div id="system-prompt-panel" class="px-3 pb-3">
              <label class="label pb-1" for="inference-system-prompt">
                <span class="label-text text-xs font-medium">System prompt</span>
              </label>
              <textarea
                id="inference-system-prompt"
                class="textarea textarea-bordered textarea-sm w-full min-h-16 text-sm font-mono"
                placeholder="Optional system message prepended to the conversation"
                bind:value={systemPromptInput}
                disabled={inFlight}
                rows={3}
                aria-label="Optional system prompt"
              ></textarea>
            </div>
          {/if}
        </div>

        <!-- User prompt -->
        <div class="form-control">
          <label class="label pb-1" for="inference-prompt">
            <span class="label-text text-xs font-medium">
              Prompt <span class="text-error">*</span>
            </span>
          </label>
          <textarea
            id="inference-prompt"
            class="textarea textarea-bordered w-full min-h-24 text-sm"
            class:textarea-error={promptError !== ''}
            placeholder="Enter your prompt..."
            bind:value={promptInput}
            disabled={inFlight}
            rows={4}
            required
            aria-label="Prompt text"
            aria-describedby={promptError ? 'prompt-error' : undefined}
          ></textarea>
          {#if promptError}
            <p id="prompt-error" class="text-error text-xs mt-1" role="alert">{promptError}</p>
          {/if}
        </div>

        <!-- Controls row -->
        <div class="flex items-center gap-3 flex-wrap">
          <!-- Stream toggle -->
          <label class="flex items-center gap-2 cursor-pointer select-none" for="inference-stream">
            <input
              id="inference-stream"
              type="checkbox"
              class="toggle toggle-sm toggle-primary"
              bind:checked={streamEnabled}
              disabled={inFlight}
              aria-label="Enable streaming mode"
            />
            <span class="text-xs text-base-content/60">Stream</span>
          </label>

          <div class="flex-1"></div>

          <!-- Cancel (visible only when in-flight) -->
          {#if inFlight}
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              onclick={handleCancel}
              aria-label="Cancel the in-flight inference request"
            >
              <X class="h-4 w-4" />
              Cancel
            </button>
          {/if}

          <!-- Send -->
          <button
            type="submit"
            class="btn btn-primary btn-sm"
            disabled={inFlight || backends.length === 0}
            aria-label="Send prompt to the selected backend"
          >
            {#if inFlight}
              <span class="loading loading-spinner loading-xs" aria-hidden="true"></span>
              Generating...
            {:else}
              <Send class="h-4 w-4" />
              Send
            {/if}
          </button>
        </div>

      </form>
    </div>
  </div>

  <!-- ===========================================================================
       Card 3 — Response
       =========================================================================== -->
  {#if hasResponse || hasResponseError || inFlight}
    <div class="card bg-base-200 rounded-box">
      <div class="card-body p-5 space-y-3">

        <div class="flex items-center justify-between">
          <h2 class="text-sm font-semibold text-base-content">Response</h2>
          <div class="flex items-center gap-2">
            {#if inFlight && !hasResponse}
              <span class="text-xs text-base-content/40 flex items-center gap-1">
                <span class="loading loading-dots loading-xs" aria-hidden="true"></span>
                Generating...
              </span>
            {/if}
            {#if hasResponse || hasResponseError}
              {#if hasResponse}
                <button
                  type="button"
                  class="btn btn-xs btn-ghost"
                  onclick={copyResponse}
                  aria-label="Copy response text to clipboard"
                >
                  <Copy class="h-3 w-3" />
                  Copy
                </button>
              {/if}
              <button
                type="button"
                class="btn btn-xs btn-ghost"
                onclick={clearResponse}
                aria-label="Clear the response area"
              >
                <Trash2 class="h-3 w-3" />
                Clear
              </button>
            {/if}
          </div>
        </div>

        <!-- Server error alert (destructive). Surfaces the typed envelope's
             `error` field so the operator can see why a 4xx/5xx response
             came back instead of just a vanishing toast. -->
        {#if hasResponseError}
          <div
            class="alert alert-error text-sm"
            role="alert"
            data-testid="inference-response-error"
          >
            <span>{responseError}</span>
          </div>
        {/if}

        <!-- Response output -->
        {#if hasResponse || inFlight}
          <pre
            class="bg-base-300 rounded-lg p-4 text-sm whitespace-pre-wrap break-words min-h-16 max-h-[32rem] overflow-y-auto font-mono leading-relaxed"
            aria-live="polite"
            aria-label="Model response"
          >{responseText}{#if inFlight && !responseText}<span class="opacity-40">Waiting for response...</span>{/if}</pre>
        {/if}

        <!-- Footer metadata -->
        {#if hasResponse && !inFlight}
          <div class="flex flex-wrap items-center gap-3 text-xs text-base-content/40 pt-1">
            {#if responseModel}
              <span>Model: <span class="font-mono text-base-content/60">{responseModel}</span></span>
            {/if}
            {#if responseStopReason}
              <span>Stop: <span class="font-mono text-base-content/60">{responseStopReason}</span></span>
            {/if}
            {#if responseElapsedMs > 0}
              <span>Elapsed: <span class="text-base-content/60">{formatElapsed(responseElapsedMs)}</span></span>
            {/if}
          </div>
        {/if}

      </div>
    </div>
  {/if}

  <!-- ===========================================================================
       Card 4 — History
       =========================================================================== -->
  {#if inferenceHistoryStore.entries.length > 0}
    <div class="card bg-base-200 rounded-box">
      <div class="card-body p-5 space-y-2">

        <div class="flex items-center justify-between">
          <h2 class="text-sm font-semibold text-base-content">
            Session History ({inferenceHistoryStore.entries.length})
          </h2>
          <button
            type="button"
            class="btn btn-xs btn-ghost"
            onclick={clearInferenceHistory}
            aria-label="Clear all inference history for this session"
          >
            <Trash2 class="h-3 w-3" />
            Clear all
          </button>
        </div>

        <div class="space-y-1">
          {#each [...inferenceHistoryStore.entries].reverse() as entry (entry.id)}
            {@const expanded = expandedHistoryIds.has(entry.id)}
            <div class="border border-base-300 rounded-lg overflow-hidden">
              <!-- Summary row -->
              <button
                type="button"
                class="flex w-full items-start gap-2 px-3 py-2 text-left hover:bg-base-300/40 transition-colors"
                onclick={() => toggleHistoryEntry(entry.id)}
                aria-expanded={expanded}
                aria-label={`Toggle history entry: ${promptSummary(entry.prompt)}`}
              >
                {#if expanded}
                  <ChevronDown class="h-3 w-3 mt-0.5 shrink-0 text-base-content/40" />
                {:else}
                  <ChevronRight class="h-3 w-3 mt-0.5 shrink-0 text-base-content/40" />
                {/if}
                <div class="flex-1 min-w-0">
                  <div class="flex flex-wrap items-center gap-2 text-xs text-base-content/40 mb-0.5">
                    <span>{formatTimeOnly(entry.timestamp)}</span>
                    <span class="badge badge-xs badge-ghost">{entry.backend}</span>
                    {#if entry.model}
                      <span class="font-mono">{entry.model}</span>
                    {/if}
                    {#if entry.hostAffinity}
                      <span>@{entry.hostAffinity}</span>
                    {/if}
                    <span class="ml-auto">{formatElapsed(entry.elapsedMs)}</span>
                  </div>
                  <p class="text-xs text-base-content/70 truncate">{promptSummary(entry.prompt)}</p>
                </div>
              </button>

              <!-- Expanded detail -->
              {#if expanded}
                <div class="px-3 pb-3 space-y-2 border-t border-base-300">
                  <p class="text-xs text-base-content/50 font-semibold mt-2">Prompt</p>
                  <pre class="bg-base-300 rounded p-2 text-xs whitespace-pre-wrap break-words max-h-32 overflow-y-auto">{entry.prompt}</pre>
                  <p class="text-xs text-base-content/50 font-semibold">Response</p>
                  <pre class="bg-base-300 rounded p-2 text-xs whitespace-pre-wrap break-words max-h-48 overflow-y-auto font-mono">{entry.response}</pre>
                  {#if entry.stopReason}
                    <p class="text-xs text-base-content/40">Stop reason: {entry.stopReason}</p>
                  {/if}
                </div>
              {/if}
            </div>
          {/each}
        </div>

      </div>
    </div>
  {/if}

</div>
