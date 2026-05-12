<!--
  DeployRemoteAgentDialog collects the flag surface for `quicbaas agent deploy`
  and forwards the request to the SDK client's orchestrator.deployAgent.

  The dialog is only mounted when the active SDK transport is 'wails' —
  other transports reject deploy with DeployNotSupportedError, so the
  button that opens this dialog is gated upstream.

  Auth method radio:
    - private-key  → local private key file path (original behaviour)
    - stored-key   → pick from a list returned by client.sshKeys.list()
    - password     → username + password via SSH Password auth
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import type { DeployAgentResult, DeployAgentSpec, SSHKey } from '@quicbaas/sdk';

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
  // Auth method
  // ---------------------------------------------------------------------------

  type AuthMethod = 'private-key' | 'stored-key' | 'password';
  let authMethod: AuthMethod = $state('private-key');

  // private-key fields
  let sshKeyPath = $state('');

  // stored-key fields
  let storedKeys: SSHKey[] = $state([]);
  let storedKeysLoading = $state(false);
  let selectedKeyName = $state('');

  // password fields (username already in sshUser)
  let sshPassword = $state('');

  // ---------------------------------------------------------------------------
  // Common form state
  // ---------------------------------------------------------------------------

  let sshUser = $state('');
  let sshHost = $state('');
  let insecureHostKey = $state(true);
  let server = $state('');
  let tenant = $state('default');
  let agentBind = $state('0.0.0.0:9091');
  let backendsCSV = $state('acp');
  let binary = $state('bin/quicbaas-static');
  let installDir = $state('');
  let dataDir = $state('');
  let systemd = $state<'auto' | 'yes' | 'no'>('auto');
  let waitOnlineSec = $state('120');

  // ---------------------------------------------------------------------------
  // Progress + result state
  // ---------------------------------------------------------------------------

  type Step = 'idle' | 'ssh' | 'upload' | 'start' | 'wait' | 'done' | 'error';
  let step: Step = $state('idle');
  let errorMessage = $state('');
  let result: DeployAgentResult | null = $state(null);

  // ---------------------------------------------------------------------------
  // Load stored SSH keys when the stored-key method is selected
  // ---------------------------------------------------------------------------

  async function loadStoredKeys(): Promise<void> {
    storedKeysLoading = true;
    try {
      const client = await getSDKClient();
      const resp = await client.sshKeys.list();
      storedKeys = Array.isArray(resp.keys) ? resp.keys : [];
      if (storedKeys.length > 0 && selectedKeyName === '') {
        selectedKeyName = storedKeys[0]?.name ?? '';
      }
    } catch {
      storedKeys = [];
    } finally {
      storedKeysLoading = false;
    }
  }

  $effect(() => {
    if (authMethod === 'stored-key' && open) {
      void loadStoredKeys();
    }
  });

  // ---------------------------------------------------------------------------
  // Submit handler
  // ---------------------------------------------------------------------------

  async function handleSubmit(): Promise<void> {
    errorMessage = '';
    result = null;

    const backends = backendsCSV
      .split(',')
      .map((b) => b.trim())
      .filter((b) => b !== '');

    if (backends.length === 0) {
      errorMessage = 'At least one backend is required';
      step = 'error';
      return;
    }

    const spec: DeployAgentSpec = {
      host: sshHost,
      ssh_user: sshUser,
      insecure_host_key: insecureHostKey,
      server,
      tenant,
      agent_bind: agentBind,
      backends,
      binary,
      install_dir: installDir || undefined,
      data_dir: dataDir || undefined,
      systemd: systemd === 'auto' ? undefined : systemd === 'yes',
      wait_online: waitOnlineSecondsToNanos(waitOnlineSec),
    };

    // Apply auth-method-specific fields.
    if (authMethod === 'private-key') {
      spec.ssh_key_path = sshKeyPath || undefined;
    } else if (authMethod === 'stored-key') {
      spec.ssh_key_path = selectedKeyName || undefined;
    } else if (authMethod === 'password') {
      spec.ssh_password = sshPassword || undefined;
    }

    try {
      step = 'ssh';
      const client = await getSDKClient();
      step = 'upload';
      const resp = await client.orchestrator.deployAgent(spec);
      step = waitOnlineSecondsToNanos(waitOnlineSec) > 0 ? 'wait' : 'start';
      result = resp;
      step = 'done';
    } catch (err: unknown) {
      errorMessage = err instanceof Error ? err.message : String(err);
      step = 'error';
    }
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape' && step !== 'ssh' && step !== 'upload' && step !== 'wait') {
      onClose?.();
    }
  }

  function handleClose(): void {
    if (step === 'ssh' || step === 'upload' || step === 'wait') return;
    step = 'idle';
    result = null;
    errorMessage = '';
    onClose?.();
  }

  function waitOnlineSecondsToNanos(value: string): number {
    const sec = parseInt(value, 10);
    if (isNaN(sec) || sec <= 0) return 0;
    return sec * 1_000_000_000;
  }

  const inFlight = $derived(step === 'ssh' || step === 'upload' || step === 'wait');

  const submitDisabled = $derived(
    inFlight ||
      sshUser === '' ||
      sshHost === '' ||
      server === '' ||
      tenant === '' ||
      (authMethod === 'stored-key' && selectedKeyName === ''),
  );
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<dialog
  class="modal"
  class:modal-open={open}
  aria-modal="true"
  aria-label="Deploy remote agent"
  onkeydown={handleKeydown}
