/**
 * The CA client's xkms CA passthrough routes: bundle, cert, sign, issue and
 * revoke. Go []byte fields travel as base64, so csr_pem goes out base64 and
 * every *_pem field comes back decoded to PEM text.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function respond(status: number, payload: unknown) {
  const body = JSON.stringify(payload);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => JSON.parse(body) as unknown,
    text: async () => body,
  };
}

const CERT = '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n';
const CHAIN = '-----BEGIN CERTIFICATE-----\nMIIC\n-----END CERTIFICATE-----\n';
// A stand-in PEM label: the test checks only that the field is decoded.
const KEY = '-----BEGIN TEST KEY-----\nMIIE\n-----END TEST KEY-----\n';
const CSR = '-----BEGIN CERTIFICATE REQUEST-----\nMIIR\n-----END CERTIFICATE REQUEST-----\n';

function call(i = 0): { url: string; method: string; body: unknown } {
  const [url, init] = fetchMock.mock.calls[i] as [string, RequestInit | undefined];
  return {
    url: String(url),
    method: init?.method ?? 'GET',
    body: init?.body ? JSON.parse(String(init.body)) : undefined,
  };
}

describe('xkms CA bundle', () => {
  it('GETs the bundle without a query when no option is set', async () => {
    fetchMock.mockResolvedValueOnce(
      respond(200, { bundle_pem: btoa(CERT), certificates: ['AAEC'], content_type: 'application/x-pem-file' }),
    );
    const client = new RestClient('http://localhost');
    const res = await client.ca.xkmsGetCABundle();
    const c = call();
    expect(c.url).toBe('http://localhost/api/v1/xkms/ca/bundle');
    expect(c.method).toBe('GET');
    expect(res.bundle_pem).toBe(CERT);
    expect(res.certificates).toEqual(['AAEC']);
  });

  it('sends only the options that are set, encoded', async () => {
    fetchMock.mockResolvedValueOnce(respond(200, { bundle_pem: CERT, certificates: [], content_type: 'x' }));
    const client = new RestClient('http://localhost');
    const res = await client.ca.xkmsGetCABundle({ algorithm: 'EC P-256' });
    expect(call().url).toBe('http://localhost/api/v1/xkms/ca/bundle?algorithm=EC%20P-256');
    expect(res.bundle_pem).toBe(CERT);
  });

  it('sends both options', async () => {
    fetchMock.mockResolvedValueOnce(respond(200, { bundle_pem: btoa(CERT), certificates: [], content_type: 'x' }));
    const client = new RestClient('http://localhost');
    await client.ca.xkmsGetCABundle({ store_type: 'root', algorithm: 'rsa' });
    expect(call().url).toBe('http://localhost/api/v1/xkms/ca/bundle?store_type=root&algorithm=rsa');
  });
});

describe('xkms CA cert', () => {
  it('GETs the cert and decodes certificate_pem', async () => {
    fetchMock.mockResolvedValueOnce(
      respond(200, {
        certificate_pem: btoa(CERT), subject: 'CN=ca', issuer: 'CN=ca', serial_number: '01',
        not_before: '2026-01-01T00:00:00Z', not_after: '2036-01-01T00:00:00Z', is_ca: true,
      }),
    );
    const client = new RestClient('http://localhost');
    const res = await client.ca.xkmsGetCACertificate();
    expect(call().url).toBe('http://localhost/api/v1/xkms/ca/cert');
    expect(call().method).toBe('GET');
    expect(res.certificate_pem).toBe(CERT);
    expect(res.is_ca).toBe(true);
  });

  it('sends identity only when set', async () => {
    fetchMock.mockResolvedValueOnce(respond(200, { certificate_pem: btoa(CERT) }));
    const client = new RestClient('http://localhost');
    await client.ca.xkmsGetCACertificate('svc/a b');
    expect(call().url).toBe('http://localhost/api/v1/xkms/ca/cert?identity=svc%2Fa%20b');
  });
});

describe('xkms CA sign', () => {
  it('POSTs csr_pem as base64 and decodes the PEM fields', async () => {
    fetchMock.mockResolvedValueOnce(
      respond(200, { certificate_pem: btoa(CERT), chain_pem: btoa(CHAIN), serial_number: '0a' }),
    );
    const client = new RestClient('http://localhost');
    const res = await client.ca.xkmsSignCSR({ csr_pem: CSR, profile: 'server', validity_days: 30, attestation_chain: ['AAEC'] });
    const c = call();
    expect(c.url).toBe('http://localhost/api/v1/xkms/ca/sign');
    expect(c.method).toBe('POST');
    expect(c.body).toEqual({ csr_pem: btoa(CSR), profile: 'server', validity_days: 30, attestation_chain: ['AAEC'] });
    expect(res.certificate_pem).toBe(CERT);
    expect(res.chain_pem).toBe(CHAIN);
    expect(res.serial_number).toBe('0a');
  });
});

describe('xkms CA issue', () => {
  it('POSTs the request and decodes the PEM fields', async () => {
    fetchMock.mockResolvedValueOnce(
      respond(201, { certificate_pem: btoa(CERT), chain_pem: btoa(CHAIN), private_key_pem: btoa(KEY), serial_number: '0b' }),
    );
    const client = new RestClient('http://localhost');
    const req = { profile: 'server', common_name: 'svc.example', sans: ['svc.example'], tenant_id: 't1' };
    const res = await client.ca.xkmsIssueCertificate(req);
    const c = call();
    expect(c.url).toBe('http://localhost/api/v1/xkms/ca/issue');
    expect(c.method).toBe('POST');
    expect(c.body).toEqual(req);
    expect(res.certificate_pem).toBe(CERT);
    expect(res.chain_pem).toBe(CHAIN);
    expect(res.private_key_pem).toBe(KEY);
  });
});

describe('xkms CA revoke', () => {
  it('POSTs serial and reason, without tenant_id when unset', async () => {
    fetchMock.mockResolvedValueOnce(respond(200, { success: true }));
    const client = new RestClient('http://localhost');
    const res = await client.ca.xkmsRevokeCertificate('0c', 1);
    const c = call();
    expect(c.url).toBe('http://localhost/api/v1/xkms/ca/revoke');
    expect(c.method).toBe('POST');
    expect(c.body).toEqual({ serial_number: '0c', reason: 1 });
    expect(res.success).toBe(true);
  });

  it('sends tenant_id when set', async () => {
    fetchMock.mockResolvedValueOnce(respond(200, { success: true, message: 'revoked' }));
    const client = new RestClient('http://localhost');
    await client.ca.xkmsRevokeCertificate('0c', 4, 't1');
    expect(call().body).toEqual({ serial_number: '0c', reason: 4, tenant_id: 't1' });
  });
});

describe('xkms CA without a configured CA', () => {
  it('rejects on 503', async () => {
    fetchMock.mockResolvedValue(respond(503, { error: 'CA not configured' }));
    const client = new RestClient('http://localhost');
    await expect(client.ca.xkmsGetCABundle()).rejects.toThrow();
  });
});
