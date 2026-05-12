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
  EmbeddingsView displays registered providers and lets users generate
  a vector embedding for arbitrary text, showing dimensions and preview values.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import type { EmbeddingProviderInfo, GenerateEmbeddingResponse } from '@quicbaas/sdk';
  import { Cpu, RefreshCw, Zap, Layers, Star } from 'lucide-svelte';

  // ---------------------------------------------------------------------------
  // Provider list state
  // ---------------------------------------------------------------------------

  let providers = $state<EmbeddingProviderInfo[]>([]);
  let defaultProvider = $state('');
  let loadingProviders = $state(false);
  let providerError = $state('');

  // LOAD_TIMEOUT_MS caps how long fetchProviders() can hold the loading state.
  // Prevents the Chrome DevTools Protocol Page.captureScreenshot from timing
  // out when the MCP extension opens a fresh tab to this route.
  const LOAD_TIMEOUT_MS = 10_000;

  async function fetchProviders(): Promise<void> {
    loadingProviders = true;
    providerError = '';
    const timeout = setTimeout(() => {
      if (loadingProviders) {
        loadingProviders = false;
        providerError = 'Provider list timed out. Use Refresh to reload.';
      }
    }, LOAD_TIMEOUT_MS);
    try {
      const client = await getSDKClient();
      const resp = await client.embeddings.listProviders({ include_unavailable: false });
      const raw = resp.providers ?? [];
      const seen = new Set<string>();
      providers = raw.filter((p) => {
        if (seen.has(p.name)) return false;
        seen.add(p.name);
        return true;
      });
      defaultProvider = resp.default_provider ?? '';
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      providerError = `Failed to load providers: ${msg}`;
    } finally {
      clearTimeout(timeout);
      loadingProviders = false;
    }
  }

  $effect(() => { void fetchProviders(); });

  // ---------------------------------------------------------------------------
  // Generate embedding state
  // ---------------------------------------------------------------------------

  let inputText = $state('');
  let selectedProvider = $state('');
  let generating = $state(false);
  let generateError = $state('');
  let generateErrorHint = $state('');
  let result = $state<GenerateEmbeddingResponse | null>(null);

  // Number of embedding values to display inline.
  const PREVIEW_COUNT = 8;

  // parseErrorEnvelope extracts a human-readable message (and optional hint)
  // from a raw value that may be a JSON error envelope, a plain string, or an
  // Error instance.  The server returns {error, code?, hint?}; some paths
  // surface the full JSON body as the Error.message before the SDK can parse
  // it.  We normalise all cases here so the UI never renders raw JSON.
  function parseErrorEnvelope(raw: unknown): { message: string; hint: string } {
    const str = raw instanceof Error ? raw.message : String(raw);
    // SDK error messages prefix with "<resource>: HTTP <code>: <body>" — strip
    // that wrapper so we can parse the body cleanly.
    const httpMatch = /HTTP \d{3}: (\{.*\})$/s.exec(str);
    const body = httpMatch ? httpMatch[1]! : str;
    try {
      const parsed = JSON.parse(body) as Record<string, unknown>;
      const errField = parsed['error'];
      let message = '';
      if (typeof errField === 'string') {
        message = errField;
      } else if (errField && typeof errField === 'object') {
        const inner = errField as Record<string, unknown>;
        if (typeof inner['message'] === 'string') message = inner['message'];
      }
      if (!message && typeof parsed['message'] === 'string') message = parsed['message'];
      if (!message) message = str;
      const hint = typeof parsed['hint'] === 'string' ? parsed['hint'] : '';
      return { message, hint };
    } catch {
      return { message: str, hint: '' };
    }
  }

  async function handleGenerate(): Promise<void> {
    if (!inputText.trim()) {
      generateError = 'Input text is required.';
      generateErrorHint = '';
      return;
    }
    generating = true;
    generateError = '';
    generateErrorHint = '';
    result = null;
    try {
      const client = await getSDKClient();
      result = await client.embeddings.generate({
        text: inputText.trim(),
        provider: selectedProvider || undefined,
      });
      showToast({ type: 'success', message: 'Embedding generated.' });
    } catch (err: unknown) {
      const { message, hint } = parseErrorEnvelope(err);
      generateError = message;
      generateErrorHint = hint;
      showToast({ type: 'error', message: `Generation failed: ${message}` });
    } finally {
      generating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Set default provider
  // ---------------------------------------------------------------------------

  let settingDefault: string | null = $state(null);
  let togglingProvider: string | null = $state(null);

  async function toggleProviderStatus(p: EmbeddingProviderInfo): Promise<void> {
    // Treat 'available' as enabled; anything else as disabled
    const currentlyEnabled = p.status === 'available';
    togglingProvider = p.name;
    try {
      const client = await getSDKClient();
      await client.embeddings.setProviderStatus(p.name, !currentlyEnabled);
      showToast({ type: 'success', message: `Provider "${p.name}" ${currentlyEnabled ? 'disabled' : 'enabled'}.` });
      await fetchProviders();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast({ type: 'error', message: `Failed: ${msg}` });
    } finally {
      togglingProvider = null;
    }
  }

  async function setDefaultProvider(providerName: string): Promise<void> {
    settingDefault = providerName;
    try {
      const client = await getSDKClient();
      await client.embeddings.setDefaultProvider(providerName);
      showToast({ type: 'success', message: `"${providerName}" set as default provider.` });
      await fetchProviders();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast({ type: 'error', message: `Failed: ${msg}` });
    } finally {
      settingDefault = null;
    }
  }

  function previewValues(embedding: number[]): string {
    const values = embedding.slice(0, PREVIEW_COUNT).map((v) => v.toFixed(6));
    if (embedding.length > PREVIEW_COUNT) {
      values.push(`… +${embedding.length - PREVIEW_COUNT} more`);
    }
    return `[${values.join(', ')}]`;
  }
</script>

<div class="flex h-full flex-col">
  <div class="p-6 pb-0">
    <PageHeader title="Embeddings" subtitle="Vector embedding generation and provider management" icon={Layers}>
      {#snippet children()}
        <button
          class="btn btn-ghost btn-sm gap-2"
          onclick={fetchProviders}
          disabled={loadingProviders}
          aria-label="Refresh providers"
        >
          <RefreshCw class={`h-4 w-4${loadingProviders ? ' animate-spin' : ''}`} />
          Refresh
        </button>
      {/snippet}
    </PageHeader>
  </div>

  <div class="flex flex-1 flex-col gap-6 overflow-y-auto p-6">

    <!-- Provider Table -->
    <section class="card bg-base-200 shadow-sm">
      <div class="card-body">
        <h2 class="card-title text-base">Providers</h2>

        {#if providerError}
          <div class="alert alert-error text-sm">{providerError}</div>
        {:else if loadingProviders}
          <div class="flex justify-center py-6">
            <span class="loading loading-spinner loading-md"></span>
          </div>
        {:else if providers.length === 0}
          <div class="py-8 text-center text-sm text-base-content/40">
            No embedding providers registered.
          </div>
        {:else}
          <div class="overflow-x-auto">
            <table class="table table-sm">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Type</th>
                  <th>Model</th>
                  <th>Dimensions</th>
                  <th>Status</th>
                  <th>Enabled</th>
                  <th>Default</th>
                  <th><span class="sr-only">Actions</span></th>
                </tr>
              </thead>
              <tbody>
                {#each providers as p (p.name)}
                  <tr class={p.name === defaultProvider ? 'bg-primary/5' : ''}>
                    <td class="font-mono text-xs">{p.name}</td>
                    <td>{p.type}</td>
                    <td class="font-mono text-xs">{p.model}</td>
                    <td>{p.dimensions.toLocaleString()}</td>
                    <td>
                      <span
                        class="badge badge-sm"
                        class:badge-success={p.status === 'available'}
                        class:badge-error={p.status === 'unavailable'}
                        class:badge-warning={p.status !== 'available' && p.status !== 'unavailable'}
                      >
                        {p.status}
                      </span>
                    </td>
                    <td>
                      <input
                        type="checkbox"
                        class="toggle toggle-sm toggle-success"
                        checked={p.status === 'available'}
                        disabled={togglingProvider === p.name}
                        onchange={() => void toggleProviderStatus(p)}
                        title={p.status === 'available' ? `Disable provider ${p.name}` : `Enable provider ${p.name}`}
                        aria-label={p.status === 'available' ? `Disable provider ${p.name}` : `Enable provider ${p.name}`}
                      />
                    </td>
                    <td>
                      {#if p.name === defaultProvider}
                        <!--
                          The "default" indicator is informational, not
                          destructive — render it as an outline-success
                          pill rather than a filled brand color so it
                          never reads as a danger/error tag.
                        -->
                        <span
                          class="badge badge-success badge-outline badge-xs"
                          data-testid="default-provider-badge"
                        >
                          default
                        </span>
                      {/if}
                    </td>
                    <td>
                      {#if p.name !== defaultProvider}
                        <button
                          class="btn btn-ghost btn-xs"
                          disabled={settingDefault === p.name}
                          onclick={() => void setDefaultProvider(p.name)}
                          title={`Set ${p.name} as default provider`}
                          aria-label={`Set ${p.name} as default provider`}
                        >
                          <Star class="h-3 w-3" />
                        </button>
                      {/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    </section>

    <!-- Generate Embedding Panel -->
    <section class="card bg-base-200 shadow-sm">
      <div class="card-body gap-4">
        <h2 class="card-title text-base">Generate Embedding</h2>

        <label class="form-control w-full">
          <div class="label"><span class="label-text">Input Text</span></div>
          <textarea
            class="textarea textarea-bordered w-full"
            rows="4"
            placeholder="Enter text to embed…"
            bind:value={inputText}
          ></textarea>
        </label>

        <label class="form-control w-full max-w-xs">
          <div class="label"><span class="label-text">Provider (optional)</span></div>
          <select class="select select-bordered w-full" bind:value={selectedProvider}>
            <option value="">Server default</option>
            {#each providers as p (p.name)}
              <option value={p.name}>{p.name}</option>
            {/each}
          </select>
        </label>

        {#if generateError}
          <div class="alert alert-error text-sm">{generateError}</div>
        {/if}

        <button
          class="btn btn-primary w-fit gap-2"
          onclick={handleGenerate}
          disabled={generating}
          aria-label="Generate Embedding"
        >
          {#if generating}
            <span class="loading loading-spinner loading-xs"></span>
          {:else}
            <Zap class="h-4 w-4" />
          {/if}
          Generate
        </button>

        {#if result}
          <div class="rounded-lg bg-base-300 p-4 space-y-3">
            {#if result.metadata}
              <div class="grid grid-cols-2 gap-2 text-sm md:grid-cols-4">
                <div>
                  <span class="text-base-content/50 text-xs uppercase tracking-wider">Provider</span>
                  <p class="font-mono">{result.metadata.provider}</p>
                </div>
                <div>
                  <span class="text-base-content/50 text-xs uppercase tracking-wider">Model</span>
                  <p class="font-mono text-xs">{result.metadata.model}</p>
                </div>
                <div>
                  <span class="text-base-content/50 text-xs uppercase tracking-wider">Dimensions</span>
                  <p class="font-semibold">{result.embedding.length.toLocaleString()}</p>
                </div>
                <div>
                  <span class="text-base-content/50 text-xs uppercase tracking-wider">Cache Hit</span>
                  <p>{result.metadata.cache_hit ? 'yes' : 'no'}</p>
                </div>
              </div>
            {/if}
            <div>
              <span class="text-base-content/50 text-xs uppercase tracking-wider">Vector Preview</span>
              <p class="font-mono text-xs mt-1 text-base-content/70 break-all">
                {previewValues(result.embedding)}
              </p>
            </div>
          </div>
        {/if}
      </div>
    </section>

    <!-- Empty state when no result yet -->
    {#if !result && !generating}
      <div class="flex flex-1 flex-col items-center justify-center gap-3 py-8 text-base-content/30">
        <Cpu class="h-10 w-10" />
        <p class="text-sm">Enter text above to generate a vector embedding.</p>
      </div>
    {/if}

  </div>
</div>
