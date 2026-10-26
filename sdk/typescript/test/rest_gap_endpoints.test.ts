// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for the 5 REST endpoint gaps wired into the TypeScript SDK:
//   Gap 1 – KMS setDefaultKey
//   Gap 2 – RAG document CRUD (listDocuments, getDocument, deleteDocument, reindexDocument)
//   Gap 3 – Secrets listVersions
//   Gap 4 – Seal rekey + rotateRootKey
//   Gap 5 – Embeddings setProviderStatus

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makeErrorResponse(status: number, message = 'error'): Response {
  return new Response(JSON.stringify({ error: message }), { status });
}

// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

// =============================================================================
// Gap 1: KMS setDefaultKey
// =============================================================================

describe('RestKMSClient.setDefaultKey', () => {
  it('sends PUT to /api/v1/kms/keys/{name}/default and returns the response', async () => {
    const body = { name: 'mykey', is_default: true, timestamp: '2026-04-25T00:00:00Z' };
    fetchMock.mockResolvedValue(makeResponse(body));

    const client = new RestClient('http://localhost');
    const result = await client.kms.setDefaultKey('mykey');

    expect(result.name).toBe('mykey');
    expect(result.is_default).toBe(true);

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/kms/keys/mykey/default');
    expect(init.method).toBe('PUT');
  });

  it('URL-encodes the key name', async () => {
    const body = { name: 'my key', is_default: true, timestamp: '2026-04-25T00:00:00Z' };
    fetchMock.mockResolvedValue(makeResponse(body));

    const client = new RestClient('http://localhost');
    await client.kms.setDefaultKey('my key');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/kms/keys/my%20key/default');
  });

  it('throws APIError on 404', async () => {
    fetchMock.mockResolvedValue(makeErrorResponse(404, 'key not found'));
    const client = new RestClient('http://localhost');
    await expect(client.kms.setDefaultKey('missing')).rejects.toBeInstanceOf(APIError);
  });
});

// =============================================================================
// Gap 2: RAG document CRUD
// =============================================================================

describe('RestRAGClient.listDocuments', () => {
  it('sends GET with namespace query parameter and normalises the response', async () => {
    const body = {
      documents: [{ id: 1, namespace: 'ns', content: 'hello', timestamp: 1000 }],
      total: 1,
    };
    fetchMock.mockResolvedValue(makeResponse(body));

    const client = new RestClient('http://localhost');
    const result = await client.rag.listDocuments('ns');

    expect(result.documents).toHaveLength(1);
    expect(result.total).toBe(1);

    const [url] = fetchMock.mock.calls[0];
    expect(url).toContain('/api/v1/rag/documents');
    expect(url).toContain('namespace=ns');
  });

  it('passes limit and cursor when supplied', async () => {
    fetchMock.mockResolvedValue(makeResponse({ documents: [], total: 0 }));

    const client = new RestClient('http://localhost');
    await client.rag.listDocuments('ns', 10, 'tok');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toContain('limit=10');
    expect(url).toContain('cursor=tok');
  });

  it('returns empty documents array when server returns no documents field', async () => {
    fetchMock.mockResolvedValue(makeResponse({ total: 0 }));
    const client = new RestClient('http://localhost');
    const result = await client.rag.listDocuments('ns');
    expect(result.documents).toEqual([]);
  });

  it('throws APIError on 503', async () => {
    fetchMock.mockResolvedValue(makeErrorResponse(503, 'vector service not configured'));
    const client = new RestClient('http://localhost');
    await expect(client.rag.listDocuments('ns')).rejects.toBeInstanceOf(APIError);
  });
});

describe('RestRAGClient.getDocument', () => {
  it('sends GET /api/v1/rag/documents/{id} and returns the document', async () => {
    const doc = { id: 42, namespace: 'ns', content: 'data', timestamp: 1000 };
    fetchMock.mockResolvedValue(makeResponse(doc));

    const client = new RestClient('http://localhost');
    const result = await client.rag.getDocument(42);

    expect(result.id).toBe(42);
    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/rag/documents/42');
  });

  it('throws APIError on 404', async () => {
    fetchMock.mockResolvedValue(makeErrorResponse(404, 'document not found'));
    const client = new RestClient('http://localhost');
    await expect(client.rag.getDocument(99)).rejects.toBeInstanceOf(APIError);
  });
});

describe('RestRAGClient.deleteDocument', () => {
  it('sends DELETE /api/v1/rag/documents/{id}', async () => {
    fetchMock.mockResolvedValue(makeResponse({}));

    const client = new RestClient('http://localhost');
    await client.rag.deleteDocument(7);

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/rag/documents/7');
    expect(init.method).toBe('DELETE');
  });

  it('throws APIError on 404', async () => {
    fetchMock.mockResolvedValue(makeErrorResponse(404, 'document not found'));
    const client = new RestClient('http://localhost');
    await expect(client.rag.deleteDocument(99)).rejects.toBeInstanceOf(APIError);
  });
});

