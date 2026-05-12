/**
 * inferenceHistoryStore.test.ts
 *
 * Validates the add / clear / cap-at-10 behaviour of the in-session
 * inference history store.
 */

import { describe, it, expect, beforeEach } from 'vitest';

// Import after Svelte rune environment is available (setup.ts handles this).
import {
  inferenceHistoryStore,
  addInferenceHistory,
  clearInferenceHistory,
} from '../src/stores/inferenceHistoryStore.svelte.ts';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeEntry(n: number) {
  return {
    timestamp: new Date(2025, 0, n + 1).toISOString(),
    backend: 'ollama',
    model: 'qwen3:1.7b',
    hostAffinity: '',
    prompt: `Prompt ${n}`,
    response: `Response ${n}`,
    stopReason: 'end_turn',
    elapsedMs: 100 * n,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('inferenceHistoryStore', () => {
  beforeEach(() => {
    clearInferenceHistory();
  });

  it('starts empty after clear', () => {
    expect(inferenceHistoryStore.entries).toHaveLength(0);
  });

  it('add inserts a new entry with a unique id', () => {
    addInferenceHistory(makeEntry(1));
    expect(inferenceHistoryStore.entries).toHaveLength(1);
    const entry = inferenceHistoryStore.entries[0];
    expect(entry).toBeDefined();
    expect(entry!.prompt).toBe('Prompt 1');
    expect(typeof entry!.id).toBe('number');
  });

  it('add assigns monotonically increasing ids', () => {
    addInferenceHistory(makeEntry(1));
    addInferenceHistory(makeEntry(2));
    const ids = inferenceHistoryStore.entries.map((e) => e.id);
    expect(ids[0]).toBeLessThan(ids[1]!);
  });

  it('clear removes all entries', () => {
    addInferenceHistory(makeEntry(1));
    addInferenceHistory(makeEntry(2));
    clearInferenceHistory();
    expect(inferenceHistoryStore.entries).toHaveLength(0);
  });

  it('caps at 10 entries (oldest evicted)', () => {
    // Add 12 entries — only the last 10 should be retained.
    for (let i = 0; i < 12; i++) {
      addInferenceHistory(makeEntry(i));
    }
    expect(inferenceHistoryStore.entries).toHaveLength(10);
    // The oldest two (0, 1) should be gone; the newest (11) should be present.
    const prompts = inferenceHistoryStore.entries.map((e) => e.prompt);
    expect(prompts).not.toContain('Prompt 0');
    expect(prompts).not.toContain('Prompt 1');
    expect(prompts).toContain('Prompt 11');
  });

  it('retains all fields verbatim', () => {
    const input = makeEntry(5);
    addInferenceHistory(input);
    const stored = inferenceHistoryStore.entries[0]!;
    expect(stored.backend).toBe(input.backend);
    expect(stored.model).toBe(input.model);
    expect(stored.prompt).toBe(input.prompt);
    expect(stored.response).toBe(input.response);
    expect(stored.stopReason).toBe(input.stopReason);
    expect(stored.elapsedMs).toBe(input.elapsedMs);
  });

  it('clear after cap leaves store empty', () => {
    for (let i = 0; i < 15; i++) {
      addInferenceHistory(makeEntry(i));
    }
    clearInferenceHistory();
    expect(inferenceHistoryStore.entries).toHaveLength(0);
  });
});
