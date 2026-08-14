// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
 * Shared display labels for pooled provider account vocabulary. Used by both
 * AccountsView (management) and SpawnDialog (account selection) so the two
 * surfaces never drift on how a provider or refresh policy is named.
 */

import type { AccountProvider, AccountRefreshPolicy } from '@quicbaas/sdk';

/** The AccountProvider IDs with an unambiguous 1:1 backend-name mapping. */
export const KNOWN_ACCOUNT_PROVIDERS: readonly AccountProvider[] = ['claude', 'codex', 'opencode'];

/** providerLabel names both the provider ID and the vendor, e.g. "Claude (Anthropic)". */
export function providerLabel(p: AccountProvider | string): string {
  switch (p) {
    case 'claude': return 'Claude (Anthropic)';
    case 'codex': return 'Codex (OpenAI)';
    case 'opencode': return 'OpenCode';
    default: return p;
  }
}

/** refreshPolicyLabel renders an AccountRefreshPolicy for display. */
export function refreshPolicyLabel(p: AccountRefreshPolicy | string): string {
  return p === 'oauth_refresh' ? 'OAuth Refresh' : 'Static Token';
}

/**
 * isKnownAccountProvider reports whether a backend name maps unambiguously
 * onto an AccountProvider ID (backend "claude" -> provider "claude", etc).
 * Callers use this to narrow an account picker to the relevant provider
 * WITHOUT hiding accounts for backends that have no such mapping — see
 * SpawnDialog's account selector.
 */
export function isKnownAccountProvider(backend: string): backend is AccountProvider {
  return (KNOWN_ACCOUNT_PROVIDERS as readonly string[]).includes(backend);
}
