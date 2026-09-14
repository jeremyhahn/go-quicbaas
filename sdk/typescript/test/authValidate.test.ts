import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';

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
  // storage) must NOT hit the server. The session-validate verb requires
  // Authorization: Bearer <token> and 400s without it — that's not an
  // error, it's just "unauthenticated".
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
    // The IAM-namespaced verb, NOT /api/v1/auth/validate: on a composed node
    // xkms's module owns that path and registers GET there, so this POST
    // matched no route and fell through to mTLS, 401ing a bearer the daemon
    // had just minted (go-quicbaas#256).
    expect(url).toBe('http://localhost/api/v1/iam/validate');
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

  // -------------------------------------------------------------------------
  // This is an authentication boundary, so it fails CLOSED. Every case below
  // is a 2xx that does NOT carry the server's affirmative "yes", and an
  // earlier draft of this method authenticated the session for all of them:
  // it read `body.valid !== false` with a `catch { return true }`. A 200 with
  // an empty object, a stringly-typed "false", or an HTML error page from an
  // intermediary would each have produced a live session client-side.
  // -------------------------------------------------------------------------
  const jsonResponse = (value: unknown): Response =>
    new Response(JSON.stringify(value), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });

  it('returns true ONLY for a literal boolean valid:true', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ valid: true }));
    const client = new RestClient('http://localhost');
    expect(await client.auth.authValidate('tok')).toBe(true);
  });

  it('returns false for an explicit valid:false', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ valid: false }));
    const client = new RestClient('http://localhost');
    expect(await client.auth.authValidate('tok')).toBe(false);
  });

  it('throws on a 200 whose body omits valid', async () => {
    fetchMock.mockResolvedValue(jsonResponse({}));
    const client = new RestClient('http://localhost');
    await expect(client.auth.authValidate('tok')).rejects.toThrow(APIError);
  });

  // "false" is a non-empty string and therefore truthy — the exact shape that
  // slips past a `!== false` check while meaning the opposite.
  it.each([
    ['the string "false"', { valid: 'false' }],
    ['the string "true"', { valid: 'true' }],
    ['null', { valid: null }],
    ['a number', { valid: 1 }],
  ])('throws when valid is %s rather than a boolean', async (_label, body) => {
    fetchMock.mockResolvedValue(jsonResponse(body));
    const client = new RestClient('http://localhost');
    await expect(client.auth.authValidate('tok')).rejects.toThrow(APIError);
  });

  it.each([
    ['a JSON null body', jsonResponse(null)],
    ['a JSON array body', jsonResponse([{ valid: true }])],
  ])('throws on %s', async (_label, response) => {
    fetchMock.mockResolvedValue(response);
    const client = new RestClient('http://localhost');
    await expect(client.auth.authValidate('tok')).rejects.toThrow(APIError);
  });

  it('throws on an empty 204 rather than authenticating it', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    const client = new RestClient('http://localhost');
    await expect(client.auth.authValidate('tok')).rejects.toThrow(APIError);
  });

  // An intermediary that answers 200 text/html is the realistic version of
  // this: a captive portal, a misrouted proxy, a load balancer error page.
  it('throws on a 200 carrying an HTML body from an intermediary', async () => {
    fetchMock.mockResolvedValue(
      new Response('<html><body>Gateway</body></html>', {
        status: 200,
        headers: { 'Content-Type': 'text/html' },
      }),
    );
    const client = new RestClient('http://localhost');
    await expect(client.auth.authValidate('tok')).rejects.toThrow(APIError);
  });

  it('throws on a truncated JSON body', async () => {
    fetchMock.mockResolvedValue(
      new Response('{"valid":tr', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new RestClient('http://localhost');
    await expect(client.auth.authValidate('tok')).rejects.toThrow(APIError);
  });

  // Asserting the TYPED error matters: a leaked SyntaxError from JSON.parse or
  // a TypeError from narrowing would satisfy a bare rejects.toThrow() while
  // meaning the method crashed rather than refused. Every throw above is the
  // SDK's own APIError, so callers can branch on it.
  it('throws APIError — not a leaked SyntaxError — on a malformed body', async () => {
    fetchMock.mockResolvedValue(
      new Response('not json at all', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new RestClient('http://localhost');
    await expect(client.auth.authValidate('tok')).rejects.toBeInstanceOf(APIError);
  });

  // A polluted Object.prototype must not supply the affirmative answer the
  // server withheld. The check is hasOwnProperty, so a 200 {} still refuses.
  it('ignores an inherited valid property from a polluted prototype', async () => {
    const proto = Object.prototype as unknown as { valid?: boolean };
    proto.valid = true;
    try {
      fetchMock.mockResolvedValue(jsonResponse({}));
      const client = new RestClient('http://localhost');
      await expect(client.auth.authValidate('tok')).rejects.toThrow(APIError);
    } finally {
      delete proto.valid;
    }
  });
});
