/**
 * accountEdit.ts — editing a pooled account's non-secret settings: its name,
 * how it is billed, how many sessions it may serve at once, and what it may
 * spend.
 *
 * The rules live here rather than in the dialog so they are testable: the
 * form is built from the account as it stands, and the request is built from
 * the form with every conversion (dollars to micro-dollars, hours to
 * nanoseconds) in one place. A value the server would refuse is refused here
 * first, with a sentence that says which field is wrong.
 */

import type { Account, AccountBilling, UpdateAccountRequest } from '@quicbaas/sdk';
import { spendLimitsFromRows, spendRowsFrom, spendRowsProblem, type SpendWindowRow } from './spendLimits.ts';

const MICROS_PER_DOLLAR = 1_000_000;

/** BillingChoice is the billing control's value; '' is "not configured". */
export type BillingChoice = '' | 'subscription' | 'api';

/** BILLING_CHOICES lists the billing modes in the order the dialog offers them. */
export const BILLING_CHOICES: { value: BillingChoice; label: string }[] = [
  { value: '', label: 'Not configured' },
  { value: 'subscription', label: 'Subscription (flat monthly fee)' },
  { value: 'api', label: 'Metered API (billed at list price)' },
];

/** AccountEditForm is the dialog's editable state. */
export interface AccountEditForm {
  displayName: string;
  billing: BillingChoice;
  /** monthlyFee is the subscription fee in dollars, as typed. */
  monthlyFee: string;
  /** maxSessions is the concurrent-session cap as typed; blank or 0 is unmetered. */
  maxSessions: string;
  spendRows: SpendWindowRow[];
}

/** accountEditForm is the form for an account as it stands. */
export function accountEditForm(account: Account): AccountEditForm {
  const fee = account.billing?.monthly_fee_micros ?? 0;
  return {
    displayName: account.display_name ?? '',
    billing: account.billing?.mode ?? '',
    monthlyFee: fee > 0 ? String(fee / MICROS_PER_DOLLAR) : '',
    maxSessions: account.max_concurrent_sessions ? String(account.max_concurrent_sessions) : '',
    spendRows: spendRowsFrom(account.spend?.quota),
  };
}

/**
 * accountUpdateRequest builds the update for form, or says why it cannot.
 *
 * Billing, spend and the session cap are always sent, because each one in the
 * form is the whole of that setting: an empty billing object clears billing,
 * an empty quota makes the account unmetered, and 0 lifts the session cap. The
 * account's circuit-breaker rule is not edited here and is carried over
 * unchanged, since a spend update replaces the whole spend policy.
 */
export function accountUpdateRequest(
  tenantID: string,
  account: Account,
  form: AccountEditForm,
): { request: UpdateAccountRequest } | { error: string } {
  const displayName = form.displayName.trim();
  if (displayName === '') return { error: 'The display name is required.' };

  const billing: AccountBilling = {};
  if (form.billing !== '') billing.mode = form.billing;
  const feeText = form.monthlyFee.trim();
  if (feeText !== '') {
    if (form.billing !== 'subscription') {
      return { error: 'Only a subscription has a monthly fee; clear the fee or choose Subscription.' };
    }
    const fee = Number(feeText);
    if (!Number.isFinite(fee) || fee < 0) return { error: 'The monthly fee must be a non-negative number of dollars.' };
    const micros = Math.round(fee * MICROS_PER_DOLLAR);
    if (micros > 0) billing.monthly_fee_micros = micros;
  }

  const sessionsText = form.maxSessions.trim();
  let maxSessions = 0;
  if (sessionsText !== '') {
    maxSessions = Number(sessionsText);
    if (!Number.isInteger(maxSessions) || maxSessions < 0) {
      return { error: 'Max concurrent sessions must be a whole number, 0 for no cap.' };
    }
  }

  const spendProblem = spendRowsProblem(form.spendRows);
  if (spendProblem !== null) return { error: spendProblem };

  const request: UpdateAccountRequest = {
    tenant_id: tenantID,
    account_id: account.id,
    display_name: displayName,
    billing,
    spend: {
      quota: spendLimitsFromRows(form.spendRows),
      ...(account.spend?.breaker ? { breaker: account.spend.breaker } : {}),
    },
    max_concurrent_sessions: maxSessions,
  };
  return { request };
}

/**
 * accountSettingsSummary is the one-line summary an account row shows of the
 * settings this dialog edits, so an operator can see which accounts are
 * capped and how they bill without opening each one. A setting nobody made
 * says so rather than showing a zero.
 */
export function accountSettingsSummary(account: Account): string[] {
  const parts: string[] = [];
  const mode = account.billing?.mode;
  const fee = account.billing?.monthly_fee_micros ?? 0;
  if (mode === 'subscription') {
    parts.push(fee > 0 ? `Subscription · ${formatDollars(fee)}/mo` : 'Subscription · fee not set');
  } else if (mode === 'api') {
    parts.push('Metered API');
  } else {
    parts.push('Billing not configured');
  }
  const cap = account.max_concurrent_sessions ?? 0;
  parts.push(cap > 0 ? `max ${cap} session${cap === 1 ? '' : 's'}` : 'no session cap');
  const windows = account.spend?.quota?.windows?.length ?? 0;
  parts.push(windows > 0 ? `${windows} spend limit${windows === 1 ? '' : 's'}` : 'no spend limit');
  return parts;
}

/** formatDollars renders micro-dollars as whole dollars and cents. */
function formatDollars(micros: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(micros / MICROS_PER_DOLLAR);
}
