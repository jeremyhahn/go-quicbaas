/**
 * tenantSwitch.svelte.ts
 *
 * Shared fixtures for the tests that switch the active tenant while a view's
 * load is still in flight. The `.svelte.ts` extension makes the store a real
 * rune-backed object, so an effect that reads it synchronously depends on it
 * exactly as it does on the production orchestrator store.
 */

import { tick } from 'svelte';

/** reactiveTenantStore stands in for orchestratorStore: only the tenant is read. */
export const reactiveTenantStore = $state({ activeTenantID: 'acme' });

/** Gate is a promise the test settles by hand. */
export interface Gate<T> {
  promise: Promise<T>;
  open: (value: T) => void;
}

/** gate returns a promise that stays pending until `open` is called. */
export function gate<T>(): Gate<T> {
  let open!: (value: T) => void;
  const promise = new Promise<T>((resolve) => {
    open = resolve;
  });
  return { promise, open };
}

/** settle drains resolved reads and Svelte's render queue. */
export async function settle(): Promise<void> {
  for (let i = 0; i < 5; i++) await Promise.resolve();
  await tick();
}
