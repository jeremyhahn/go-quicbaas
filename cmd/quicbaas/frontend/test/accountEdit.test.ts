/**
 * accountEdit.test.ts — the rules the account edit dialog saves by: the form
 * is the account as it stands, every conversion is exact, and a value the
 * server would refuse is refused before the round trip with a sentence that
 * names the field.
 */

import { describe, it, expect } from 'vitest';
import type { Account } from '@quicbaas/sdk';
import { accountEditForm, accountSettingsSummary, accountUpdateRequest } from '../src/lib/accountEdit.ts';

const NANOS_PER_HOUR = 3_600_000_000_000;

function account(overrides: Partial<Account> = {}): Account {
  return {
    id: 'acct-1',
    tenant_id: 'acme',
    provider: 'claude',
    display_name: 'claude3',
    default: false,
    status: 'active',
    refresh_policy: 'static_token',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

describe('accountEditForm', () => {
  it('reads the account as it stands, in the units a person types', () => {
    const form = accountEditForm(
      account({
        billing: { mode: 'subscription', monthly_fee_micros: 200_000_000 },
        max_concurrent_sessions: 2,
        spend: { quota: { windows: [{ name: 'five-hour', size: 5 * NANOS_PER_HOUR, max_tokens: 1000 }] } },
      }),
    );
    expect(form).toMatchObject({ displayName: 'claude3', billing: 'subscription', monthlyFee: '200', maxSessions: '2' });
    expect(form.spendRows).toEqual([{ name: 'five-hour', hours: 5, tokens: 1000, usd: 0 }]);
  });

  it('leaves unset settings blank rather than zero', () => {
    const form = accountEditForm(account({ display_name: undefined }));
    expect(form).toEqual({ displayName: '', billing: '', monthlyFee: '', maxSessions: '', spendRows: [] });
  });
});

describe('accountUpdateRequest', () => {
  it('sends the whole of each setting, converted, and keeps the breaker rule', () => {
    const breaker = { window: 60_000_000_000, failure_threshold: 3, cooldown: 300_000_000_000 };
    const current = account({ spend: { breaker } });
    const built = accountUpdateRequest('acme', current, {
      displayName: ' claude3 ',
      billing: 'subscription',
      monthlyFee: '200',
      maxSessions: '3',
      spendRows: [{ name: 'weekly', hours: 168, tokens: 0, usd: 50 }],
    });
    expect('request' in built).toBe(true);
    if (!('request' in built)) return;
    expect(built.request).toEqual({
      tenant_id: 'acme',
      account_id: 'acct-1',
      display_name: 'claude3',
      billing: { mode: 'subscription', monthly_fee_micros: 200_000_000 },
      spend: { quota: { windows: [{ name: 'weekly', size: 168 * NANOS_PER_HOUR, max_equivalent_micros: 50_000_000 }] }, breaker },
      max_concurrent_sessions: 3,
    });
  });

  it('clears billing, lifts the session cap and unmeters the account when the form is emptied', () => {
    const built = accountUpdateRequest(
      'acme',
      account({ billing: { mode: 'api' }, max_concurrent_sessions: 4 }),
      { displayName: 'claude3', billing: '', monthlyFee: '', maxSessions: '', spendRows: [] },
    );
    if (!('request' in built)) throw new Error(built.error);
    expect(built.request.billing).toEqual({});
    expect(built.request.max_concurrent_sessions).toBe(0);
    expect(built.request.spend).toEqual({ quota: { windows: [] } });
  });

  it.each([
    ['a blank name', { displayName: '  ' }, 'display name'],
    ['a fee without a subscription', { billing: 'api' as const, monthlyFee: '10' }, 'Only a subscription'],
    ['a negative fee', { billing: 'subscription' as const, monthlyFee: '-1' }, 'non-negative'],
    ['a fractional session cap', { maxSessions: '1.5' }, 'whole number'],
    ['a negative session cap', { maxSessions: '-2' }, 'whole number'],
    ['a window that caps nothing', { spendRows: [{ name: 'daily', hours: 24, tokens: 0, usd: 0 }] }, 'caps nothing'],
  ])('refuses %s', (_label, change, message) => {
    const built = accountUpdateRequest('acme', account(), {
      displayName: 'claude3',
      billing: '',
      monthlyFee: '',
      maxSessions: '',
      spendRows: [],
      ...change,
    });
    expect('error' in built && built.error).toContain(message);
  });
});

describe('accountSettingsSummary', () => {
  it('summarises billing, the session cap and the limits, naming what is not set', () => {
    expect(accountSettingsSummary(account())).toEqual(['Billing not configured', 'no session cap', 'no spend limit']);
    expect(
      accountSettingsSummary(
        account({
          billing: { mode: 'subscription', monthly_fee_micros: 200_000_000 },
          max_concurrent_sessions: 1,
          spend: { quota: { windows: [{ name: 'a', size: 1 }, { name: 'b', size: 1 }] } },
        }),
      ),
    ).toEqual(['Subscription · $200.00/mo', 'max 1 session', '2 spend limits']);
    expect(accountSettingsSummary(account({ billing: { mode: 'subscription' } }))[0]).toBe('Subscription · fee not set');
    expect(accountSettingsSummary(account({ billing: { mode: 'api' } }))[0]).toBe('Metered API');
  });
});
