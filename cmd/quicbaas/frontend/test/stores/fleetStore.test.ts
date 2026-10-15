// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

/**
 * fleetStore presence: "N online" counts only nodes whose agent has started
 * and is still heartbeating, while the spawn dialog keeps offering nodes that
 * are registered but not yet heard from (go-quicbaas#851).
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import type { FleetNode } from '@quicbaas/sdk';

const { listFleetNodes } = vi.hoisted(() => ({ listFleetNodes: vi.fn() }));

vi.mock('../../src/stores/sdkClient.ts', () => ({
  getSDKClient: async () => ({ orchestrator: { listFleetNodes } }),
}));

import {
  FLEET_HEARTBEAT_TIMEOUT_MS,
  fleetStore,
  isFleetNodeOnline,
  onlineFleetNodes,
  refreshFleet,
  spawnableFleetNodes,
} from '../../src/stores/fleetStore.svelte.ts';

const NOW_MS = Date.UTC(2026, 9, 15, 12, 0, 0);

/** A node with the given status, last heard from `ageMs` before NOW_MS. */
function node(id: string, status: string, ageMs: number | undefined): FleetNode {
  const lastSeen = ageMs === undefined ? undefined : (NOW_MS - ageMs) * 1_000_000;
  return { id, name: id, host: `${id}.local`, status, last_seen: lastSeen } as unknown as FleetNode;
}

function ids(list: FleetNode[]): string[] {
  return list.map((n) => n.id);
}

afterEach(() => {
  vi.useRealTimers();
  listFleetNodes.mockReset();
});

describe('isFleetNodeOnline', () => {
  it('counts running and idle nodes heard from within the heartbeat timeout', () => {
    expect(isFleetNodeOnline(node('r', 'running', 1_000), NOW_MS)).toBe(true);
    expect(isFleetNodeOnline(node('i', 'idle', FLEET_HEARTBEAT_TIMEOUT_MS), NOW_MS)).toBe(true);
  });

  it('does not count a registered node, however fresh', () => {
    expect(isFleetNodeOnline(node('g', 'registered', 1_000), NOW_MS)).toBe(false);
  });

  it('does not count a running node whose last heartbeat is past the timeout', () => {
    expect(isFleetNodeOnline(node('s', 'running', FLEET_HEARTBEAT_TIMEOUT_MS + 1), NOW_MS)).toBe(false);
  });

  it('does not count a running node with a missing or unparseable last_seen', () => {
    expect(isFleetNodeOnline(node('m', 'running', undefined), NOW_MS)).toBe(false);
    const zero = { ...node('z', 'running', 0), last_seen: 0 } as FleetNode;
    const text = { ...node('t', 'running', 0), last_seen: 'yesterday' } as unknown as FleetNode;
    const nan = { ...node('n', 'running', 0), last_seen: Number.NaN } as FleetNode;
    expect(isFleetNodeOnline(zero, NOW_MS)).toBe(false);
    expect(isFleetNodeOnline(text, NOW_MS)).toBe(false);
    expect(isFleetNodeOnline(nan, NOW_MS)).toBe(false);
  });

  it('matches the server heartbeat timeout of 90 seconds', () => {
    expect(FLEET_HEARTBEAT_TIMEOUT_MS).toBe(90_000);
  });
});

describe('onlineFleetNodes and spawnableFleetNodes', () => {
  const fleet = [
    node('fresh', 'running', 5_000),
    node('stale', 'running', 10 * 60_000),
    node('registered', 'registered', undefined),
    node('offline', 'offline', 1_000),
  ];

  it('online keeps only started, fresh nodes', () => {
    expect(ids(onlineFleetNodes(fleet, NOW_MS))).toEqual(['fresh']);
  });

  it('spawnable includes registered nodes and ignores freshness', () => {
    expect(ids(spawnableFleetNodes(fleet))).toEqual(['fresh', 'stale', 'registered']);
  });
});

describe('fleetStore getters', () => {
  it('apply the rules to the fetched fleet at the current time', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(NOW_MS);
    listFleetNodes.mockResolvedValue({
      nodes: [node('fresh', 'idle', 1_000), node('stale', 'idle', 120_000), node('registered', 'registered', undefined)],
    });

    await refreshFleet();

    expect(ids(fleetStore.onlineNodes)).toEqual(['fresh']);
    expect(ids(fleetStore.spawnableNodes)).toEqual(['fresh', 'stale', 'registered']);

    vi.setSystemTime(NOW_MS + FLEET_HEARTBEAT_TIMEOUT_MS);
    expect(ids(fleetStore.onlineNodes)).toEqual([]);
  });
});
