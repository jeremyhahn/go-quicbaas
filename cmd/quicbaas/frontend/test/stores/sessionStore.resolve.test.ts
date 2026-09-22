/**
 * stores/sessionStore.resolve.test.ts
 *
 * resolveSession answers "which session is this route about?" now that the
 * session list is a PAGE.
 *
 * The Sessions table pages through the tenant's sessions, so a row opened from
 * page three names a record the store has never read. Looking only in the
 * cache answered "Session not found" for a session that plainly exists; the
 * listing's search matches the session id, which makes the miss one request
 * rather than a walk through every page.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { SessionStatusResponse } from '@quicbaas/sdk';

const { listSessionsPageMock, listSessionsMock } = vi.hoisted(() => ({
  listSessionsPageMock: vi.fn(),
  listSessionsMock: vi.fn(),
}));

vi.mock('../../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      listSessions: listSessionsMock,
      listSessionsPage: listSessionsPageMock,
      getSessionHistory: vi.fn().mockResolvedValue({ events: [], next_seq: 0 }),
      subscribeSession: vi.fn(),
    },
  }),
}));

import { resolveSession, refreshSessions, sessionStore } from '../../src/stores/sessionStore.svelte.ts';

function makeSession(id: string, overrides: Partial<SessionStatusResponse> = {}): SessionStatusResponse {
  return {
    session_id: id,
    backend: 'claude',
    status: 'active',
    created_at: 1700000000,
    last_active: 1700000000,
    ...overrides,
  } as SessionStatusResponse;
}

describe('resolveSession', () => {
  beforeEach(() => {
    listSessionsPageMock.mockReset();
    listSessionsMock.mockReset();
    listSessionsMock.mockResolvedValue({ sessions: [] });
  });

  it('answers from the list already held, without asking the server', async () => {
    listSessionsMock.mockResolvedValue({ sessions: [makeSession('sess-held')] });
    await refreshSessions();
    expect(sessionStore.sessions).toHaveLength(1);

    const found = await resolveSession('sess-held');

    expect(found?.session_id).toBe('sess-held');
    expect(listSessionsPageMock).not.toHaveBeenCalled();
  });

  it('asks the server for a session the held page does not carry', async () => {
    listSessionsPageMock.mockResolvedValue({
      items: [makeSession('sess-on-page-three', { runner: 'review' })],
      total: 1,
      limit: 1,
      offset: 0,
    });

    const found = await resolveSession('sess-on-page-three');

    expect(found?.session_id).toBe('sess-on-page-three');
    // The role travels with the record, so the detail page can name it.
    expect(found?.runner).toBe('review');
    // ONE request, narrowed by the id — not a walk through every page.
    expect(listSessionsPageMock).toHaveBeenCalledTimes(1);
    expect(listSessionsPageMock.mock.calls[0]?.[1]).toMatchObject({ q: 'sess-on-page-three' });
  });

  it('claims nothing when the search matches only a different session', async () => {
    // `q` is a SUBSTRING match, so an answer must be confirmed against the id
    // that was asked for rather than trusted to be the only thing that matched.
    listSessionsPageMock.mockResolvedValue({
      items: [makeSession('sess-abc-longer')],
      total: 1,
      limit: 1,
      offset: 0,
    });

    expect(await resolveSession('sess-abc')).toBeNull();
  });

  it('answers null for an empty id and for a read that fails', async () => {
    expect(await resolveSession('')).toBeNull();
    expect(listSessionsPageMock).not.toHaveBeenCalled();

    listSessionsPageMock.mockRejectedValue(new Error('forbidden'));
    expect(await resolveSession('sess-unreadable')).toBeNull();
  });
});
