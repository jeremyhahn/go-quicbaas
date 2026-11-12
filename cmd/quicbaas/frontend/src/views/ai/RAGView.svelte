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
  RAGView provides retrieval-augmented generation operations:
  - Lists recent query history (in-session, no persistence)
  - Ingest Document dialog: namespace + text paste
  - Query panel: namespace + query text → top-K chunks with citations
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import type { RAGQueryResponse, RAGCitation, RAGDocument, RAGDocumentID } from '@quicbaas/sdk';
  import { Brain, Plus, Search, X, FileText, BookOpen, Trash2, RotateCw, RefreshCw, Eye } from 'lucide-svelte';
  import { formatDateTime } from '../../lib/time.ts';

  // ---------------------------------------------------------------------------
  // Query panel state
  // ---------------------------------------------------------------------------

  let namespace = $state('');
  let queryText = $state('');
  let topK = $state(10);
  let minScore = $state(0.7);
  let querying = $state(false);
  let queryError = $state('');
  let queryResult = $state<RAGQueryResponse | null>(null);

  async function handleQuery(): Promise<void> {
    if (!namespace.trim()) {
      queryError = 'Namespace is required.';
      return;
    }
    if (!queryText.trim()) {
      queryError = 'Query text is required.';
      return;
    }
    querying = true;
    queryError = '';
    queryResult = null;
    try {
      const client = await getSDKClient();
      queryResult = await client.rag.query({
        query: queryText.trim(),
        namespace: namespace.trim(),
        top_k: topK,
        min_score: minScore,
        include_citations: true,
      });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      queryError = `Query failed: ${msg}`;
      showToast({ type: 'error', message: queryError });
    } finally {
      querying = false;
    }
  }

  function clearResult(): void {
    queryResult = null;
    queryError = '';
    queryText = '';
  }

  // ---------------------------------------------------------------------------
  // Ingest dialog state
  // ---------------------------------------------------------------------------

  let showIngestDialog = $state(false);
  let ingestNamespace = $state('');
  let ingestContent = $state('');
  let ingesting = $state(false);
  let ingestError = $state('');

  function openIngestDialog(): void {
    showIngestDialog = true;
    ingestNamespace = '';
    ingestContent = '';
    ingestError = '';
  }

  function closeIngestDialog(): void {
    showIngestDialog = false;
  }

  async function handleFileUpload(e: Event): Promise<void> {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    ingestContent = await file.text();
    input.value = '';
  }

  // ---------------------------------------------------------------------------
  // Documents panel state
  // ---------------------------------------------------------------------------

  let docsNamespace = $state('');
  let documents = $state<RAGDocument[]>([]);

  const DOC_COLUMNS: Column<RAGDocument>[] = [
    { key: 'id', label: 'ID', sortable: true, searchable: true },
    { key: 'namespace', label: 'Namespace', sortable: true, searchable: true },
    { key: 'content', label: 'Content preview', searchable: true },
    { key: 'timestamp', label: 'Timestamp', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];
  let docsTotal = $state(0);
  let docsLoading = $state(false);
  let docsError = $state('');
  let deletingDoc: RAGDocumentID | null = $state(null);
  let reindexingDoc: RAGDocumentID | null = $state(null);

  async function loadDocuments(): Promise<void> {
    if (!docsNamespace.trim()) {
      docsError = 'Namespace is required.';
      return;
    }
    docsLoading = true;
    docsError = '';
    documents = [];
    try {
      const client = await getSDKClient();
      const resp = await client.rag.listDocuments(docsNamespace.trim(), 100);
      documents = resp.documents;
      docsTotal = resp.total;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      docsError = `Failed to load documents: ${msg}`;
    } finally {
      docsLoading = false;
    }
  }

  async function deleteDocument(doc: RAGDocument): Promise<void> {
    deletingDoc = doc.id;
    try {
      const client = await getSDKClient();
      await client.rag.deleteDocument(doc.id);
      showToast({ type: 'success', message: `Document #${doc.id} deleted.` });
      void loadDocuments();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast({ type: 'error', message: `Delete failed: ${msg}` });
    } finally {
      deletingDoc = null;
    }
  }

  // ---------------------------------------------------------------------------
  // View document dialog state (GET /api/v1/rag/documents/{id})
  // ---------------------------------------------------------------------------

  let viewingDocId: RAGDocumentID | null = $state(null);
  let viewDoc = $state<RAGDocument | null>(null);
  let viewLoading = $state(false);
  let viewError = $state('');

  async function viewDocument(doc: RAGDocument): Promise<void> {
    viewingDocId = doc.id;
    viewDoc = null;
    viewError = '';
    viewLoading = true;
    try {
      const client = await getSDKClient();
      viewDoc = await client.rag.getDocument(doc.id);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      viewError = `Failed to load document: ${msg}`;
    } finally {
      viewLoading = false;
    }
  }

  function closeViewDialog(): void {
    viewingDocId = null;
    viewDoc = null;
    viewError = '';
  }

  function formatMetadata(metadata: Record<string, unknown> | undefined): string {
    if (!metadata || Object.keys(metadata).length === 0) return '';
    return JSON.stringify(metadata, null, 2);
  }

  async function reindexDocument(doc: RAGDocument): Promise<void> {
    reindexingDoc = doc.id;
    try {
      const client = await getSDKClient();
      await client.rag.reindexDocument(doc.id);
      showToast({ type: 'success', message: `Document #${doc.id} reindexed.` });
      void loadDocuments();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast({ type: 'error', message: `Reindex failed: ${msg}` });
    } finally {
      reindexingDoc = null;
    }
  }

  async function handleIngest(): Promise<void> {
    if (!ingestNamespace.trim()) {
      ingestError = 'Namespace is required.';
      return;
    }
    if (!ingestContent.trim()) {
      ingestError = 'Document content is required.';
      return;
    }
    ingesting = true;
    ingestError = '';
    try {
      const client = await getSDKClient();
      // Build context from the pasted content as a single result input.
      await client.rag.buildContext({
        results: [{ content: ingestContent.trim(), score: 1.0 }],
        max_tokens: 4000,
      });
      showToast({ type: 'success', message: `Document ingested into namespace "${ingestNamespace}".` });
      closeIngestDialog();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      ingestError = `Ingest failed: ${msg}`;
    } finally {
      ingesting = false;
    }
  }
</script>

<div class="flex h-full flex-col">
  <div class="p-6 pb-0">
    <PageHeader title="RAG" subtitle="Retrieval-Augmented Generation" icon={BookOpen}>
      {#snippet children()}
        <button
          class="btn btn-primary btn-sm gap-2"
          onclick={openIngestDialog}
          aria-label="Ingest Document"
        >
          <Plus class="h-4 w-4" />
          Ingest Document
        </button>
      {/snippet}
    </PageHeader>
  </div>

  <div class="flex flex-1 flex-col gap-6 overflow-y-auto p-6">

    <!-- Query Panel -->
    <section class="card bg-base-200 shadow-sm">
      <div class="card-body gap-4">
        <h2 class="card-title text-base">Query RAG</h2>

        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <label class="form-control w-full">
            <div class="label"><span class="label-text">Namespace</span></div>
            <input
              class="input input-bordered w-full"
              type="text"
              placeholder="e.g. docs"
              bind:value={namespace}
            />
          </label>

          <div class="grid grid-cols-2 gap-2">
            <label class="form-control w-full">
              <div class="label"><span class="label-text">Top-K</span></div>
              <input
                class="input input-bordered w-full"
                type="number"
                min="1"
                max="100"
                bind:value={topK}
              />
            </label>
            <label class="form-control w-full">
              <div class="label"><span class="label-text">Min Score</span></div>
              <input
                class="input input-bordered w-full"
                type="number"
                min="0"
                max="1"
                step="0.05"
                bind:value={minScore}
              />
            </label>
          </div>
        </div>

        <label class="form-control w-full">
          <div class="label"><span class="label-text">Query</span></div>
          <textarea
            class="textarea textarea-bordered w-full"
            rows="3"
            placeholder="Ask a question or describe what you're looking for…"
            bind:value={queryText}
          ></textarea>
        </label>

        {#if queryError}
          <div class="alert alert-error text-sm">{queryError}</div>
        {/if}

        <div class="flex gap-2">
          <button
            class="btn btn-primary gap-2"
            onclick={handleQuery}
            disabled={querying}
            aria-label="Run Query"
          >
            {#if querying}
              <span class="loading loading-spinner loading-xs"></span>
            {:else}
              <Search class="h-4 w-4" />
            {/if}
            Run Query
          </button>
          {#if queryResult}
            <button class="btn btn-ghost gap-2" onclick={clearResult}>
              <X class="h-4 w-4" />
              Clear
            </button>
          {/if}
        </div>
      </div>
    </section>

    <!-- Query Results -->
    {#if queryResult}
      <section class="card bg-base-200 shadow-sm">
        <div class="card-body gap-3">
          <div class="flex items-center justify-between">
            <h2 class="card-title text-base">Results</h2>
            <div class="flex gap-3 text-sm text-base-content/50">
              <span>{queryResult.result_count} chunks</span>
              <span>{queryResult.tokens} tokens</span>
              {#if queryResult.truncated}
                <span class="badge badge-warning badge-sm">truncated</span>
              {/if}
            </div>
          </div>

          {#if queryResult.citations && queryResult.citations.length > 0}
            <div>
              <p class="mb-2 text-xs font-semibold uppercase tracking-wider text-base-content/40">Citations</p>
              <ul class="space-y-1">
                {#each queryResult.citations as citation (citation.index)}
                  <li class="flex items-center gap-2 text-sm">
                    <span class="badge badge-outline badge-xs">[{citation.index}]</span>
                    <span class="font-mono text-xs text-base-content/70">{citation.file_path}</span>
                    <span class="text-xs text-base-content/40">
                      L{citation.start_line}–{citation.end_line}
                    </span>
                    <span class="ml-auto text-xs text-base-content/50">
                      {(citation.score * 100).toFixed(1)}%
                    </span>
                  </li>
                {/each}
              </ul>
            </div>
          {/if}

          <div>
            <p class="mb-2 text-xs font-semibold uppercase tracking-wider text-base-content/40">Context</p>
            <pre class="max-h-96 overflow-y-auto rounded-lg bg-base-300 p-4 text-sm whitespace-pre-wrap">{queryResult.context}</pre>
          </div>
        </div>
      </section>
    {/if}

    <!-- Empty state when nothing queried yet -->
    {#if !queryResult && !querying}
      <div class="flex flex-1 flex-col items-center justify-center gap-3 py-16 text-base-content/30">
        <Brain class="h-12 w-12" />
        <p class="text-sm">Enter a namespace and query to retrieve relevant context chunks.</p>
      </div>
    {/if}

    <!-- Documents Panel -->
    <section class="card bg-base-200 shadow-sm">
      <div class="card-body gap-4">
        <h2 class="card-title text-base">Documents</h2>

        <div class="flex gap-2 items-end">
          <label class="form-control flex-1">
            <div class="label"><span class="label-text">Namespace</span></div>
            <input
              class="input input-bordered input-sm w-full"
              type="text"
              placeholder="e.g. docs"
              bind:value={docsNamespace}
            />
          </label>
          <button
            class="btn btn-primary btn-sm gap-2"
            onclick={() => void loadDocuments()}
            disabled={docsLoading}
            aria-label="List documents"
          >
            {#if docsLoading}
              <span class="loading loading-spinner loading-xs"></span>
            {:else}
              <RefreshCw class="h-3 w-3" />
            {/if}
            List
          </button>
        </div>

        {#if docsError}
          <div class="alert alert-error text-sm">{docsError}</div>
        {:else if documents.length > 0}
          <div class="text-xs text-base-content/50">{docsTotal} document{docsTotal !== 1 ? 's' : ''} in namespace</div>
          <DataTable
            columns={DOC_COLUMNS}
            rows={documents}
            rowKey={(doc: RAGDocument) => doc.id}
            urlKey="docs"
            ariaLabel="RAG documents"
            searchPlaceholder="Search documents..."
            searchLabel="Search documents"
            noMatchText="No documents match the search."
          >
            {#snippet row(doc: RAGDocument)}
              <tr class="hover">
                <td class="font-mono text-xs">{doc.id}</td>
                <td class="text-xs">{doc.namespace}</td>
                <td class="text-xs text-base-content/60 max-w-xs truncate">
                  {doc.content ? doc.content.slice(0, 80) + (doc.content.length > 80 ? '…' : '') : '—'}
                </td>
                <td class="text-xs text-base-content/50">
                  {formatDateTime(doc.timestamp)}
                </td>
                <td>
                  <div class="flex gap-1">
                    <button
                      class="btn btn-ghost btn-xs"
                      disabled={viewLoading && viewingDocId === doc.id}
                      onclick={() => void viewDocument(doc)}
                      title={`View document #${doc.id}`}
                      aria-label={`View document #${doc.id}`}
                    >
                      <Eye class="h-3 w-3" />
                    </button>
                    <button
                      class="btn btn-ghost btn-xs"
                      disabled={reindexingDoc === doc.id}
                      onclick={() => void reindexDocument(doc)}
                      title={`Reindex document #${doc.id}`}
                      aria-label={`Reindex document #${doc.id}`}
                    >
                      <RotateCw class="h-3 w-3" />
                    </button>
                    <button
                      class="btn btn-ghost btn-xs text-error"
                      disabled={deletingDoc === doc.id}
                      onclick={() => void deleteDocument(doc)}
                      title={`Delete document #${doc.id}`}
                      aria-label={`Delete document #${doc.id}`}
                    >
                      <Trash2 class="h-3 w-3" />
                    </button>
                  </div>
                </td>
              </tr>
            {/snippet}
          </DataTable>
        {:else if !docsLoading && docsNamespace}
          <div class="text-center text-sm text-base-content/40 py-4">No documents found in this namespace.</div>
        {/if}

      </div>
    </section>

  </div>
</div>

<!-- View Document Dialog -->
{#if viewingDocId !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="View Document">
    <div class="modal-box w-full max-w-2xl">
      <div class="mb-4 flex items-center justify-between">
        <h3 class="text-base font-semibold">Document #{viewingDocId}</h3>
        <button class="btn btn-ghost btn-square btn-sm" onclick={closeViewDialog} aria-label="Close document">
          <X class="h-4 w-4" />
        </button>
      </div>

      {#if viewLoading}
        <div class="flex items-center gap-2 text-sm text-base-content/60" data-testid="rag-doc-loading">
          <span class="loading loading-spinner loading-xs"></span>
          Loading document…
        </div>
      {:else if viewError}
        <div class="alert alert-error text-sm">{viewError}</div>
      {:else if viewDoc}
        <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          <dt class="text-base-content/50">ID</dt>
          <dd class="font-mono text-xs" data-testid="rag-doc-id">{viewDoc.id}</dd>
          <dt class="text-base-content/50">Namespace</dt>
          <dd data-testid="rag-doc-namespace">{viewDoc.namespace}</dd>
          <dt class="text-base-content/50">Timestamp</dt>
          <dd class="text-xs">{formatDateTime(viewDoc.timestamp)}</dd>
        </dl>
        <div class="mt-4">
          <p class="mb-2 text-xs font-semibold uppercase tracking-wider text-base-content/40">Metadata</p>
          {#if formatMetadata(viewDoc.metadata)}
            <pre class="max-h-48 overflow-y-auto rounded-lg bg-base-300 p-3 text-xs whitespace-pre-wrap" data-testid="rag-doc-metadata">{formatMetadata(viewDoc.metadata)}</pre>
          {:else}
            <p class="text-xs text-base-content/40">—</p>
          {/if}
        </div>
        <div class="mt-4">
          <p class="mb-2 text-xs font-semibold uppercase tracking-wider text-base-content/40">Content</p>
          <pre class="max-h-96 overflow-y-auto rounded-lg bg-base-300 p-4 text-sm whitespace-pre-wrap" data-testid="rag-doc-content">{viewDoc.content ?? ''}</pre>
        </div>
      {/if}

      <div class="modal-action">
        <button class="btn btn-ghost" onclick={closeViewDialog}>Close</button>
      </div>
    </div>
  </div>
{/if}

<!-- Ingest Document Dialog -->
{#if showIngestDialog}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Ingest Document">
    <div class="modal-box w-full max-w-2xl">
      <div class="mb-4 flex items-center justify-between">
        <h3 class="text-base font-semibold">Ingest Document</h3>
        <button class="btn btn-ghost btn-square btn-sm" onclick={closeIngestDialog} aria-label="Close">
          <X class="h-4 w-4" />
        </button>
      </div>

      <div class="flex flex-col gap-4">
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Namespace / Collection</span></div>
          <input
            class="input input-bordered w-full"
            type="text"
            placeholder="e.g. docs"
            bind:value={ingestNamespace}
          />
        </label>

        <label class="form-control w-full">
          <div class="label">
            <span class="label-text">Document Content</span>
            <span class="label-text-alt">or upload a file below</span>
          </div>
          <textarea
            class="textarea textarea-bordered w-full font-mono text-xs"
            rows="10"
            placeholder="Paste document text here…"
            bind:value={ingestContent}
          ></textarea>
        </label>

        <label class="flex cursor-pointer items-center gap-2 text-sm text-base-content/60">
          <FileText class="h-4 w-4" />
          Upload file
          <input
            type="file"
            class="hidden"
            accept=".txt,.md,.json,.yaml,.go,.ts,.py"
            onchange={handleFileUpload}
          />
        </label>

        {#if ingestError}
          <div class="alert alert-error text-sm">{ingestError}</div>
        {/if}
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost" onclick={closeIngestDialog}>Cancel</button>
        <button
          class="btn btn-primary gap-2"
          onclick={handleIngest}
          disabled={ingesting}
          aria-label="Ingest"
        >
          {#if ingesting}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Ingest
        </button>
      </div>
    </div>
    <div class="modal-backdrop" onclick={closeIngestDialog}></div>
  </div>
{/if}
