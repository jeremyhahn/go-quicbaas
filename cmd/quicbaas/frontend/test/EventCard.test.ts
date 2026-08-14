/**
 * EventCard.test.ts
 *
 * Verifies that EventCard dispatches each session event kind to the correct
 * sub-component and that edge-case rendering (truncation, diff display) works.
 */

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import EventCard from '../src/components/orchestrator/EventCard.svelte';
import type { ParsedSessionEvent } from '../src/stores/sessionStore.svelte.ts';

function makeEvent(overrides: Partial<ParsedSessionEvent>): ParsedSessionEvent {
  return {
    sequence: 1,
    timestamp: '2025-01-01T00:00:00Z',
    kind: 'system',
    raw: {},
    ...overrides,
  };
}

describe('EventCard', () => {
  // ---------------------------------------------------------------------------
  // prompt
  // ---------------------------------------------------------------------------
  it('renders prompt text in a right-aligned bubble', () => {
    const event = makeEvent({ kind: 'prompt', prompt: 'Hello world' });
    render(EventCard, { props: { event } });
    expect(screen.getByText('Hello world')).toBeInTheDocument();
  });

  it('renders empty prompt without crashing', () => {
    const event = makeEvent({ kind: 'prompt', prompt: '' });
    const { container } = render(EventCard, { props: { event } });
    // chat-end bubble should still appear.
    expect(container.querySelector('.chat-end')).toBeTruthy();
  });

  // ---------------------------------------------------------------------------
  // assistant_chunk
  // ---------------------------------------------------------------------------
  it('renders assistant chunk text in a left-aligned bubble', () => {
    const event = makeEvent({ kind: 'assistant_chunk', assistantChunk: 'Hi there' });
    render(EventCard, { props: { event } });
    expect(screen.getByText('Hi there')).toBeInTheDocument();
  });

  it('shows streaming cursor when isStreaming=true', () => {
    const event = makeEvent({ kind: 'assistant_chunk', assistantChunk: 'typing' });
    const { container } = render(EventCard, { props: { event, isStreaming: true } });
    // The cursor span uses animate-pulse.
    expect(container.querySelector('.animate-pulse')).toBeTruthy();
  });

  it('does not show streaming cursor when isStreaming=false', () => {
    const event = makeEvent({ kind: 'assistant_chunk', assistantChunk: 'done' });
    const { container } = render(EventCard, { props: { event, isStreaming: false } });
    expect(container.querySelector('.animate-pulse')).toBeNull();
  });

  // ---------------------------------------------------------------------------
  // thought
  // ---------------------------------------------------------------------------
  it('renders a collapsed "Thinking…" summary for a thought event', () => {
    const event = makeEvent({ kind: 'thought', thoughtText: 'weighing the options', source: 'agent:claude' });
    const { container } = render(EventCard, { props: { event } });
    expect(screen.getByText(/Thinking/i)).toBeInTheDocument();
    const details = container.querySelector('details');
    expect(details).toBeTruthy();
    expect(details!.open).toBe(false);
  });

  it('does not show the thought text until expanded', () => {
    const event = makeEvent({ kind: 'thought', thoughtText: 'weighing the options' });
    const { container } = render(EventCard, { props: { event } });
    const details = container.querySelector('details') as HTMLDetailsElement;
    expect(details.open).toBe(false);
  });

  it('expands to show the full thought text on click', async () => {
    const event = makeEvent({ kind: 'thought', thoughtText: 'weighing the options' });
    const { container } = render(EventCard, { props: { event } });
    const summary = screen.getByLabelText('Toggle agent thinking');
    await fireEvent.click(summary);
    const details = container.querySelector('details') as HTMLDetailsElement;
    expect(details.open).toBe(true);
    expect(screen.getByText('weighing the options')).toBeInTheDocument();
  });

  // ---------------------------------------------------------------------------
  // tool_invocation
  // ---------------------------------------------------------------------------
  it('renders tool name in tool invocation summary', () => {
    const event = makeEvent({
      kind: 'tool_invocation',
      toolName: 'ReadFile',
      toolArgs: { path: 'pkg/foo.go' },
      invocationId: 'inv-1',
    });
    render(EventCard, { props: { event } });
    expect(screen.getByText('ReadFile')).toBeInTheDocument();
  });

  it('renders collapsible details element for tool invocation', () => {
    const event = makeEvent({
      kind: 'tool_invocation',
      toolName: 'Bash',
      toolArgs: { cmd: 'ls -la' },
    });
    const { container } = render(EventCard, { props: { event } });
    expect(container.querySelector('details')).toBeTruthy();
  });

  // ---------------------------------------------------------------------------
  // tool_result
  // ---------------------------------------------------------------------------
  it('renders short tool result without truncation button', () => {
    const event = makeEvent({ kind: 'tool_result', toolResult: 'ok' });
    render(EventCard, { props: { event } });
    expect(screen.getByText('ok')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /more chars/i })).toBeNull();
  });

  it('truncates tool result longer than 512 chars and shows expand button', () => {
    const longText = 'x'.repeat(600);
    const event = makeEvent({ kind: 'tool_result', toolResult: longText });
    render(EventCard, { props: { event } });
    // Expand button should appear.
    expect(screen.getByRole('button', { name: /more chars/i })).toBeInTheDocument();
  });

  // ---------------------------------------------------------------------------
  // file_op
  // ---------------------------------------------------------------------------
  it('renders file path for read op', () => {
    const event = makeEvent({
      kind: 'file_op',
      fileOp: 'read',
      filePath: 'pkg/server.go',
    });
    render(EventCard, { props: { event } });
    expect(screen.getByText('pkg/server.go')).toBeInTheDocument();
  });

  it('renders inline diff for edit op', () => {
    const event = makeEvent({
      kind: 'file_op',
      fileOp: 'edit',
      filePath: 'main.go',
      fileDiff: '+added line\n-removed line',
    });
    const { container } = render(EventCard, { props: { event } });
    // The diff renderer should produce styled divs.
    expect(container.querySelector('[aria-label="File diff"]')).toBeTruthy();
  });

  // ---------------------------------------------------------------------------
  // system
  // ---------------------------------------------------------------------------
  it('renders system message text', () => {
    const event = makeEvent({ kind: 'system', systemMessage: 'Build complete', systemLevel: 'info' });
    render(EventCard, { props: { event } });
    expect(screen.getByText('Build complete')).toBeInTheDocument();
  });

  it('renders error-level system message with alert-error class', () => {
    const event = makeEvent({ kind: 'system', systemMessage: 'Fatal error', systemLevel: 'error' });
    const { container } = render(EventCard, { props: { event } });
    expect(container.querySelector('.alert-error')).toBeTruthy();
  });

  // ---------------------------------------------------------------------------
  // operator_joined / operator_left
  // ---------------------------------------------------------------------------
  it('renders operator_joined event', () => {
    const event = makeEvent({ kind: 'operator_joined', operatorId: 'alice' });
    render(EventCard, { props: { event } });
    expect(screen.getByText(/alice/)).toBeInTheDocument();
    expect(screen.getByText(/joined/)).toBeInTheDocument();
  });

  it('renders operator_left event', () => {
    const event = makeEvent({ kind: 'operator_left', operatorId: 'bob' });
    render(EventCard, { props: { event } });
    expect(screen.getByText(/bob/)).toBeInTheDocument();
    expect(screen.getByText(/left/)).toBeInTheDocument();
  });

  // ---------------------------------------------------------------------------
  // closed
  // ---------------------------------------------------------------------------
  it('renders session closed banner', () => {
    const event = makeEvent({ kind: 'closed', closeReason: 'user request' });
    render(EventCard, { props: { event } });
    expect(screen.getByText(/session ended/i)).toBeInTheDocument();
    expect(screen.getByText('user request')).toBeInTheDocument();
  });

  // ---------------------------------------------------------------------------
  // resumed
  // ---------------------------------------------------------------------------
  it('renders session resumed chip', () => {
    const event = makeEvent({ kind: 'resumed' });
    render(EventCard, { props: { event } });
    expect(screen.getByText(/resumed/i)).toBeInTheDocument();
  });

  // ---------------------------------------------------------------------------
  // turn_started / turn_completed / turn_failed
  // ---------------------------------------------------------------------------
  it('renders an unobtrusive marker for turn_started', () => {
    const event = makeEvent({ kind: 'turn_started', turnId: 't1', turnBackend: 'ollama', turnModel: 'llama3' });
    const { container } = render(EventCard, { props: { event } });
    expect(container.querySelector('[data-testid="turn-started-marker"]')).toBeTruthy();
    expect(screen.getByText('llama3')).toBeInTheDocument();
  });

  it('renders an unobtrusive marker with duration for turn_completed', () => {
    const event = makeEvent({
      kind: 'turn_completed',
      turnId: 't1',
      turnStopReason: 'end_turn',
      turnDurationMs: 4200,
    });
    const { container } = render(EventCard, { props: { event } });
    expect(container.querySelector('[data-testid="turn-completed-marker"]')).toBeTruthy();
    expect(screen.getByText('4.2s')).toBeInTheDocument();
  });

  it('renders turn_failed visibly with the agent error, not silently', () => {
    const event = makeEvent({ kind: 'turn_failed', turnId: 't1', turnError: 'backend crashed' });
    const { container } = render(EventCard, { props: { event } });
    expect(container.querySelector('.alert-error')).toBeTruthy();
    expect(screen.getByText('backend crashed')).toBeInTheDocument();
  });

  // ---------------------------------------------------------------------------
  // unknown kind
  // ---------------------------------------------------------------------------
  it('renders unknown kind in a collapsible raw JSON block', () => {
    // Cast to bypass TS type system for the unknown kind test.
    const event = makeEvent({ kind: 'unknown_kind' as never, raw: { foo: 'bar' } });
    const { container } = render(EventCard, { props: { event } });
    expect(container.querySelector('details')).toBeTruthy();
    expect(screen.getByText(/Unknown event/i)).toBeInTheDocument();
  });
});
