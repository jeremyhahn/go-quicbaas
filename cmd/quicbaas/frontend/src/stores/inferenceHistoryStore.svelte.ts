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
 * inferenceHistoryStore — in-session history of past inference calls.
 *
 * Holds the last MAX_HISTORY entries in memory. Survives view re-mounts
 * within the same browser session because the store is module-level state.
 * Cleared on explicit clear() or page reload.
 */

// Maximum number of history entries retained.
const MAX_HISTORY = 10;

// InferenceHistoryEntry records a completed inference call.
export interface InferenceHistoryEntry {
  /** Unique entry ID (monotonic counter, not persisted). */
  readonly id: number;
  /** ISO-8601 timestamp when the call was initiated. */
  readonly timestamp: string;
  /** Backend used (e.g., "ollama"). */
  readonly backend: string;
  /** Model name used. */
  readonly model: string;
  /** Host affinity used, or empty string for "Any". */
  readonly hostAffinity: string;
  /** The user prompt. */
  readonly prompt: string;
  /** The accumulated response text. */
  readonly response: string;
  /** Stop reason reported by the model, if available. */
  readonly stopReason: string;
  /** Wall-clock elapsed time in milliseconds. */
  readonly elapsedMs: number;
}

let nextID = 0;

// entries is the reactive array of history records, newest-last.
let entries: InferenceHistoryEntry[] = $state([]);

// add inserts a new entry and trims the list to MAX_HISTORY from the tail.
export function addInferenceHistory(entry: Omit<InferenceHistoryEntry, 'id'>): void {
  const record: InferenceHistoryEntry = { id: nextID++, ...entry };
  const updated = [...entries, record];
  // Keep only the most recent MAX_HISTORY items.
  entries = updated.length > MAX_HISTORY ? updated.slice(updated.length - MAX_HISTORY) : updated;
}

// clear removes all history entries.
export function clearInferenceHistory(): void {
  entries = [];
}

// inferenceHistoryStore exposes the reactive state and mutation functions.
export const inferenceHistoryStore = {
  get entries(): InferenceHistoryEntry[] {
    return entries;
  },
};
