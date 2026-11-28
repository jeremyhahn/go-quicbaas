/*
 * Copyright (c) 2026 Jeremy Hahn
 * Copyright (c) 2026 Automate The Things, LLC
 *
 * This file is part of go-quicbaas.
 *
 * go-quicbaas is dual-licensed:
 *
 * 1. GNU Affero General Public License v3.0 (AGPL-3.0)
 * 2. Commercial License
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { ImpersonationSession } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const activeSession: ImpersonationSession = {
  id: '101',
  admin_user_id: '1',
  admin_username: 'root-admin',
  target_user_id: '42',
  target_username: 'alice',
  tenant_id: '7',
  reason: 'ticket-881',
  started_at: '2026-11-12T10:00:00Z',
  expires_at: '2026-11-12T10:30:00Z',
  operation_count: 3,
  audit_log_id: '9001',
};

const endedSession: ImpersonationSession = {
  ...activeSession,
  id: '100',
  reason: 'ticket-880',
  ended_at: '2026-11-11T10:20:00Z',
};

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const {
  mockListImpersonations,
  mockStartImpersonation,
  mockEndImpersonation,
  mockListUserImpersonations,
  mockListTenants,
  mockListUsers,
  mockGetSDKClient,
  mockShowToast,
} = vi.hoisted(() => ({
  mockListImpersonations: vi.fn(),
  mockStartImpersonation: vi.fn(),
  mockEndImpersonation: vi.fn(),
  mockListUserImpersonations: vi.fn(),
  mockListTenants: vi.fn(),
  mockListUsers: vi.fn(),
  mockGetSDKClient: vi.fn(),
  mockShowToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: mockShowToast,
}));

import ImpersonationView from '../src/views/security/ImpersonationView.svelte';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ImpersonationView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListImpersonations.mockResolvedValue({ sessions: [activeSession], total: 1 });
    mockStartImpersonation.mockResolvedValue({ ...activeSession, id: '102' });
    mockEndImpersonation.mockResolvedValue({ ...activeSession, ended_at: '2026-11-12T10:05:00Z' });
    mockListUserImpersonations.mockResolvedValue({ user_id: '42', sessions: [endedSession] });
    mockListTenants.mockResolvedValue({ tenants: [{ id: '7', name: 'acme' }] });
    mockListUsers.mockResolvedValue({ users: [{ id: '42', username: 'alice', email: 'a@x', tenant_id: '7' }] });
    mockGetSDKClient.mockResolvedValue({
      iam: {
        listImpersonations: mockListImpersonations,
        startImpersonation: mockStartImpersonation,
        endImpersonation: mockEndImpersonation,
        listUserImpersonations: mockListUserImpersonations,
        listTenants: mockListTenants,
        listUsers: mockListUsers,
      },
    });
  });

  async function fillStartForm(reason: string): Promise<void> {
    await waitFor(() => expect(mockListUsers).toHaveBeenCalledWith('7'));
    await waitFor(() => screen.getByRole('option', { name: 'alice' }));
    await fireEvent.change(screen.getByLabelText('Duration'), { target: { value: '1h' } });
    await fireEvent.input(screen.getByLabelText('Reason'), { target: { value: reason } });
  }

  it('lists active sessions by default and filters by admin', async () => {
    render(ImpersonationView);
    await waitFor(() => {
      expect(screen.getByText('ticket-881')).toBeInTheDocument();
      expect(screen.getByText('Active')).toBeInTheDocument();
    });
    expect(mockListImpersonations).toHaveBeenCalledWith({ activeOnly: true, adminUserID: undefined });

    await fireEvent.input(screen.getByLabelText('Admin user ID filter'), { target: { value: '5' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Filter' }));
    await waitFor(() => {
      expect(mockListImpersonations).toHaveBeenLastCalledWith({ activeOnly: true, adminUserID: '5' });
    });
  });

  it('starts a session after confirmation and reloads', async () => {
    render(ImpersonationView);
    await fillStartForm('support ticket');
    await fireEvent.click(screen.getByRole('button', { name: /Start Impersonation/i }));
    expect(mockStartImpersonation).not.toHaveBeenCalled();
    await fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }));

    await waitFor(() => {
      expect(mockStartImpersonation).toHaveBeenCalledWith({
        target_user_id: '42',
        tenant_id: '7',
        reason: 'support ticket',
        duration: '1h',
      });
      expect(mockListImpersonations).toHaveBeenCalledTimes(2);
    });
  });

  it('calls nothing when the reason is missing', async () => {
    render(ImpersonationView);
    await fillStartForm('   ');
    const button = screen.getByRole('button', { name: /Start Impersonation/i });
    expect(button).toBeDisabled();
    await fireEvent.submit(screen.getByLabelText('Start impersonation form'));
    expect(screen.queryByRole('button', { name: 'Confirm' })).toBeNull();
    expect(mockStartImpersonation).not.toHaveBeenCalled();
  });

  it('ends a session after confirmation with a session result', async () => {
    render(ImpersonationView);
    await fireEvent.click(await screen.findByLabelText('End session 101'));
    expect(mockEndImpersonation).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => {
      expect(mockEndImpersonation).toHaveBeenCalledWith('101');
      expect(mockShowToast).toHaveBeenCalledWith('success', 'Impersonation session 101 ended');
      expect(mockListImpersonations).toHaveBeenCalledTimes(2);
    });
  });

  it('ends a session with a bare status result', async () => {
    mockEndImpersonation.mockResolvedValue({ status: 'impersonation_ended' });
    render(ImpersonationView);
    await fireEvent.click(await screen.findByLabelText('End session 101'));
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => {
      expect(mockEndImpersonation).toHaveBeenCalledWith('101');
      expect(mockShowToast).toHaveBeenCalledWith('success', 'Impersonation session 101 ended');
      expect(mockListImpersonations).toHaveBeenCalledTimes(2);
    });
  });

  it('looks up the sessions of one user', async () => {
    render(ImpersonationView);
    await fireEvent.input(screen.getByLabelText('Lookup user ID'), { target: { value: '42' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Look up' }));
    await waitFor(() => {
      expect(mockListUserImpersonations).toHaveBeenCalledWith('42');
      expect(screen.getByText('ticket-880')).toBeInTheDocument();
    });
  });

  it('shows the error state when the SDK fails', async () => {
    mockListImpersonations.mockRejectedValue(new Error('forbidden'));
    render(ImpersonationView);
    await waitFor(() => {
      expect(screen.getByText('forbidden')).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
    });
  });
});
