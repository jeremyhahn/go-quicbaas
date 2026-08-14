/**
 * SessionActivityIndicator.test.ts
 *
 * Verifies the header badge renders the correct label + aria-label for each
 * of the four TurnStatus values sessionStore can report, so an operator can
 * always tell whether the agent is working, waiting to start, idle, or the
 * live tail is unavailable.
 */

import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import type { TurnStatus } from '../src/stores/sessionStore.svelte.ts';

let mockTurnStatus: TurnStatus = 'idle';

vi.mock('../src/stores/sessionStore.svelte.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../src/stores/sessionStore.svelte.ts')>();
  return {
    ...actual,
    sessionStore: {
      get turnStatus() { return mockTurnStatus; },
    },
  };
});

import SessionActivityIndicator from '../src/components/orchestrator/SessionActivityIndicator.svelte';

describe('SessionActivityIndicator', () => {
  it('renders "Working" while the agent is streaming', () => {
    mockTurnStatus = 'working';
    render(SessionActivityIndicator);
    expect(screen.getByText('Working')).toBeInTheDocument();
    expect(screen.getByRole('status', { name: /agent is working/i })).toBeInTheDocument();
  });

  it('renders "Waiting" when a prompt was sent but nothing has come back yet', () => {
    mockTurnStatus = 'pending';
    render(SessionActivityIndicator);
    expect(screen.getByText('Waiting')).toBeInTheDocument();
    expect(screen.getByRole('status', { name: /waiting for the agent to start/i })).toBeInTheDocument();
  });

  it('renders "Idle" when awaiting the next prompt', () => {
    mockTurnStatus = 'idle';
    render(SessionActivityIndicator);
    expect(screen.getByText('Idle')).toBeInTheDocument();
    expect(screen.getByRole('status', { name: /awaiting a prompt/i })).toBeInTheDocument();
  });

  it('renders "Disconnected" when the live tail is unavailable', () => {
    mockTurnStatus = 'disconnected';
    render(SessionActivityIndicator);
    expect(screen.getByText('Disconnected')).toBeInTheDocument();
    expect(screen.getByRole('status', { name: /live updates unavailable/i })).toBeInTheDocument();
  });
});