describe('RestRAGClient.reindexDocument', () => {
  it('sends POST /api/v1/rag/documents/{id}/reindex with optional body', async () => {
    const doc = { id: 5, namespace: 'ns', content: 'updated', timestamp: 2000 };
    fetchMock.mockResolvedValue(makeResponse(doc));

    const client = new RestClient('http://localhost');
    const result = await client.rag.reindexDocument(5, { provider: 'ollama' });

    expect(result.id).toBe(5);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/rag/documents/5/reindex');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ provider: 'ollama' });
  });

  it('sends an empty body when no request is provided', async () => {
    const doc = { id: 3, namespace: 'ns', content: 'data', timestamp: 1000 };
    fetchMock.mockResolvedValue(makeResponse(doc));

    const client = new RestClient('http://localhost');
    await client.rag.reindexDocument(3);

    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body)).toEqual({});
  });

  it('throws APIError on 404', async () => {
    fetchMock.mockResolvedValue(makeErrorResponse(404, 'document not found'));
    const client = new RestClient('http://localhost');
    await expect(client.rag.reindexDocument(99)).rejects.toBeInstanceOf(APIError);
  });
});

// =============================================================================
// Gap 3: Secrets listVersions
// =============================================================================

describe('RestSecretsClient.listVersions', () => {
  it('sends GET /api/v1/secrets-versions/{path} and returns versions', async () => {
    const body = {
      versions: [{ secret_id: 'prod/db', version: 1, created_at: '2026-01-01T00:00:00Z' }],
      count: 1,
    };
    fetchMock.mockResolvedValue(makeResponse(body));

    const client = new RestClient('http://localhost');
    const result = await client.secrets.listVersions('prod/db');

    expect(result.versions).toHaveLength(1);
    expect(result.count).toBe(1);

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/secrets-versions/prod%2Fdb');
  });

  it('URL-encodes path segments', async () => {
    fetchMock.mockResolvedValue(makeResponse({ versions: [], count: 0 }));

    const client = new RestClient('http://localhost');
    await client.secrets.listVersions('a b/c');
    const [url] = fetchMock.mock.calls[0];
    expect(url).toContain('a%20b%2Fc');
  });

  it('returns empty versions array when server omits the versions field', async () => {
    fetchMock.mockResolvedValue(makeResponse({ count: 0 }));
    const client = new RestClient('http://localhost');
    const result = await client.secrets.listVersions('x');
    expect(result.versions).toEqual([]);
  });

  it('throws APIError on 404', async () => {
    fetchMock.mockResolvedValue(makeErrorResponse(404, 'secret not found'));
    const client = new RestClient('http://localhost');
    await expect(client.secrets.listVersions('missing')).rejects.toBeInstanceOf(APIError);
  });
});

// =============================================================================
// Gap 4: Seal rekey + rotateRootKey
// =============================================================================

describe('RestSealClient.rekey', () => {
  it('POSTs to /api/v1/seal/rekey and decodes the share-free response', async () => {
    // The body pkg/service RekeyResponse marshals to: the new version and
    // time, never share material. Officers re-claim their shares instead.
    const body = { version: 2, timestamp: '2026-04-25T00:00:00Z' };
    fetchMock.mockResolvedValue(makeResponse(body));

    const client = new RestClient('http://localhost');
    const result = await client.seal.rekey({ new_threshold: 2, new_total_shares: 5 });

    expect(result).toEqual(body);
    // @ts-expect-error a rekey returns no shares (qrdb share-free contract).
    expect(result.shares).toBeUndefined();

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/seal/rekey');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ new_threshold: 2, new_total_shares: 5 });
  });

  it('throws APIError on 503 when sealed', async () => {
    fetchMock.mockResolvedValue(makeErrorResponse(503, 'database is sealed'));
    const client = new RestClient('http://localhost');
    await expect(client.seal.rekey({ new_threshold: 1, new_total_shares: 1 })).rejects.toBeInstanceOf(APIError);
  });
});

describe('RestSealClient.rotateRootKey', () => {
  it('POSTs to /api/v1/seal/rotate-root with no body', async () => {
    const body = { success: true, message: 'root key rotated', timestamp: '2026-04-25T00:00:00Z' };
    fetchMock.mockResolvedValue(makeResponse(body));

    const client = new RestClient('http://localhost');
    const result = await client.seal.rotateRootKey();

    expect(result.success).toBe(true);

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/seal/rotate-root');
    expect(init.method).toBe('POST');
  });

  it('throws APIError on 503 when sealed', async () => {
    fetchMock.mockResolvedValue(makeErrorResponse(503, 'database is sealed'));
    const client = new RestClient('http://localhost');
    await expect(client.seal.rotateRootKey()).rejects.toBeInstanceOf(APIError);
  });
});

