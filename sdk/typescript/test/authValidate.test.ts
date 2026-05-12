import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

describe('RestAuthClient.authValidate', () => {
  const originalFetch = globalThis.fetch;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  // Regression for Bug #6: an unauthenticated first visit (no token in
  // storage) must NOT hit the server. The server's /api/v1/auth/validate
  // requires Authorization: Bearer <token> and 400s without it — that's
  // not an error, it's just "unauthenticated".
  it('returns false WITHOUT making a network call when token is empty', async () => {
    const client = new RestClient('http://localhost');
    const result = await client.auth.authValidate();
    expect(result).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('returns false and skips fetch when token is undefined', async () => {
    const client = new RestClient('http://localhost');
    await client.auth.authValidate(undefined);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('POSTs with Bearer token when token is supplied', async () => {
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ valid: true }), { status: 200 }),
    );
    const client = new RestClient('http://localhost');
    const result = await client.auth.authValidate('good-token');
    expect(result).toBe(true);
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/auth/validate');
    expect(init.method).toBe('POST');
    expect(init.headers.Authorization).toBe('Bearer good-token');
  });

  it('returns false on 400 (missing header) and 401 (invalid token)', async () => {
    const client = new RestClient('http://localhost');

    fetchMock.mockResolvedValueOnce(new Response('', { status: 400 }));
    expect(await client.auth.authValidate('bad')).toBe(false);

    fetchMock.mockResolvedValueOnce(new Response('', { status: 401 }));
    expect(await client.auth.authValidate('expired')).toBe(false);
  });

  it('throws APIError on 5xx', async () => {
    fetchMock.mockResolvedValue(new Response('boom', { status: 503 }));
    const client = new RestClient('http://localhost');
    await expect(client.auth.authValidate('tok')).rejects.toThrow();
  });
});
