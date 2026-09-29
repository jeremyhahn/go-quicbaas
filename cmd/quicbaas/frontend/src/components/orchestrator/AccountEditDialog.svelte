<!--
  AccountEditDialog edits a pooled account's non-secret settings: its name,
  how it is billed (the subscription fee is what the Cost page shares over a
  range), how many sessions it may serve at once, and its spend limits. The
  credential is not here: it is write-only and has its own rotate dialog.

  The form and the request it becomes live in lib/accountEdit.ts, so every
  conversion and refusal is tested there rather than re-derived in this
  template.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import type { Account } from '@quicbaas/sdk';
  import SpendLimitsEditor from './SpendLimitsEditor.svelte';
  import {
    BILLING_CHOICES,
    accountEditForm,
    accountUpdateRequest,
    type AccountEditForm,
  } from '../../lib/accountEdit.ts';

  let {
    account,
    tenantID,
    onclose,
    onsaved,
  }: {
    account: Account;
    tenantID: string;
    onclose: () => void;
    onsaved: () => void;
  } = $props();

  // The form is taken from the account once, when the dialog opens: the
  // dialog is mounted per account, and re-reading the prop would discard what
  // the operator has typed.
  let form = $state<AccountEditForm>(untrack(() => accountEditForm(account)));
  let error = $state('');
  let saving = $state(false);

  async function save(): Promise<void> {
    const built = accountUpdateRequest(tenantID, account, form);
    if ('error' in built) {
      error = built.error;
      return;
    }
    saving = true;
    error = '';
    try {
      const client = await getSDKClient();
      await client.accounts.updateAccount(built.request);
      showToast('success', `Saved "${form.displayName.trim()}"`);
      onsaved();
    } catch (err: unknown) {
      error = `Save failed: ${err instanceof Error ? err.message : String(err)}`;
    } finally {
      saving = false;
    }
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<dialog
  open
  class="modal modal-open"
  aria-modal="true"
  aria-label="Edit account"
  data-testid="account-edit-dialog"
  onkeydown={(e) => { if (e.key === 'Escape') onclose(); }}
>
  <div class="modal-box max-w-xl">
    <h3 class="text-base font-semibold">Edit {account.display_name || account.id}</h3>
    <p class="mt-1 text-xs text-base-content/60">
      Billing decides what each turn adds to the bill; limits and the session cap decide when this
      account stops taking work.
    </p>

    <div class="mt-5 space-y-5">
      <label class="block" for="acct-edit-name">
        <span class="mb-1.5 block text-sm font-medium">Display name</span>
        <input id="acct-edit-name" type="text" class="input input-sm w-full bg-base-100" bind:value={form.displayName} />
      </label>

      <div class="grid gap-4 sm:grid-cols-2">
        <label class="block" for="acct-edit-billing">
          <span class="mb-1.5 block text-sm font-medium">Billing</span>
          <select id="acct-edit-billing" class="select select-sm w-full bg-base-100" bind:value={form.billing}>
            {#each BILLING_CHOICES as choice (choice.value)}
              <option value={choice.value}>{choice.label}</option>
            {/each}
          </select>
        </label>
        <label class="block" for="acct-edit-fee">
          <span class="mb-1.5 block text-sm font-medium">Monthly fee ($)</span>
          <input
            id="acct-edit-fee"
            type="text"
            inputmode="decimal"
            class="input input-sm w-full bg-base-100 tabular-nums"
            placeholder={form.billing === 'subscription' ? 'e.g. 200' : 'subscriptions only'}
            disabled={form.billing !== 'subscription'}
            bind:value={form.monthlyFee}
          />
        </label>
      </div>

      <div class="max-w-xs">
        <label class="mb-1.5 block text-sm font-medium" for="acct-edit-sessions">Max concurrent sessions</label>
        <input
          id="acct-edit-sessions"
          type="text"
          inputmode="numeric"
          class="input input-sm w-full bg-base-100 tabular-nums"
          placeholder="no cap"
          aria-describedby="acct-edit-sessions-help"
          bind:value={form.maxSessions}
        />
        <span id="acct-edit-sessions-help" class="mt-1 block text-xs text-base-content/60">Blank or 0 lets the account serve any number.</span>
      </div>

      <SpendLimitsEditor
        idPrefix="acct-edit"
        bind:rows={form.spendRows}
        help="A spent window takes the account out of the pool until the window frees."
      />
    </div>

    {#if error !== ''}
      <p role="alert" class="mt-4 text-sm text-error" data-testid="account-edit-error">{error}</p>
    {/if}

    <div class="modal-action">
      <button class="btn btn-sm btn-ghost" type="button" onclick={onclose}>Cancel</button>
      <button class="btn btn-sm btn-primary" type="button" disabled={saving} onclick={() => { void save(); }}>
        {saving ? 'Saving…' : 'Save'}
      </button>
    </div>
  </div>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onclick={onclose} onkeydown={() => {}}></div>
</dialog>
