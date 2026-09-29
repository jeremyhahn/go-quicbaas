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
 * The one map from a RefKind to its RefProvider. `providers.ts` populates it
 * once, at import time; `RefPicker` and `RefChip` only ever read it, which
 * keeps them ignorant of what kinds exist — a new kind is registered, never
 * wired into the components themselves.
 */

import type { RefKind, RefProvider } from './types.ts';

const providers = new Map<RefKind, RefProvider>();

/** registerRefProvider adds or replaces the provider for its own kind. */
export function registerRefProvider(provider: RefProvider): void {
  providers.set(provider.kind, provider);
}

/** getRefProvider returns the registered provider for a kind, or undefined if none was registered. */
export function getRefProvider(kind: RefKind): RefProvider | undefined {
  return providers.get(kind);
}

/** refProviderKinds lists every kind with a registered provider, for tests and diagnostics. */
export function refProviderKinds(): RefKind[] {
  return [...providers.keys()];
}

/** clearRefProviders empties the registry — a test-only hook, never called from app code. */
export function clearRefProviders(): void {
  providers.clear();
}
