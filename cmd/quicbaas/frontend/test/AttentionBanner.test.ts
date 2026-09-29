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

/**
 * AttentionBanner.test.ts
 *
 * Verifies the banner renders one entry per visible environmental alert,
 * renders nothing when there are none, and dismisses an alert for the
 * session when its dismiss button is clicked.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import type { AttentionItem } from '@quicbaas/sdk';

const { mockDismissAlert, state } = vi.hoisted(() => ({
  mockDismissAlert: vi.fn(),
  state: { visibleAlerts: [] as AttentionItem[] },
}));

vi.mock('../src/stores/attentionStore.svelte.ts', () => ({
  attentionStore: {
    get visibleAlerts() { return state.visibleAlerts; },
  },
  dismissAlert: mockDismissAlert,
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeAttention: vi.fn(),
}));

import AttentionBanner from '../src/components/orchestrator/AttentionBanner.svelte';

beforeEach(() => {
  state.visibleAlerts = [];
  mockDismissAlert.mockClear();
});

describe('AttentionBanner', () => {
  it('renders nothing when there are no visible alerts', () => {
    render(AttentionBanner);
    expect(screen.queryByTestId('attention-banner')).not.toBeInTheDocument();
  });

  it('renders one row per alert with its message', async () => {
    state.visibleAlerts = [
      { id: 'agent_offline:a1', kind: 'agent_offline', subject: 'Fleet agent agent-04 is offline', message: 'work will not run', severity: 'critical' },
      { id: 'barrier_sealed:system', kind: 'barrier_sealed', subject: 'The system barrier is sealed', message: 'no data operation can run', severity: 'critical' },
    ];
    render(AttentionBanner);

    expect(await screen.findByText(/Fleet agent agent-04 is offline/)).toBeInTheDocument();
    expect(screen.getByText(/The system barrier is sealed/)).toBeInTheDocument();
  });

  it('dismisses an alert when its dismiss button is clicked', async () => {
    state.visibleAlerts = [
      { id: 'agent_offline:a1', kind: 'agent_offline', subject: 'Fleet agent agent-04 is offline', message: 'work will not run', severity: 'critical' },
    ];
    render(AttentionBanner);

    const dismiss = await screen.findByTestId('attention-banner-dismiss');
    await fireEvent.click(dismiss);

    expect(mockDismissAlert).toHaveBeenCalledWith('agent_offline:a1');
  });
});
