/**
 * MySessionsView.test.ts
 *
 * Verifies that MySessionsView:
 *   - Lists the caller's sessions and marks the current one.
 *   - Revokes one session with its ID after the confirmation dialog.
 *   - Revokes the other sessions with the current session's ID after the
 *     confirmation dialog.
 *   - Shows a load error.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { IAMSession } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockSessions: (IAMSession & { current?: boolean })[] = [
  {
    id: 'sess-current',
    created: '2026-11-01T10:00:00Z',
    last_active: '2026-11-12T09:00:00Z',
    expires_at: '2026-11-13T10:00:00Z',
    mfa_verified: true,
    ip_address: '10.0.0.5',
    user_agent: 'Mozilla/5.0 (X11; Linux x86_64)',
    device_info: { id: 'dev-1', name: 'Work laptop', type: 'desktop', fingerprint: 'fp1', trusted: true },
    current: true,
  },
  {
    id: 'sess-phone',
    created: '2026-11-02T10:00:00Z',
    last_active: '2026-11-11T09:00:00Z',
    expires_at: '2026-11-14T10:00:00Z',
    mfa_verified: false,
    ip_address: '192.168.1.20',
    user_agent: 'Mobile Safari',
    device_info: { id: 'dev-2', name: 'Phone', type: 'mobile', fingerprint: 'fp2', trusted: false },
  },
];

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const { mockListSessions, mockRevokeSession, mockRevokeOthers, mockGetSDKClient } = vi.hoisted(() => ({
  mockListSessions: vi.fn(),
  mockRevokeSession: vi.fn(),
  mockRevokeOthers: vi.fn(),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import MySessionsView from '../src/views/security/MySessionsView.svelte';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('MySessionsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListSessions.mockResolvedValue({ sessions: mockSessions });
    mockRevokeSession.mockResolvedValue(undefined);
    mockRevokeOthers.mockResolvedValue({ message: 'ok', kept_session_id: 'sess-current' });
    mockGetSDKClient.mockResolvedValue({
      iam: {
        listSessions: mockListSessions,
        getSession: vi.fn(),
        revokeSession: mockRevokeSession,
        revokeAllSessions: vi.fn(),
        revokeOtherSessions: mockRevokeOthers,
      },
    });
  });

  it('lists the sessions and marks the current one', async () => {
    render(MySessionsView);
    await waitFor(() => {
      expect(screen.getByText('Work laptop')).toBeInTheDocument();
      expect(screen.getByText('Phone')).toBeInTheDocument();
      expect(screen.getByText('192.168.1.20')).toBeInTheDocument();
      expect(screen.getByText('Current')).toBeInTheDocument();
    });
  });

  it('revokes one session with its ID after the confirmation', async () => {
    render(MySessionsView);
    await waitFor(() => screen.getByText('Phone'));

    await fireEvent.click(screen.getByRole('button', { name: /Revoke session Phone/i }));
    expect(screen.getByRole('dialog', { name: /Revoke session confirmation/i })).toBeInTheDocument();
    expect(mockRevokeSession).not.toHaveBeenCalled();

    await fireEvent.click(screen.getByRole('button', { name: /^Revoke$/ }));
    await waitFor(() => {
      expect(mockRevokeSession).toHaveBeenCalledWith('sess-phone');
    });
  });

  it('revokes the other sessions with the current session ID after the confirmation', async () => {
    render(MySessionsView);
    await waitFor(() => screen.getByText('Phone'));

    await fireEvent.click(screen.getByRole('button', { name: /Revoke other sessions/i }));
    expect(screen.getByRole('dialog', { name: /Revoke session confirmation/i })).toBeInTheDocument();
    expect(mockRevokeOthers).not.toHaveBeenCalled();

    await fireEvent.click(screen.getByRole('button', { name: /^Revoke$/ }));
    await waitFor(() => {
      expect(mockRevokeOthers).toHaveBeenCalledWith('sess-current');
    });
  });

  it('shows a load error', async () => {
    mockListSessions.mockRejectedValue(new Error('Network timeout'));
    render(MySessionsView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Network timeout');
    });
  });
});
