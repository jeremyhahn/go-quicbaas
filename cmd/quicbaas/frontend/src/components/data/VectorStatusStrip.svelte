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
  VectorStatusStrip shows the vector store's health, its cluster stats and the
  admin global stats. Each is loaded on its own, so one failing leaves the
  others shown.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import type { VectorHealthResponse, VectorClusterStatsResponse, ClusterStatsResponse } from '@quicbaas/sdk';

  let health = $state<VectorHealthResponse | null>(null);
  let cluster = $state<VectorClusterStatsResponse | null>(null);
  let globalStats = $state<ClusterStatsResponse | null>(null);
  let healthError = $state('');
  let clusterError = $state('');
  let globalError = $state('');

  function errText(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
  }

  async function load(): Promise<void> {
    let client;
    try {
      client = await getSDKClient();
    } catch (err: unknown) {
      healthError = clusterError = globalError = errText(err);
      return;
    }
    await Promise.all([
      Promise.resolve().then(() => client.vectors.health()).then(
        (v) => { health = v; },
        (err: unknown) => { healthError = errText(err); },
      ),
      Promise.resolve().then(() => client.vectors.clusterStats()).then(
        (v) => { cluster = v; },
        (err: unknown) => { clusterError = errText(err); },
      ),
      Promise.resolve().then(() => client.vectorAdmin.getGlobalStats()).then(
        (v) => { globalStats = v; },
        (err: unknown) => { globalError = errText(err); },
      ),
    ]);
  }

  $effect(() => { void load(); });
</script>

<div class="grid gap-3 sm:grid-cols-3 mb-4" aria-label="Vector store status">
  <div class="card card-sm bg-base-200">
    <div class="card-body gap-1 text-xs">
      <p class="font-semibold text-base-content/50 uppercase tracking-wider">Health</p>
      {#if healthError}
        <p class="text-error" data-testid="vector-health-error">{healthError}</p>
      {:else if health}
        <p data-testid="vector-health">
          <span class="badge badge-sm" class:badge-success={health.status === 'healthy'} class:badge-warning={health.status !== 'healthy'}>{health.status}</span>
          · v{health.version} · node {health.node_id}
        </p>
        <p class="text-base-content/50">
          {health.namespaces_healthy} healthy · {health.namespaces_degraded} degraded · {health.namespaces_unhealthy} unhealthy
        </p>
      {:else}
        <span class="loading loading-spinner loading-xs"></span>
      {/if}
    </div>
  </div>

  <div class="card card-sm bg-base-200">
    <div class="card-body gap-1 text-xs">
      <p class="font-semibold text-base-content/50 uppercase tracking-wider">Cluster</p>
      {#if clusterError}
        <p class="text-error" data-testid="vector-cluster-error">{clusterError}</p>
      {:else if cluster}
        <p data-testid="vector-cluster">
          {cluster.total_nodes} nodes · {cluster.total_namespaces} namespaces · {cluster.total_vectors} vectors
        </p>
        <p class="text-base-content/50">{cluster.cluster_qps} qps</p>
      {:else}
        <span class="loading loading-spinner loading-xs"></span>
      {/if}
    </div>
  </div>

  <div class="card card-sm bg-base-200">
    <div class="card-body gap-1 text-xs">
      <p class="font-semibold text-base-content/50 uppercase tracking-wider">Storage</p>
      {#if globalError}
        <p class="text-error" data-testid="vector-global-error">{globalError}</p>
      {:else if globalStats}
        <p data-testid="vector-global">
          {globalStats.total_storage_bytes} B stored · {globalStats.total_index_bytes} B index
        </p>
        <p class="text-base-content/50">{globalStats.node_stats?.length ?? 0} node reports</p>
      {:else}
        <span class="loading loading-spinner loading-xs"></span>
      {/if}
    </div>
  </div>
</div>
