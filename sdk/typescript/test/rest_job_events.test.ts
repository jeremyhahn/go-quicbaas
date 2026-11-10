// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Tests for RestOrchestratorClient.subscribeJobEvents.
//
// Regression coverage for the "UI login refused" investigation
// (2026-09-25): the stream used to open via `new EventSource(url)`, which
// cannot carry a custom Authorization header. Every request it sent
// therefore arrived at the daemon with NO Authorization header at all and
// was refused — indistinguishable, from the browser, from an expired or
// rejected session. subscribeJobEvents now does its own fetch-based SSE
// parsing (the same approach subscribeSession already used, see
// rest_subscribe.test.ts), so it carries the same bearer every other
// authenticated call on this client does, resolved fresh on every
// (re)connect.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest.js';
import type { JobEvent } from '../src/types.js';

/** Craft an SSE ReadableStream from a string of raw SSE bytes. */
function makeSseStream(raw: string): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  const bytes = encoder.encode(raw);
  return new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(bytes);
      controller.close();
    },
  });
}

/** An SSE stream that never closes — models a `follow`-style live tail. */
function makeOpenSseStream(): { stream: ReadableStream<Uint8Array>; push: (raw: string) => void; end: () => void } {
  let controllerRef!: ReadableStreamDefaultController<Uint8Array>;
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controllerRef = controller;
    },
  });
  return {
    stream,
    push: (raw: string) => controllerRef.enqueue(encoder.encode(raw)),
    end: () => controllerRef.close(),
  };
}

describe('RestOrchestratorClient.subscribeJobEvents', () => {
  const BASE = 'http://node';
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('opens the job event stream over fetch (not EventSource) with the correct URL', async () => {
    fetchMock.mockResolvedValueOnce({ ok: true, body: makeSseStream('') });

    const client = new RestClient(BASE);
    const onEvent = vi.fn();
    const stop = client.orchestrator.subscribeJobEvents('acme', 'job-1', onEvent);

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled());
    stop();

    const url = fetchMock.mock.calls[0][0] as string;
    expect(url).toBe('http://node/api/v1/orchestrator/jobs/job-1/events?tenant_id=acme');
  });

  it('sends the bearer token and Accept: text/event-stream on every connection attempt', async () => {
    fetchMock.mockResolvedValueOnce({ ok: true, body: makeSseStream('') });

    const client = new RestClient(BASE, { token: async () => 'live-token' });
    const stop = client.orchestrator.subscribeJobEvents('acme', 'job-1', vi.fn());

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled());
    stop();

    const init = fetchMock.mock.calls[0][1] as RequestInit;
    const headers = init.headers as Record<string, string>;
    expect(headers['Authorization']).toBe('Bearer live-token');
    expect(headers['Accept']).toBe('text/event-stream');
  });

  it('delivers each parsed event and discards unparseable frames', async () => {
    const event: JobEvent = { job_id: 'job-1', state: 'executing' } as JobEvent;
    const sseRaw = [
      `data: ${JSON.stringify(event)}`,
      '',
      'data: not json',
      '',
      '',
    ].join('\n');
    fetchMock.mockResolvedValueOnce({ ok: true, body: makeSseStream(sseRaw) });

    const client = new RestClient(BASE);
    const onEvent = vi.fn();
    const stop = client.orchestrator.subscribeJobEvents('acme', 'job-1', onEvent);

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled());
    // The malformed frame does not throw and does not stop delivery of the
    // valid one.
    await vi.waitFor(() => expect(onEvent).toHaveBeenCalledTimes(1));
    expect(onEvent.mock.calls[0][0]).toMatchObject({ job_id: 'job-1', state: 'executing' });
    stop();
  });

  it('reports a refused stream (403) once, and does not retry', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 403,
      statusText: 'Forbidden',
      json: async () => ({ error: 'forbidden' }),
    });

    const client = new RestClient(BASE);
    const onError = vi.fn();
    const stop = client.orchestrator.subscribeJobEvents('acme', 'job-1', vi.fn(), onError);

    await vi.waitFor(() => expect(onError).toHaveBeenCalledTimes(1));
    // Give any (incorrect) retry a chance to fire before asserting it didn't.
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(onError).toHaveBeenCalledTimes(1);
    stop();
  });

  it('reconnects (re-resolving the token) after a stream ends naturally, without calling onError', async () => {
    vi.useFakeTimers();
    const first = makeOpenSseStream();
    fetchMock
      .mockResolvedValueOnce({ ok: true, body: first.stream })
      .mockResolvedValueOnce({ ok: true, body: makeSseStream('') });

    const token = vi.fn().mockResolvedValue('token-a');
    const client = new RestClient(BASE, { token });
    const onError = vi.fn();
    const stop = client.orchestrator.subscribeJobEvents('acme', 'job-1', vi.fn(), onError);

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    // The server closes the stream (e.g. the job finished) — a natural end,
    // not a refusal.
    first.end();

    // Reconnect happens after the backoff delay, not immediately and not
    // never.
    await vi.advanceTimersByTimeAsync(3000);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));

    expect(onError).not.toHaveBeenCalled();
    // The token factory is asked again on the reconnect, not reused from the
    // first attempt's resolved value.
    expect(token).toHaveBeenCalledTimes(2);
    stop();
  });

  it('does not reconnect or call onError after the caller stops the subscription', async () => {
    vi.useFakeTimers();
    const first = makeOpenSseStream();
    fetchMock.mockResolvedValueOnce({ ok: true, body: first.stream });

    const client = new RestClient(BASE);
    const onError = vi.fn();
    const stop = client.orchestrator.subscribeJobEvents('acme', 'job-1', vi.fn(), onError);

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    stop();
    first.end();

    await vi.advanceTimersByTimeAsync(5000);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(onError).not.toHaveBeenCalled();
  });
});
