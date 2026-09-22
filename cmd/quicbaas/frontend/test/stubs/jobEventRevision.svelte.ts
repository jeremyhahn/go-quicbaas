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
 * A REACTIVE stand-in for orchestratorStore.jobEventRevision.
 *
 * A plain property on a mock object is not reactive, so a view that re-reads
 * its page when the job event stream moves would never be seen to do it: the
 * effect watching the counter simply never re-runs. Runes only work in
 * `.svelte.ts` modules, which is why this one exists beside the test rather
 * than inside it.
 */

let revision = $state(0);

/** jobEventRevision is the counter a mocked store getter returns. */
export function jobEventRevision(): number {
  return revision;
}

/** bumpJobEventRevision is one job event that changed a job's record. */
export function bumpJobEventRevision(): void {
  revision += 1;
}

/** resetJobEventRevision returns the counter to its initial value. */
export function resetJobEventRevision(): void {
  revision = 0;
}
