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

// Tests for the cost-settings verbs (go-quicbaas#1058): a tenant's electricity
// rate and engine draws.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { QuicClient } from '../src/quic';
import type { CostSettings } from '../src/types';

const serverBody: CostSettings = {
  settings: { tenant_id: 'acme', electricity_price_per_kwh: 0.2, engine_power_watts: { openvino: 12 } },
  electricity: { price_per_kwh: 0.2, source: 'tenant_settings' },
  engine_power_watts: { openvino: 12, ollama: 250 },
};

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn(() =>
    Promise.resolve(new Response(JSON.stringify(serverBody), { status: 200, headers: { 'Content-Type': 'application/json' } })),
  );
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

describe('RestOrchestratorClient cost settings', () => {
  it('reads the tenant scoped cost-settings endpoint', async () => {
    const got = await new RestClient('http://localhost').orchestrator.getCostSettings({ tenant_id: 'acme' });
    expect(fetchMock.mock.calls[0][0]).toBe('http://localhost/api/v1/orchestrator/cost-settings?tenant_id=acme');
    expect(got.electricity.price_per_kwh).toBe(0.2);
    expect(got.engine_power_watts?.ollama).toBe(250);
  });

  it('omits the tenant when none is named', async () => {
    await new RestClient('http://localhost').orchestrator.getCostSettings({});
    expect(fetchMock.mock.calls[0][0]).toBe('http://localhost/api/v1/orchestrator/cost-settings');
  });

  it('PUTs the rate and draws, replacing the settings', async () => {
    await new RestClient('http://localhost').orchestrator.setCostSettings({
      tenant_id: 'acme',
      electricity_price_per_kwh: 0.2,
      engine_power_watts: { openvino: 12 },
    });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/orchestrator/cost-settings?tenant_id=acme');
    expect(init.method).toBe('PUT');
    expect(JSON.parse(init.body as string)).toEqual({ electricity_price_per_kwh: 0.2, engine_power_watts: { openvino: 12 } });
  });

  it('is refused, not silently empty, on a transport that does not carry it', async () => {
    const quic = new QuicClient('https://localhost:4433');
    await expect(quic.orchestrator.getCostSettings({ tenant_id: 'acme' })).rejects.toThrow();
  });
});
