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
 * reloadApp reloads the whole UI. A daemon switch calls it, so no store,
 * cached record, in-flight answer or live stream from the previous daemon
 * survives the switch. It is its own module so tests can mock it (jsdom has
 * no reload).
 */
export function reloadApp(): void {
  window.location.reload();
}
