// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for Wails binding rejections.
//
// A Wails binding rejects with the payload the Go ErrorFormatter builds
// (formatGUIError in cmd/quicbaas/errors_gui.go). The SDK must turn it into
// the APIError the REST client throws, so a denial on the desktop transport
// classifies exactly like a denial over REST (#236).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { APIError } from '../src/errors.js';
import { WailsClient, wailsError } from '../src/wails.js';

function installWindow(listJobs: (...args: unknown[]) => Promise<unknown>): void {
  Object.defineProperty(globalThis, 'window', {
    value: { go: { main: { OrchestratorBinding: { ListJobs: listJobs } } } },
    writable: true,
    configurable: true,
  });
}

describe('wailsError', () => {
  it('turns a formatter payload into an APIError carrying its status', () => {
    const err = wailsError({ code: 'FORBIDDEN', status: 403, message: 'permission denied' });
    expect(err).toBeInstanceOf(APIError);
    expect((err as APIError).status).toBe(403);
    expect((err as APIError).message).toBe('permission denied');
  });

  it('passes a plain string rejection through unchanged', () => {
    expect(wailsError('GUI transport error: broken pipe')).toBe('GUI transport error: broken pipe');
  });

  it('passes an object without a numeric status through unchanged', () => {
    const reason = { status: '403', message: 'not a payload' };
    expect(wailsError(reason)).toBe(reason);
  });
});

describe('WailsClient binding rejections', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    Object.defineProperty(globalThis, 'window', { value: undefined, writable: true, configurable: true });
  });

  it('rejects a denied binding call with an APIError of status 403', async () => {
    installWindow(vi.fn().mockRejectedValue({ code: 'FORBIDDEN', status: 403, message: 'denied' }));
    const client = new WailsClient();

    const err = await client.orchestrator.listJobs('acme').catch((e: unknown) => e);

    expect(err).toBeInstanceOf(APIError);
    expect((err as APIError).status).toBe(403);
  });

  it('resolves a successful binding call unchanged', async () => {
    const listJobs = vi.fn().mockResolvedValue([]);
    installWindow(listJobs);
    const client = new WailsClient();

    await expect(client.orchestrator.listJobs('acme')).resolves.toEqual([]);
    expect(listJobs).toHaveBeenCalledWith({ tenant_id: 'acme', task_id: undefined });
  });
});