// =============================================================================
// Gap 5: Embeddings setProviderStatus
// =============================================================================

describe('RestEmbeddingsClient.setProviderStatus', () => {
  it('sends PUT to /api/v1/embedding/providers/{name}/status with enabled=true', async () => {
    const body = { name: 'ollama', enabled: true, timestamp: '2026-04-25T00:00:00Z' };
    fetchMock.mockResolvedValue(makeResponse(body));

    const client = new RestClient('http://localhost');
    const result = await client.embeddings.setProviderStatus('ollama', true);

    expect(result.name).toBe('ollama');
    expect(result.enabled).toBe(true);

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/embedding/providers/ollama/status');
    expect(init.method).toBe('PUT');
    expect(JSON.parse(init.body)).toEqual({ enabled: true });
  });

  it('sends enabled=false to disable a provider', async () => {
    const body = { name: 'openai', enabled: false, timestamp: '2026-04-25T00:00:00Z' };
    fetchMock.mockResolvedValue(makeResponse(body));

    const client = new RestClient('http://localhost');
    const result = await client.embeddings.setProviderStatus('openai', false);

    expect(result.enabled).toBe(false);
    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body)).toEqual({ enabled: false });
  });

  it('URL-encodes the provider name', async () => {
    const body = { name: 'my provider', enabled: true, timestamp: '2026-04-25T00:00:00Z' };
    fetchMock.mockResolvedValue(makeResponse(body));

    const client = new RestClient('http://localhost');
    await client.embeddings.setProviderStatus('my provider', true);

    const [url] = fetchMock.mock.calls[0];
    expect(url).toContain('my%20provider');
  });

  it('throws APIError on 404 when provider not found', async () => {
    fetchMock.mockResolvedValue(makeErrorResponse(404, 'provider not found'));
    const client = new RestClient('http://localhost');
    await expect(client.embeddings.setProviderStatus('missing', true)).rejects.toBeInstanceOf(APIError);
  });
});

// =============================================================================
// Bug #88: KMS listKeyVersions – unix-second created_at must be ISO string
// =============================================================================

describe('RestKMSClient.listKeyVersions', () => {
  it('converts unix-second created_at to an ISO string (bug #88)', async () => {
    // Server returns created_at as a Unix epoch second (int64).
    // 1746230400 seconds = 2025-05-03T00:00:00.000Z
    const serverTimestamp = 1746230400;
    fetchMock.mockResolvedValue(
      makeResponse({
        key_id: 'my-key',
        versions: [
          { version: 1, status: 'ENABLED', created_at: serverTimestamp },
          { version: 2, status: 'ENABLED', created_at: serverTimestamp },
        ],
      }),
    );

    const client = new RestClient('http://localhost');
    const versions = await client.kms.listKeyVersions('my-key');

    expect(versions).toHaveLength(2);
    // Each version's created_at must be the ISO representation, NOT the raw integer.
    for (const v of versions) {
      expect(typeof v.created_at).toBe('string');
      // Must parse back to a date in year 2025, not 1970.
      const d = new Date(v.created_at as string);
      expect(d.getFullYear()).toBe(2025);
    }
  });

  it('returns undefined for created_at when the server omits it', async () => {
    fetchMock.mockResolvedValue(
      makeResponse({
        key_id: 'my-key',
        versions: [{ version: 1, status: 'ENABLED' }],
      }),
    );

    const client = new RestClient('http://localhost');
    const versions = await client.kms.listKeyVersions('my-key');

    expect(versions).toHaveLength(1);
    expect(versions[0].created_at).toBeUndefined();
  });

  it('returns undefined for created_at when the server sends 0 (zero timestamp)', async () => {
    fetchMock.mockResolvedValue(
      makeResponse({
        key_id: 'my-key',
        versions: [{ version: 1, status: 'ENABLED', created_at: 0 }],
      }),
    );

    const client = new RestClient('http://localhost');
    const versions = await client.kms.listKeyVersions('my-key');

    expect(versions[0].created_at).toBeUndefined();
  });

  it('returns empty array when server returns no versions', async () => {
    fetchMock.mockResolvedValue(makeResponse({ key_id: 'my-key', versions: [] }));

    const client = new RestClient('http://localhost');
    const versions = await client.kms.listKeyVersions('my-key');

    expect(versions).toHaveLength(0);
  });
});
