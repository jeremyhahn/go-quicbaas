/**
 * PromptComposer.test.ts
 *
 * Verifies the composer's slash-command surface end-to-end through the real
 * component: typing a "/command" and submitting dispatches it locally
 * instead of sending it to the agent, renders its output as a transient
 * banner, and an unrecognized command surfaces a clear error listing the
 * valid ones. A plain prompt still goes through promptSession unchanged.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { SLASH_COMMANDS } from '../src/lib/slashCommands.ts';

const { mocks } = vi.hoisted(() => ({
  mocks: {
    appendLocalPrompt: vi.fn(),
    removeLocalPrompt: vi.fn(),
    clearLocalTranscript: vi.fn(),
    requestCloseSession: vi.fn().mockResolvedValue(undefined),
    promptSession: vi.fn().mockResolvedValue(undefined),
  },
}));

vi.mock('../src/stores/sessionStore.svelte.ts', () => ({
  sessionStore: {
    events: [],
    turnStatus: 'idle',
    activeSession: {
      session_id: 'sess-1',
      backend: 'claude',
      status: 'active',
      model: 'claude-3-5-sonnet',
      tenant_id: 'system',
      created_at: 0,
      last_active: 0,
    },
  },
  appendLocalPrompt: mocks.appendLocalPrompt,
  removeLocalPrompt: mocks.removeLocalPrompt,
  clearLocalTranscript: mocks.clearLocalTranscript,
  requestCloseSession: mocks.requestCloseSession,
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: { promptSession: mocks.promptSession },
  }),
}));

import PromptComposer from '../src/components/orchestrator/PromptComposer.svelte';

async function typeAndSend(input: HTMLElement, button: HTMLElement, text: string): Promise<void> {
  await fireEvent.input(input, { target: { value: text } });
  await fireEvent.click(button);
}

describe('PromptComposer — slash commands', () => {
  beforeEach(() => {
    mocks.appendLocalPrompt.mockClear();
    mocks.removeLocalPrompt.mockClear();
    mocks.clearLocalTranscript.mockClear();
    mocks.requestCloseSession.mockClear();
    mocks.promptSession.mockClear();
  });

  it('/help renders the command list without calling promptSession', async () => {
    render(PromptComposer, { props: { sessionId: 'sess-1' } });
    const input = screen.getByLabelText('Prompt input');
    const button = screen.getByRole('button', { name: /send prompt/i });

    await typeAndSend(input, button, '/help');

    expect(mocks.promptSession).not.toHaveBeenCalled();
    for (const name of SLASH_COMMANDS) {
      expect(screen.getByText(new RegExp(`/${name}`))).toBeInTheDocument();
    }
  });

  it('unknown command shows a clear error listing valid commands', async () => {
    render(PromptComposer, { props: { sessionId: 'sess-1' } });
    const input = screen.getByLabelText('Prompt input');
    const button = screen.getByRole('button', { name: /send prompt/i });

    await typeAndSend(input, button, '/bogus');

    expect(mocks.promptSession).not.toHaveBeenCalled();
    const banner = await screen.findByRole('alert');
    expect(banner.textContent).toMatch(/unknown command/i);
    for (const name of SLASH_COMMANDS) {
      expect(banner.textContent).toContain(`/${name}`);
    }
  });

  it('/close calls requestCloseSession', async () => {
    render(PromptComposer, { props: { sessionId: 'sess-1' } });
    const input = screen.getByLabelText('Prompt input');
    const button = screen.getByRole('button', { name: /send prompt/i });

    await typeAndSend(input, button, '/close');

    expect(mocks.requestCloseSession).toHaveBeenCalledWith('sess-1');
  });

  it('/clear invokes clearLocalTranscript', async () => {
    render(PromptComposer, { props: { sessionId: 'sess-1' } });
    const input = screen.getByLabelText('Prompt input');
    const button = screen.getByRole('button', { name: /send prompt/i });

    await typeAndSend(input, button, '/clear');

    expect(mocks.clearLocalTranscript).toHaveBeenCalledOnce();
  });

  it('a plain prompt still goes through promptSession, not a slash command', async () => {
    render(PromptComposer, { props: { sessionId: 'sess-1' } });
    const input = screen.getByLabelText('Prompt input');
    const button = screen.getByRole('button', { name: /send prompt/i });

    await typeAndSend(input, button, 'write me a function');

    expect(mocks.promptSession).toHaveBeenCalledWith(
      expect.objectContaining({ session_id: 'sess-1', prompt: 'write me a function' }),
    );
  });
});
