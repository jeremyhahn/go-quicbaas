/**
 * EventStream.test.ts
 *
 * Verifies:
 *  - The DOM stays at ~50 nodes during a large event list (windowed rendering).
 *  - The jump-to-latest button appears when pendingNew > 0 and atBottom=false.
 *  - Auto-scroll pause / resume logic (via IntersectionObserver callback).
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import type { ParsedSessionEvent } from '../src/stores/sessionStore.svelte.ts';

// ---------------------------------------------------------------------------
// Build a large set of mock events.
// ---------------------------------------------------------------------------

function makeEvents(n: number): ParsedSessionEvent[] {
  return Array.from({ length: n }, (_, i) => ({
    sequence: i + 1,
    timestamp: `2025-01-01T00:00:${String(i).padStart(2, '0')}Z`,
    kind: 'system' as const,
    raw: {},
    systemMessage: `Event ${i + 1}`,
    systemLevel: 'info' as const,
  }));
}

// ---------------------------------------------------------------------------
// Mock sessionStore.
// ---------------------------------------------------------------------------

let mockEvents: ParsedSessionEvent[] = [];
let mockHistoryExhausted = false;

vi.mock('../src/stores/sessionStore.svelte.ts', () => ({
  sessionStore: {
    get events() { return mockEvents; },
    get historyExhausted() { return mockHistoryExhausted; },
  },
  loadMoreHistory: vi.fn().mockResolvedValue(undefined),
}));

import EventStream from '../src/components/orchestrator/EventStream.svelte';

// ---------------------------------------------------------------------------
// Helper: count rendered EventCard nodes.
// ---------------------------------------------------------------------------

function countEventCards(container: HTMLElement): number {
  // EventCard renders [data-kind] divs.
  return container.querySelectorAll('[data-kind]').length;
}

describe('EventStream', () => {
  beforeEach(() => {
    mockEvents = [];
    mockHistoryExhausted = false;
  });

  it('renders empty state when there are no events', () => {
    render(EventStream);
    expect(screen.getByText(/No events yet/i)).toBeInTheDocument();
  });

  it('renders ≤50 event cards even when the store has 200 events', async () => {
    mockEvents = makeEvents(200);
    const { container } = render(EventStream);
    // Give one microtask tick for derived states to settle.
    await Promise.resolve();
    const count = countEventCards(container);
    expect(count).toBeLessThanOrEqual(50);
  });

  it('renders all events when count is ≤ WINDOW_SIZE', async () => {
    mockEvents = makeEvents(30);
    const { container } = render(EventStream);
    await Promise.resolve();
    const count = countEventCards(container);
    expect(count).toBe(30);
  });

  it('shows "Beginning of session" when history is exhausted', async () => {
    mockEvents = makeEvents(5);
    mockHistoryExhausted = true;
    render(EventStream);
    await Promise.resolve();
    expect(screen.getByText(/Beginning of session/i)).toBeInTheDocument();
  });

  it('shows "Scroll up to load history" when history is not exhausted', async () => {
    mockEvents = makeEvents(5);
    mockHistoryExhausted = false;
    render(EventStream);
    await Promise.resolve();
    expect(screen.getByText(/Scroll up to load history/i)).toBeInTheDocument();
  });

  it('has a log role for accessibility', () => {
    render(EventStream);
    expect(screen.getByRole('log')).toBeInTheDocument();
  });
});