>
  <div class="modal-box w-full max-w-2xl">
    <h3 class="text-base font-semibold mb-4">Deploy Remote Agent</h3>

    <form
      class="grid grid-cols-2 gap-3"
      onsubmit={(e) => { e.preventDefault(); void handleSubmit(); }}
    >
      <!-- SSH user / host -->
      <label class="form-control">
        <div class="label"><span class="label-text">SSH user</span></div>
        <input
          type="text"
          class="input input-bordered input-sm"
          bind:value={sshUser}
          placeholder="deploy"
          required
          aria-label="SSH user"
        />
      </label>
      <label class="form-control">
        <div class="label"><span class="label-text">SSH host</span></div>
        <input
          type="text"
          class="input input-bordered input-sm"
          bind:value={sshHost}
          placeholder="node01.example.com[:port]"
          required
          aria-label="SSH host"
        />
      </label>

      <!-- Auth method radio -->
      <div class="col-span-2">
        <div class="label"><span class="label-text font-medium">Authentication method</span></div>
        <div class="flex items-center gap-6" role="radiogroup" aria-label="Authentication method">
          <label class="flex items-center gap-2 cursor-pointer">
            <input
              type="radio"
              class="radio radio-sm"
              name="authMethod"
              value="private-key"
              bind:group={authMethod}
              aria-label="Private Key File"
            />
            <span class="text-sm">Private Key File</span>
          </label>
          <label class="flex items-center gap-2 cursor-pointer">
            <input
              type="radio"
              class="radio radio-sm"
              name="authMethod"
              value="stored-key"
              bind:group={authMethod}
              aria-label="Stored SSH Key"
            />
            <span class="text-sm">Stored SSH Key</span>
          </label>
          <label class="flex items-center gap-2 cursor-pointer">
            <input
              type="radio"
              class="radio radio-sm"
              name="authMethod"
              value="password"
              bind:group={authMethod}
              aria-label="Username + Password"
            />
            <span class="text-sm">Username + Password</span>
          </label>
        </div>
      </div>

      <!-- Auth method fields -->
      {#if authMethod === 'private-key'}
        <label class="form-control col-span-2">
          <div class="label"><span class="label-text">SSH key path</span></div>
          <input
            type="text"
            class="input input-bordered input-sm"
            bind:value={sshKeyPath}
            placeholder="~/.ssh/id_ed25519"
            aria-label="SSH key path"
          />
        </label>

      {:else if authMethod === 'stored-key'}
        <div class="form-control col-span-2">
          <div class="label"><span class="label-text">Stored SSH key</span></div>
          {#if storedKeysLoading}
            <div class="flex items-center gap-2 text-sm text-base-content/50">
              <span class="loading loading-spinner loading-xs"></span>
              Loading stored keys…
            </div>
          {:else if storedKeys.length === 0}
            <div class="text-sm text-base-content/40 italic">
              No stored keys found. Add SSH keys via the Security section, or choose another auth method.
            </div>
          {:else}
            <select
              class="select select-bordered select-sm"
              bind:value={selectedKeyName}
              aria-label="Stored SSH key"
            >
              {#each storedKeys as key (key.name)}
                <option value={key.name}>{key.name} — {key.fingerprint}</option>
              {/each}
            </select>
          {/if}
        </div>

      {:else if authMethod === 'password'}
        <label class="form-control col-span-2">
          <div class="label"><span class="label-text">SSH password</span></div>
          <input
            type="password"
            class="input input-bordered input-sm"
            bind:value={sshPassword}
            placeholder="SSH password"
            aria-label="SSH password"
          />
        </label>
      {/if}

      <label class="form-control col-span-2 flex-row items-center gap-3">
        <input type="checkbox" class="checkbox checkbox-sm" bind:checked={insecureHostKey} aria-label="Insecure host key" />
        <span class="label-text">Trust remote host key (insecure)</span>
      </label>

      <!-- Server / tenant / bind -->
      <label class="form-control">
        <div class="label"><span class="label-text">Server URL</span></div>
        <input
          type="text"
          class="input input-bordered input-sm"
          bind:value={server}
          placeholder="https://quicbaas.example.com:8443"
          required
          aria-label="Server URL"
        />
      </label>
      <label class="form-control">
        <div class="label"><span class="label-text">Tenant</span></div>
        <input
          type="text"
          class="input input-bordered input-sm"
          bind:value={tenant}
          required
          aria-label="Tenant"
        />
      </label>

      <label class="form-control">
        <div class="label"><span class="label-text">Agent bind</span></div>
        <input
          type="text"
          class="input input-bordered input-sm"
          bind:value={agentBind}
          aria-label="Agent bind"
        />
      </label>
      <label class="form-control">
        <div class="label"><span class="label-text">Backends (CSV)</span></div>
        <input
          type="text"
          class="input input-bordered input-sm"
          bind:value={backendsCSV}
          placeholder="acp,ollama"
          aria-label="Backends"
        />
      </label>

      <!-- Binary + dirs -->
      <label class="form-control">
        <div class="label"><span class="label-text">Local binary</span></div>
        <input
          type="text"
          class="input input-bordered input-sm"
          bind:value={binary}
          aria-label="Binary"
        />
      </label>
      <label class="form-control">
        <div class="label"><span class="label-text">Install dir (remote)</span></div>
        <input
          type="text"
          class="input input-bordered input-sm"
          bind:value={installDir}
          placeholder="~/.quicbaas/bin"
          aria-label="Install dir"
        />
      </label>
      <label class="form-control">
        <div class="label"><span class="label-text">Data dir (remote)</span></div>
        <input
          type="text"
          class="input input-bordered input-sm"
          bind:value={dataDir}
          placeholder="~/.quicbaas/agent"
          aria-label="Data dir"
        />
      </label>
      <label class="form-control">
        <div class="label"><span class="label-text">Supervisor</span></div>
        <select
          class="select select-bordered select-sm"
          bind:value={systemd}
          aria-label="Supervisor"
        >
          <option value="auto">Auto-detect</option>
          <option value="yes">systemd --user</option>
          <option value="no">nohup fallback</option>
        </select>
      </label>

      <label class="form-control">
        <div class="label"><span class="label-text">Wait online (s)</span></div>
        <input
          type="text"
          inputmode="numeric"
          pattern="[0-9]*"
          class="input input-bordered input-sm"
          bind:value={waitOnlineSec}
          aria-label="Wait online"
        />
      </label>
      <!-- Progress / result -->
      <div class="col-span-2">
        {#if step !== 'idle'}
          <div class="mt-2 text-sm">
            <div class="font-semibold mb-1">Status</div>
            <ul class="steps w-full">
              <li class="step" class:step-primary={['ssh','upload','start','wait','done'].includes(step)}>SSH</li>
              <li class="step" class:step-primary={['upload','start','wait','done'].includes(step)}>Upload</li>
              <li class="step" class:step-primary={['start','wait','done'].includes(step)}>Start</li>
              <li class="step" class:step-primary={['wait','done'].includes(step)}>Online</li>
            </ul>
          </div>
        {/if}

        {#if step === 'done' && result !== null}
          <div class="alert alert-success mt-3 py-2 px-3 text-xs" role="status">
            <div>
              Deployed to <span class="font-mono">{result.host}</span> via {result.service_status}.
              {#if result.agent_id}
                <span>Agent ID: <span class="font-mono">{result.agent_id}</span></span>
              {/if}
            </div>
          </div>
        {/if}

        {#if step === 'error' && errorMessage !== ''}
          <div class="alert alert-error mt-3 py-2 px-3 text-xs" role="alert">{errorMessage}</div>
        {/if}
      </div>

      <div class="modal-action col-span-2 mt-2">
        <button
          type="button"
          class="btn btn-ghost btn-sm"
          onclick={handleClose}
          disabled={inFlight}
        >
          {#if step === 'done'}
            Close
          {:else}
            Cancel
          {/if}
        </button>
        <button
          type="submit"
          class="btn btn-primary btn-sm"
          disabled={submitDisabled}
          aria-label="Deploy agent"
        >
          {#if inFlight}
            <span class="loading loading-spinner loading-xs"></span>
            Deploying…
          {:else}
            Deploy
          {/if}
        </button>
      </div>
    </form>
  </div>

  <!-- Backdrop click closes the dialog (but not during a deploy). -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onclick={handleClose} onkeydown={() => {}}></div>
</dialog>
