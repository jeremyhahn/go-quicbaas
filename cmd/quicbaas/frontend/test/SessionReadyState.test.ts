/**
 * SessionReadyState.test.ts
 *
 * Verifies the "new chat" ready-state placeholder shown by EventStream when
 * a session has no transcript events: it names the backend + model, states
 * that the agent is ready and waiting for a prompt, and hints at /help.
 */

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import SessionReadyState from '../src/components/orchestrator/SessionReadyState.svelte';

describe('SessionReadyState', () => {
  it('names the backend and model', () => {
    render(SessionReadyState, { props: { backend: 'claude', model: 'claude-3-5-sonnet' } });
    expect(screen.getByText('claude')).toBeInTheDocument();
    expect(screen.getByText('claude-3-5-sonnet')).toBeInTheDocument();
  });

  it('states the agent is ready and waiting for a prompt', () => {
    render(SessionReadyState, { props: { backend: 'claude' } });
    expect(screen.getByText(/ready and waiting for your first prompt/i)).toBeInTheDocument();
  });

  it('hints at /help for slash commands', () => {
    render(SessionReadyState, { props: { backend: 'claude' } });
    expect(screen.getByText('/help')).toBeInTheDocument();
  });

  it('omits the model chip when no model override is set', () => {
    render(SessionReadyState, { props: { backend: 'codex' } });
    expect(screen.getByText('codex')).toBeInTheDocument();
    expect(screen.queryByText(/claude-3-5-sonnet/)).toBeNull();
  });
});
