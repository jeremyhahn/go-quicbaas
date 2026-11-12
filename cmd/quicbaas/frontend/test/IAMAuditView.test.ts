/**
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
import type { IAMAuditEvent, IAMTenant } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockTenants: IAMTenant[] = [
  { id: 'tenant-a', name: 'Acme', description: '', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
  { id: 'tenant-b', name: 'Beta', description: '', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
];

const loginEvent: IAMAuditEvent = {
  id: 'evt-1',
  tenant_id: 'tenant-a',
  timestamp: '2026-11-01T10:00:00Z',
  event_type: 'user.login',
  category: 'authentication',
  actor: { user_id: 'u-1', username: 'alice' },
  resource: { type: 'session', id: 's-1' },
  action: 'login',
  outcome: 'success',
};

const impersonatedEvent: IAMAuditEvent = {
  id: 'evt-2',
  tenant_id: 'tenant-a',
  timestamp: '2026-11-02T10:00:00Z',
  event_type: 'role.assign',
  category: 'authorization',
  actor: { user_id: 'u-2', username: 'bob', impersonating_user_id: 'u-9', impersonating_username: 'root-admin' },
  resource: { type: 'role', id: 'r-1', name: 'editor' },
  action: 'assign',
  outcome: 'denied',
  details: { reason: 'missing permission' },
  correlation_id: 'corr-42',
};

const {
  mockListTenants,
  mockListAuditEvents,
  mockGetAuditEvent,
  mockDeleteAuditEventsBefore,
  mockGetSDKClient,
  mockShowToast,
} = vi.hoisted(() => ({
  mockListTenants: vi.fn(),
  mockListAuditEvents: vi.fn(),
  mockGetAuditEvent: vi.fn(),
  mockDeleteAuditEventsBefore: vi.fn(),
  mockGetSDKClient: vi.fn(),
  mockShowToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: mockShowToast,
}));

import IAMAuditView from '../src/views/security/IAMAuditView.svelte';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

function lastListOptions(): Record<string, unknown> {
  const calls = mockListAuditEvents.mock.calls;
  return calls[calls.length - 1]?.[0] ?? {};
}

describe('IAMAuditView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListTenants.mockResolvedValue({ tenants: mockTenants });
    mockListAuditEvents.mockResolvedValue({
      events: [loginEvent, impersonatedEvent],
      total: 120,
      limit: 50,
      offset: 0,
    });
    mockGetAuditEvent.mockResolvedValue(impersonatedEvent);
    mockDeleteAuditEventsBefore.mockResolvedValue({ deleted_count: 7, before: '2026-10-01T00:00:00Z' });
    mockGetSDKClient.mockResolvedValue({
      iam: {
        listTenants: mockListTenants,
        listAuditEvents: mockListAuditEvents,
        getAuditEvent: mockGetAuditEvent,
        deleteAuditEventsBefore: mockDeleteAuditEventsBefore,
      },
    });
  });

  it('lists the selected tenant’s events', async () => {
    render(IAMAuditView);
    await waitFor(() => {
      expect(screen.getByText('user.login')).toBeInTheDocument();
      expect(screen.getByText('alice')).toBeInTheDocument();
      expect(screen.getByText('session/s-1')).toBeInTheDocument();
    });
    expect(lastListOptions()).toMatchObject({ tenantID: 'tenant-a', limit: 50, offset: 0 });
  });

  it('reloads when the tenant changes', async () => {
    render(IAMAuditView);
    await waitFor(() => screen.getByText('user.login'));

    await fireEvent.change(screen.getByLabelText('Tenant'), { target: { value: 'tenant-b' } });
    await waitFor(() => {
      expect(lastListOptions()).toMatchObject({ tenantID: 'tenant-b', offset: 0 });
    });
  });

  it('Next sends offset 50 and Apply sends filters from the first page', async () => {
    render(IAMAuditView);
    await waitFor(() => screen.getByText('user.login'));

    await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    await waitFor(() => expect(lastListOptions()).toMatchObject({ offset: 50 }));
    await waitFor(() => screen.getByText('user.login'));

    await fireEvent.input(screen.getByLabelText('Event type'), { target: { value: 'user.login' } });
    await fireEvent.input(screen.getByLabelText('Category'), { target: { value: 'authentication' } });
    await fireEvent.change(screen.getByLabelText('Outcome'), { target: { value: 'failure' } });
    await fireEvent.input(screen.getByLabelText('Actor ID'), { target: { value: 'u-1' } });
    await fireEvent.input(screen.getByLabelText('Resource ID'), { target: { value: 's-1' } });
    await fireEvent.input(screen.getByLabelText('From'), { target: { value: '2026-11-01T08:30' } });
    await fireEvent.input(screen.getByLabelText('To'), { target: { value: '2026-11-02T09:00' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => {
      expect(lastListOptions()).toEqual({
        tenantID: 'tenant-a',
        limit: 50,
        offset: 0,
        eventType: 'user.login',
        category: 'authentication',
        outcome: 'failure',
        actorID: 'u-1',
        resourceID: 's-1',
        startTime: new Date('2026-11-01T08:30').toISOString(),
        endTime: new Date('2026-11-02T09:00').toISOString(),
      });
    });
    expect(String(lastListOptions().startTime)).toMatch(/Z$/);
  });

  it('a row click shows getAuditEvent’s detail', async () => {
    render(IAMAuditView);
    await waitFor(() => screen.getByText('role.assign'));

    await fireEvent.click(screen.getByText('role.assign'));
    await waitFor(() => {
      expect(mockGetAuditEvent).toHaveBeenCalledWith('evt-2', 'tenant-a');
      const dialog = screen.getByRole('dialog', { name: 'Audit event detail' });
      expect(dialog).toHaveTextContent('Impersonated by root-admin');
      expect(dialog).toHaveTextContent('corr-42');
      expect(dialog).toHaveTextContent('editor');
    });
    expect(screen.getByLabelText('Event details').textContent).toContain('"reason": "missing permission"');
  });

  it('delete-before is confirmed, sends the RFC 3339 date and reloads', async () => {
    render(IAMAuditView);
    await waitFor(() => screen.getByText('user.login'));
    const before = mockListAuditEvents.mock.calls.length;

    await fireEvent.input(screen.getByLabelText('Delete before date'), { target: { value: '2026-10-01' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Delete events before' }));

    const dialog = await screen.findByRole('dialog', { name: 'Delete confirmation' });
    expect(dialog).toHaveTextContent('2026-10-01');
    expect(dialog).toHaveTextContent('Acme');
    expect(mockDeleteAuditEventsBefore).not.toHaveBeenCalled();

    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await waitFor(() => {
      expect(mockDeleteAuditEventsBefore).toHaveBeenCalledWith('2026-10-01T00:00:00Z', 'tenant-a');
      expect(mockShowToast).toHaveBeenCalledWith('success', 'Deleted 7 audit events');
      expect(mockListAuditEvents.mock.calls.length).toBeGreaterThan(before);
    });
  });

  it('an SDK error shows the error state', async () => {
    mockListAuditEvents.mockRejectedValue(new Error('Audit store unavailable'));
    render(IAMAuditView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Audit store unavailable');
    });
  });
});
