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
  App is the top-level state machine. It handles:
    loading - SDK and theme initializing
    app     - Fully ready, MainApp rendered
    error   - Unrecoverable startup failure with retry
-->
<script lang="ts">
  import MainApp from './views/MainApp.svelte';
  import ToastContainer from './components/core/ToastContainer.svelte';
  import { initTheme } from './stores/theme.svelte.ts';
  import { setConnected, setConnectionError } from './stores/connection.svelte.ts';

  // ---------------------------------------------------------------------------
  // App state machine
  // ---------------------------------------------------------------------------

  type AppState = 'loading' | 'app' | 'error';

  let appState: AppState = $state('loading');
  let errorMessage: string = $state('');

  // ---------------------------------------------------------------------------
  // Initialization
  // ---------------------------------------------------------------------------

  async function initApp(): Promise<void> {
    try {
      appState = 'loading';
      errorMessage = '';

      initTheme();

      // In web mode, the daemon is reached via proxy at window.location.origin.
      // Mark as connected optimistically; health polling will detect failures.
      const origin = window.location.origin;
      setConnected(origin, 'rest');

      appState = 'app';
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      errorMessage = msg;
      setConnectionError(msg);
      appState = 'error';
    }
  }

  function retry(): void {
    void initApp();
  }

  // Run once on mount.
  $effect(() => {
    void initApp();
  });
</script>

{#if appState === 'loading'}
  <div class="flex min-h-screen items-center justify-center">
    <div class="flex flex-col items-center gap-4">
      <div class="text-4xl font-bold gradient-text">QuicBaaS</div>
      <span class="loading loading-ring loading-lg text-primary"></span>
      <p class="text-sm text-base-content/60">Initializing...</p>
    </div>
  </div>

{:else if appState === 'app'}
  <MainApp />

{:else if appState === 'error'}
  <div class="flex min-h-screen items-center justify-center">
    <div class="card bg-base-200 w-96 shadow-xl">
      <div class="card-body items-center text-center">
        <h2 class="card-title text-error">Connection Error</h2>
        <p class="text-sm text-base-content/70 break-all">{errorMessage}</p>
        <div class="card-actions mt-4">
          <button class="btn btn-primary" onclick={retry}>Retry</button>
        </div>
      </div>
    </div>
  </div>
{/if}

<ToastContainer />
