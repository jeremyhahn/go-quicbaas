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
 * Slash-command surface for the session composer (PromptComposer.svelte).
 *
 * Every command here is backed by a real, already-wired capability — no
 * stubs. Commands never mutate the durable session transcript (the KV-backed
 * SessionEventLog): they either read state the client already has (session
 * status/model), act through the same SDK verb the toolbar buttons already
 * use (close), or affect purely local rendering (clear). Output is rendered
 * as a transient, client-side-only message in the composer — it is never
 * appended to the server-side event log, so it never desyncs other
 * attachers' view of the transcript.
 *
 * This module is intentionally framework-free (no Svelte imports) so its
 * parsing/dispatch logic is unit-testable without mounting a component.
 */

/** SessionSnapshot is the minimal session state a slash command may read. */
export interface SessionSnapshot {
  /** Current lifecycle status ("active", "closed", etc.). */
  status: string;
  /** Backend name ("claude", "codex", "opencode", "ollama"). */
  backend: string;
  /** Model override for the session, if one was set at spawn time. */
  model?: string;
  /** Tenant the session belongs to. */
  tenantId?: string;
}

/** SlashCommandContext supplies the session state and side-effecting hooks a command may need. */
export interface SlashCommandContext {
  /** sessionId of the session the composer is attached to. */
  sessionId: string;
  /** Snapshot of the session's current known state. */
  session: SessionSnapshot;
  /** Number of transcript events currently loaded on the client. */
  eventCount: number;
  /** Requests the session be closed via the SDK. Rejects on failure. */
  closeSession: () => Promise<void>;
  /** Clears the locally-rendered transcript view (server history is untouched). */
  clearTranscript: () => void;
}

/** SlashCommandResult is the outcome of dispatching one composer submission. */
export interface SlashCommandResult {
  /** True when the input was recognized as a slash command (started with "/"). */
  handled: boolean;
  /** Human-readable text to render in the composer as a transient message. */
  output?: string;
  /** True when output describes a failure or an unknown command. */
  isError?: boolean;
}

/** ParsedSlashCommand is a slash command split into its name and argument string. */
export interface ParsedSlashCommand {
  /** Command name, lowercased, without the leading "/". */
  name: string;
  /** Everything after the first space, trimmed. Empty when no arguments were given. */
  args: string;
}

/** SLASH_COMMANDS is the canonical, ordered list of supported command names (without "/"). */
export const SLASH_COMMANDS = ['help', 'status', 'model', 'clear', 'close'] as const;

/** SlashCommandName is the union of every supported command name. */
export type SlashCommandName = (typeof SLASH_COMMANDS)[number];

/** SLASH_COMMAND_DESCRIPTIONS documents each command for /help and the unknown-command error. */
export const SLASH_COMMAND_DESCRIPTIONS: Record<SlashCommandName, string> = {
  help: 'List available slash commands.',
  status: 'Show this session\'s status, backend, and loaded event count.',
  model: 'Show the model configured for this session.',
  clear: 'Clear the local transcript view (server history is not affected).',
  close: 'Close this session.',
};

/**
 * parseSlashCommand splits raw composer input into a command name + argument
 * string, or returns null when the input is not a slash command (does not
 * start with "/", or is bare "/" with nothing after it).
 */
export function parseSlashCommand(input: string): ParsedSlashCommand | null {
  const trimmed = input.trim();
  if (!trimmed.startsWith('/') || trimmed.length < 2) return null;
  const withoutSlash = trimmed.slice(1);
  const spaceIdx = withoutSlash.indexOf(' ');
  if (spaceIdx === -1) {
    return { name: withoutSlash.toLowerCase(), args: '' };
  }
  return {
    name: withoutSlash.slice(0, spaceIdx).toLowerCase(),
    args: withoutSlash.slice(spaceIdx + 1).trim(),
  };
}

/** isSlashCommand reports whether raw composer input parses as a slash command. */
export function isSlashCommand(input: string): boolean {
  return parseSlashCommand(input) !== null;
}

/** helpText renders the canonical list of supported commands, one per line. */
export function helpText(): string {
  const lines = SLASH_COMMANDS.map((name) => `/${name} — ${SLASH_COMMAND_DESCRIPTIONS[name]}`);
  return ['Available commands:', ...lines].join('\n');
}

/** unknownCommandText renders the error shown for an unrecognized command name. */
export function unknownCommandText(name: string): string {
  return `Unknown command: /${name}. Valid commands: ${SLASH_COMMANDS.map((c) => '/' + c).join(', ')}`;
}

/**
 * dispatchSlashCommand parses and executes a composer submission. Returns
 * `{ handled: false }` when the input is not a slash command at all, so the
 * caller falls through to sending it as a normal prompt. A recognized-but-
 * invalid command name still reports `handled: true` with `isError: true`,
 * matching the requirement that an unknown command produce a clear error
 * rather than silently being sent to the agent as a prompt.
 */
export async function dispatchSlashCommand(
  input: string,
  ctx: SlashCommandContext,
): Promise<SlashCommandResult> {
  const parsed = parseSlashCommand(input);
  if (parsed === null) return { handled: false };

  switch (parsed.name as SlashCommandName) {
    case 'help':
      return { handled: true, output: helpText() };

    case 'status': {
      const lines = [
        `Session: ${ctx.sessionId}`,
        `Status: ${ctx.session.status}`,
        `Backend: ${ctx.session.backend}`,
        ...(ctx.session.model ? [`Model: ${ctx.session.model}`] : []),
        ...(ctx.session.tenantId ? [`Tenant: ${ctx.session.tenantId}`] : []),
        `Events loaded: ${ctx.eventCount}`,
      ];
      return { handled: true, output: lines.join('\n') };
    }

    case 'model':
      return {
        handled: true,
        output: ctx.session.model
          ? `Model: ${ctx.session.model}`
          : `No model override for this session; using the ${ctx.session.backend} backend's default.`,
      };

    case 'clear':
      ctx.clearTranscript();
      return { handled: true, output: 'Transcript view cleared. Server-side history is unaffected.' };

    case 'close':
      try {
        await ctx.closeSession();
        return { handled: true, output: 'Session close requested.' };
      } catch (err: unknown) {
        return {
          handled: true,
          isError: true,
          output: `Failed to close session: ${err instanceof Error ? err.message : String(err)}`,
        };
      }

    default:
      return { handled: true, isError: true, output: unknownCommandText(parsed.name) };
  }
}
