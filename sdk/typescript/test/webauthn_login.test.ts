import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

// The WebAuthn login rides the unified login ceremony
// (/api/v1/auth/login/{begin,finish}, type=fido2). These tests pin the wire:
// begin extracts the challenge envelope + both session ids from the ceremony
// response; finish submits the proof triple the server's WebAuthn verifier
// reads (lookup_key = credential-id bytes, signature = assertion JSON,
// method_metadata = { session_id, credential_id }) and maps the bearer +
// refresh credential onto the token bundle.

describe('RestAuthClient.webAuthnBeginLogin / webAuthnFinishLogin', () => {
  const originalFetch = globalThis.fetch;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('begin POSTs the fido2 ceremony body and unpacks the challenge', async () => {
    const options = { publicKey: { challenge: 'abc' } };
    fetchMock.mockResolvedValue(
      new Response(
        JSON.stringify({
          session_id: 'ceremony-1',
          challenge_metadata: {
            options: JSON.stringify(options),
            session_id: 'rp-1',
          },
        }),
        { status: 200 },
      ),
    );

    const client = new RestClient('http://localhost');
    const challenge = await client.auth.webAuthnBeginLogin('acme', 'alice@example.com');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/auth/login/begin');
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      type: 'fido2',
      hint: 'alice@example.com',
      tenant_id: 'acme',
    });

    expect(challenge.session_id).toBe('ceremony-1');
    expect(challenge.rp_session_id).toBe('rp-1');
    expect(challenge.options).toEqual(options);
  });

  it('begin throws when the server returns no challenge options', async () => {
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ session_id: 'ceremony-1' }), { status: 200 }),
    );

    const client = new RestClient('http://localhost');
    await expect(
      client.auth.webAuthnBeginLogin('acme', 'alice@example.com'),
    ).rejects.toThrow(/challenge options/);
  });

  it('begin surfaces the uniform 401 rejection', async () => {
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ error: 'authentication failed' }), { status: 401 }),
    );

    const client = new RestClient('http://localhost');
    await expect(
      client.auth.webAuthnBeginLogin('acme', 'ghost@example.com'),
    ).rejects.toThrow(/authentication failed/);
  });

  it('finish submits the proof triple and maps tokens', async () => {
    const expires = new Date(Date.now() + 900_000).toISOString();
    fetchMock.mockResolvedValue(
      new Response(
        JSON.stringify({
          bearer: 'bearer-1',
          refresh_token: 'refresh-1',
          expires_at: expires,
        }),
        { status: 200 },
      ),
    );

    const client = new RestClient('http://localhost');
    const credential = {
      id: 'cred-id',
      rawId: 'cred-id',
      type: 'public-key',
      response: {
        authenticatorData: 'a',
        clientDataJSON: 'c',
        signature: 's',
      },
    };
    const tokens = await client.auth.webAuthnFinishLogin(
      { session_id: 'ceremony-1', rp_session_id: 'rp-1', options: { publicKey: { challenge: 'x' } } },
      credential,
    );

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/auth/login/finish');
    const body = JSON.parse((init as RequestInit).body as string);
    expect(body.session_id).toBe('ceremony-1');
    expect(body.type).toBe('fido2');
    // lookup_key is standard base64 of the UTF-8 bytes of the base64url
    // credential id ("cred-id"), matching Go []byte JSON decoding.
    expect(atob(body.lookup_key)).toBe('cred-id');
    expect(JSON.parse(atob(body.signature))).toEqual(credential);
    expect(body.method_metadata).toEqual({ session_id: 'rp-1', credential_id: 'cred-id' });

    expect(tokens.access_token).toBe('bearer-1');
    expect(tokens.refresh_token).toBe('refresh-1');
    expect(tokens.token_type).toBe('Bearer');
    expect(tokens.expires_in).toBeGreaterThan(0);
  });

  it('finish surfaces the uniform 401 rejection', async () => {
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ error: 'authentication failed' }), { status: 401 }),
    );

    const client = new RestClient('http://localhost');
    await expect(
      client.auth.webAuthnFinishLogin(
        { session_id: 's', rp_session_id: 'r', options: { publicKey: { challenge: 'x' } } },
        {
          id: 'x',
          rawId: 'x',
          type: 'public-key',
          response: { authenticatorData: 'a', clientDataJSON: 'c', signature: 's' },
        },
      ),
    ).rejects.toThrow(/authentication failed/);
  });
});
