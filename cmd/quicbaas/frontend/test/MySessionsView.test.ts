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
import { render, screen, fireEvent, waitFor, within } from '@testing-library/svelte';
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

const {
  mockListSessions,
  mockRevokeSession,
  mockRevokeOthers,
  mockRevokeAll,
  mockGetSession,
  mockGetSDKClient,
} = vi.hoisted(() => ({
  mockListSessions: vi.fn(),
  mockRevokeAll: vi.fn(),
  mockGetSession: vi.fn(),
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
import { showToast } from '../src/stores/toast.svelte.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('MySessionsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListSessions.mockResolvedValue({ sessions: mockSessions });
    mockRevokeSession.mockResolvedValue(undefined);
    mockRevokeAll.mockResolvedValue(undefined);
    mockGetSession.mockResolvedValue(mockSessions[1]);
    mockRevokeOthers.mockResolvedValue({ message: 'ok', kept_session_id: 'sess-current' });
    mockGetSDKClient.mockResolvedValue({
      iam: {
        listSessions: mockListSessions,
        getSession: mockGetSession,
        revokeSession: mockRevokeSession,
        revokeAllSessions: mockRevokeAll,
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

  it('signs out of every session with no options only after the confirmation, then reloads', async () => {
    render(MySessionsView);
    await waitFor(() => screen.getByText('Phone'));
    expect(mockListSessions).toHaveBeenCalledTimes(1);

    await fireEvent.click(screen.getByRole('button', { name: 'Sign out of every session' }));
    const dialog = screen.getByRole('dialog', { name: /Revoke session confirmation/i });
    expect(dialog).toHaveTextContent(/every session, this one included/i);
    expect(mockRevokeAll).not.toHaveBeenCalled();

    await fireEvent.click(screen.getByRole('button', { name: /^Revoke$/ }));
    await waitFor(() => {
      expect(mockRevokeAll).toHaveBeenCalledTimes(1);
      expect(mockRevokeAll.mock.calls[0]).toHaveLength(0);
      expect(showToast).toHaveBeenCalledWith('success', 'Signed out of every session');
      expect(mockListSessions).toHaveBeenCalledTimes(2);
    });
    expect(screen.queryByRole('dialog', { name: /Revoke session confirmation/i })).not.toBeInTheDocument();
  });

  it('cancelling sign out everywhere calls nothing', async () => {
    render(MySessionsView);
    await waitFor(() => screen.getByText('Phone'));

    await fireEvent.click(screen.getByRole('button', { name: 'Sign out of every session' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog', { name: /Revoke session confirmation/i })).not.toBeInTheDocument();
    expect(mockRevokeAll).not.toHaveBeenCalled();
    expect(mockRevokeOthers).not.toHaveBeenCalled();
    expect(mockRevokeSession).not.toHaveBeenCalled();
  });

  it('shows a sign out everywhere error', async () => {
    mockRevokeAll.mockRejectedValue(new Error('Forbidden'));
    render(MySessionsView);
    await waitFor(() => screen.getByText('Phone'));

    await fireEvent.click(screen.getByRole('button', { name: 'Sign out of every session' }));
    await fireEvent.click(screen.getByRole('button', { name: /^Revoke$/ }));
    await waitFor(() => {
      expect(showToast).toHaveBeenCalledWith('error', 'Forbidden');
    });
    expect(mockListSessions).toHaveBeenCalledTimes(1);
  });

  it('shows a session\'s details from getSession with the row ID', async () => {
    render(MySessionsView);
    await waitFor(() => screen.getByText('Phone'));

    await fireEvent.click(screen.getByRole('button', { name: 'Details for session Phone' }));
    expect(mockGetSession).toHaveBeenCalledWith('sess-phone');
    const dialog = await screen.findByRole('dialog', { name: 'Session details' });
    const s = mockSessions[1];
    await waitFor(() => {
      expect(within(dialog).getByText('sess-phone')).toBeInTheDocument();
    });
    const d = within(dialog);
    expect(d.getByText(new Date(s.created).toLocaleString())).toBeInTheDocument();
    expect(d.getByText(new Date(s.last_active).toLocaleString())).toBeInTheDocument();
    expect(d.getByText(new Date(s.expires_at).toLocaleString())).toBeInTheDocument();
    expect(d.getByText('192.168.1.20')).toBeInTheDocument();
    expect(d.getByText('Mobile Safari')).toBeInTheDocument();
    expect(d.getByText('dev-2')).toBeInTheDocument();
    expect(d.getByText('Phone')).toBeInTheDocument();
    expect(d.getByText('mobile')).toBeInTheDocument();
    expect(d.getByText('fp2')).toBeInTheDocument();
    // MFA and trusted are both false for this session.
    expect(d.getAllByText('No')).toHaveLength(2);

    await fireEvent.click(d.getByRole('button', { name: 'Close' }));
    expect(screen.queryByRole('dialog', { name: 'Session details' })).not.toBeInTheDocument();
  });

  it('shows a getSession error in the details dialog', async () => {
    mockGetSession.mockRejectedValue(new Error('Session not found'));
    render(MySessionsView);
    await waitFor(() => screen.getByText('Phone'));

    await fireEvent.click(screen.getByRole('button', { name: 'Details for session Work laptop' }));
    expect(mockGetSession).toHaveBeenCalledWith('sess-current');
    const dialog = await screen.findByRole('dialog', { name: 'Session details' });
    await waitFor(() => {
      expect(within(dialog).getByRole('alert')).toHaveTextContent('Session not found');
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
