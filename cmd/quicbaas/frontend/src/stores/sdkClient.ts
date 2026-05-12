// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

/**
 * SDK client singleton store.
 *
 * Exposes a lazily-initialised Client instance. Transport is auto-detected:
 * Wails desktop → 'wails'; browser → 'rest' with baseURL from window.location.
 *
 * All components share this single instance so subscriptions and state
 * mutations don't conflict with each other.
 */

import { createClient, isWailsRuntime } from '@quicbaas/sdk';
import type { Client } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Module-level singleton
// ---------------------------------------------------------------------------

let _client: Client | null = null;
let _initPromise: Promise<Client> | null = null;

/**
 * getSDKClient returns the singleton Client, initialising it on first call.
 * Subsequent calls return the cached instance without re-initialising.
 */
export async function getSDKClient(): Promise<Client> {
  if (_client !== null) return _client;

  if (_initPromise !== null) return _initPromise;

  _initPromise = (async (): Promise<Client> => {
    if (isWailsRuntime()) {
      _client = await createClient({ transport: 'wails' });
    } else {
      const baseURL =
        typeof window !== 'undefined' ? window.location.origin : '';
      _client = await createClient({ transport: 'rest', baseURL });
    }
    return _client;
  })();

  return _initPromise;
}

/**
 * resetSDKClient tears down the singleton. Used in tests to ensure a fresh
 * client per test case. Not intended for production use.
 */
export function resetSDKClient(): void {
  _client = null;
  _initPromise = null;
}
