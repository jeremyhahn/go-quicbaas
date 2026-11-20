// toSessionInfo normalises a session record from either transport — the REST
// daemon (`state`, `last_activity`) or the Wails bridge (`status`,
// `last_active`, go-quicbaas#856) — into the console's SessionInfo.
import { describe, it, expect } from 'vitest';
import { toSessionInfo } from '../src/lib/sessions.ts';

describe('toSessionInfo', () => {
  it('reads a REST-shaped record', () => {
    expect(
      toSessionInfo({
        session_id: 's-1',
        project_id: 'p-1',
        backend: 'claude',
        model: 'opus',
        state: 'running',
        created_at: 100,
        last_activity: 200,
        job_id: 'j-1',
      }),
    ).toEqual({
      session_id: 's-1',
      project_id: 'p-1',
      backend: 'claude',
      model: 'opus',
      state: 'running',
      created_at: 100,
      last_activity: 200,
      job_id: 'j-1',
    });
  });

  it('reads a Wails-shaped record, with project_id empty when absent', () => {
    expect(
      toSessionInfo({
        session_id: 's-2',
        backend: 'codex',
        status: 'idle',
        container_id: 'c-1',
        created_at: 10,
        last_active: 20,
      }),
    ).toEqual({
      session_id: 's-2',
      project_id: '',
      backend: 'codex',
      state: 'idle',
      created_at: 10,
      last_activity: 20,
    });
  });

  it('prefers the REST spelling when a record carries both', () => {
    const info = toSessionInfo({
      session_id: 's-3',
      project_id: 'p-3',
      backend: 'claude',
      state: 'running',
      status: 'stopped',
      created_at: 1,
      last_activity: 5,
      last_active: 3,
    });
    expect(info.state).toBe('running');
    expect(info.last_activity).toBe(5);
  });
});
