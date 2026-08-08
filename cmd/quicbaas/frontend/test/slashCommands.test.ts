/**
 * slashCommands.test.ts
 *
 * Verifies parsing and dispatch of the composer's slash-command surface:
 * /help, /status, /model, /clear, /close, plain (non-slash) input, and the
 * unknown-command error path.
 */

import { describe, it, expect, vi } from 'vitest';
import {
  parseSlashCommand,
  isSlashCommand,
  dispatchSlashCommand,
  helpText,
  unknownCommandText,
  SLASH_COMMANDS,
  type SlashCommandContext,
} from '../src/lib/slashCommands.ts';

function makeContext(overrides: Partial<SlashCommandContext> = {}): SlashCommandContext {
  return {
    sessionId: 'sess-1',
    session: { status: 'active', backend: 'claude', model: 'claude-3-5-sonnet', tenantId: 'system' },
    eventCount: 3,
    closeSession: vi.fn().mockResolvedValue(undefined),
    clearTranscript: vi.fn(),
    ...overrides,
  };
}

describe('parseSlashCommand', () => {
  it('returns null for plain text', () => {
    expect(parseSlashCommand('hello world')).toBeNull();
  });

  it('returns null for a bare slash with nothing after it', () => {
    expect(parseSlashCommand('/')).toBeNull();
  });

  it('returns null for empty input', () => {
    expect(parseSlashCommand('')).toBeNull();
  });

  it('parses a command with no arguments', () => {
    expect(parseSlashCommand('/help')).toEqual({ name: 'help', args: '' });
  });

  it('parses a command with arguments, trimmed', () => {
    expect(parseSlashCommand('/model   gpt-4  ')).toEqual({ name: 'model', args: 'gpt-4' });
  });

  it('lowercases the command name', () => {
    expect(parseSlashCommand('/HELP')).toEqual({ name: 'help', args: '' });
  });

  it('tolerates leading/trailing whitespace around the whole input', () => {
    expect(parseSlashCommand('  /status  ')).toEqual({ name: 'status', args: '' });
  });
});

describe('isSlashCommand', () => {
  it('is true for slash input', () => {
    expect(isSlashCommand('/close')).toBe(true);
  });

  it('is false for a normal prompt, even one mentioning a slash mid-string', () => {
    expect(isSlashCommand('please run a/b test')).toBe(false);
  });
});

describe('dispatchSlashCommand — non-command input', () => {
  it('returns handled=false for plain prompts, letting the caller send them normally', async () => {
    const result = await dispatchSlashCommand('write me a function', makeContext());
    expect(result).toEqual({ handled: false });
  });
});

describe('dispatchSlashCommand — /help', () => {
  it('lists every supported command', async () => {
    const result = await dispatchSlashCommand('/help', makeContext());
    expect(result.handled).toBe(true);
    expect(result.isError).toBeFalsy();
    for (const name of SLASH_COMMANDS) {
      expect(result.output).toContain(`/${name}`);
    }
  });

  it('matches the standalone helpText() output', async () => {
    const result = await dispatchSlashCommand('/help', makeContext());
    expect(result.output).toBe(helpText());
  });
});

describe('dispatchSlashCommand — /status', () => {
  it('reports session id, status, backend, model, tenant, and event count', async () => {
    const result = await dispatchSlashCommand('/status', makeContext({ eventCount: 42 }));
    expect(result.handled).toBe(true);
    expect(result.output).toContain('sess-1');
    expect(result.output).toContain('active');
    expect(result.output).toContain('claude');
    expect(result.output).toContain('claude-3-5-sonnet');
    expect(result.output).toContain('system');
    expect(result.output).toContain('42');
  });

  it('omits model and tenant lines when absent', async () => {
    const ctx = makeContext({ session: { status: 'active', backend: 'ollama' } });
    const result = await dispatchSlashCommand('/status', ctx);
    expect(result.output).not.toContain('Model:');
    expect(result.output).not.toContain('Tenant:');
  });
});

describe('dispatchSlashCommand — /model', () => {
  it('shows the configured model', async () => {
    const result = await dispatchSlashCommand('/model', makeContext());
    expect(result.output).toBe('Model: claude-3-5-sonnet');
    expect(result.isError).toBeFalsy();
  });

  it('reports the backend default when no model override is set', async () => {
    const ctx = makeContext({ session: { status: 'active', backend: 'ollama' } });
    const result = await dispatchSlashCommand('/model', ctx);
    expect(result.output).toContain('ollama');
    expect(result.output).toContain('default');
  });
});

describe('dispatchSlashCommand — /clear', () => {
  it('invokes clearTranscript and confirms server history is unaffected', async () => {
    const ctx = makeContext();
    const result = await dispatchSlashCommand('/clear', ctx);
    expect(ctx.clearTranscript).toHaveBeenCalledOnce();
    expect(result.handled).toBe(true);
    expect(result.isError).toBeFalsy();
    expect(result.output).toMatch(/cleared/i);
  });
});

describe('dispatchSlashCommand — /close', () => {
  it('calls closeSession and reports success', async () => {
    const ctx = makeContext();
    const result = await dispatchSlashCommand('/close', ctx);
    expect(ctx.closeSession).toHaveBeenCalledOnce();
    expect(result.handled).toBe(true);
    expect(result.isError).toBeFalsy();
    expect(result.output).toMatch(/close/i);
  });

  it('reports an error when closeSession rejects', async () => {
    const ctx = makeContext({ closeSession: vi.fn().mockRejectedValue(new Error('network down')) });
    const result = await dispatchSlashCommand('/close', ctx);
    expect(result.handled).toBe(true);
    expect(result.isError).toBe(true);
    expect(result.output).toContain('network down');
  });
});

describe('dispatchSlashCommand — unknown command', () => {
  it('returns a clear error listing every valid command', async () => {
    const result = await dispatchSlashCommand('/nonexistent', makeContext());
    expect(result.handled).toBe(true);
    expect(result.isError).toBe(true);
    expect(result.output).toBe(unknownCommandText('nonexistent'));
    for (const name of SLASH_COMMANDS) {
      expect(result.output).toContain(`/${name}`);
    }
  });

  it('does not execute any side effect for an unknown command', async () => {
    const ctx = makeContext();
    await dispatchSlashCommand('/bogus', ctx);
    expect(ctx.closeSession).not.toHaveBeenCalled();
    expect(ctx.clearTranscript).not.toHaveBeenCalled();
  });
});
