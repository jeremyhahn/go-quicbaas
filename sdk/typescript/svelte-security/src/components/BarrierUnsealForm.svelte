<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.
-->

<!--
  BarrierUnsealForm submits a single share/password to the BarrierClient.
  Used by the software strategy where one password is enough; the Shamir
  strategy uses ShamirShareInput instead so it can show a progress meter.
-->
<script lang="ts">
  import type { BarrierClient, BarrierUnsealResponse } from '../types.ts';

  interface Props {
    client: BarrierClient;
    label?: string;
    placeholder?: string;
    onunseal?: (resp: BarrierUnsealResponse) => void;
    onerror?: (err: Error) => void;
  }

  const {
    client,
    label = 'Password',
    placeholder = 'Enter unseal password',
    onunseal,
    onerror,
  }: Props = $props();

  let value = $state('');
  let submitting = $state(false);
  let errorMsg: string | null = $state(null);

  async function submit(): Promise<void> {
    errorMsg = null;
    if (!value.trim()) {
      errorMsg = `${label} is required`;
      return;
    }
    submitting = true;
    try {
      const resp = await client.unseal({ share: value.trim() });
      value = '';
      onunseal?.(resp);
    } catch (e) {
      const err = e instanceof Error ? e : new Error(String(e));
      errorMsg = err.message;
      onerror?.(err);
    } finally {
      submitting = false;
    }
  }
</script>

<div class="space-y-3" data-testid="barrier-unseal-form">
  {#if errorMsg !== null}
    <div class="alert alert-error text-sm" role="alert">{errorMsg}</div>
  {/if}

  <div class="form-control">
    <label class="label label-text" for="barrier-unseal-input">{label}</label>
    <input
      id="barrier-unseal-input"
      class="input input-bordered input-sm font-mono"
      type="password"
      autocomplete="off"
      placeholder={placeholder}
      bind:value
    />
  </div>

  <button
    class="btn btn-primary btn-sm"
    disabled={submitting}
    onclick={submit}
  >
    {submitting ? 'Submitting...' : 'Unseal'}
  </button>
</div>
