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

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { IAMExternalProvider, IAMTenant } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockTenants = [
  { id: 't1', name: 'Acme' },
  { id: 't2', name: 'Globex' },
] as unknown as IAMTenant[];

const mockProviders = [
  {
    id: 'p1',
    name: 'okta',
    type: 'oidc',
    enabled: true,
    client_id: 'okta-client',
    client_secret: '',
    issuer_url: 'https://okta.example.com',
    scopes: ['openid', 'email'],
    updated: '2026-10-01T00:00:00Z',
  },
  {
    id: 'p2',
    name: 'corp-ldap',
    type: 'ldap',
    enabled: false,
    client_id: 'ldap-client',
    client_secret: '',
    updated: '2026-10-02T00:00:00Z',
  },
] as unknown as IAMExternalProvider[];

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const {
  mockListTenants,
  mockListProviders,
  mockRegisterProvider,
  mockUpdateProvider,
  mockDeleteProvider,
  mockGetSDKClient,
} = vi.hoisted(() => ({
  mockListTenants: vi.fn(),
  mockListProviders: vi.fn(),
  mockRegisterProvider: vi.fn(),
  mockUpdateProvider: vi.fn(),
  mockDeleteProvider: vi.fn(),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import IdentityProvidersView from '../src/views/security/IdentityProvidersView.svelte';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('IdentityProvidersView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListTenants.mockResolvedValue({ tenants: mockTenants });
    mockListProviders.mockResolvedValue({ providers: mockProviders });
    mockRegisterProvider.mockResolvedValue(mockProviders[0]);
    mockUpdateProvider.mockResolvedValue(mockProviders[0]);
    mockDeleteProvider.mockResolvedValue(undefined);
    mockGetSDKClient.mockResolvedValue({
      iam: {
        listTenants: mockListTenants,
        listProviders: mockListProviders,
        registerProvider: mockRegisterProvider,
        updateProvider: mockUpdateProvider,
        deleteProvider: mockDeleteProvider,
      },
    });
  });

  it('lists the providers of the selected tenant', async () => {
    render(IdentityProvidersView);
    await waitFor(() => {
      expect(screen.getByText('okta')).toBeInTheDocument();
      expect(screen.getByText('corp-ldap')).toBeInTheDocument();
    });
    expect(mockListProviders).toHaveBeenCalledWith('t1', { type: undefined });

    await fireEvent.change(screen.getByLabelText('Tenant'), { target: { value: 't2' } });
    await waitFor(() => {
      expect(mockListProviders).toHaveBeenCalledWith('t2', { type: undefined });
    });
  });

  it('filters the providers by type', async () => {
    render(IdentityProvidersView);
    await waitFor(() => screen.getByText('okta'));

    await fireEvent.change(screen.getByLabelText('Provider type filter'), { target: { value: 'saml' } });
    await waitFor(() => {
      expect(mockListProviders).toHaveBeenCalledWith('t1', { type: 'saml' });
    });
  });

  it('registers a provider', async () => {
    render(IdentityProvidersView);
    await waitFor(() => screen.getByText('okta'));
    const before = mockListProviders.mock.calls.length;

    await fireEvent.input(screen.getByLabelText('Provider name'), { target: { value: 'google' } });
    await fireEvent.input(screen.getByLabelText('Provider client ID'), { target: { value: 'g-client' } });
    await fireEvent.click(screen.getByRole('button', { name: /Register Provider/i }));

    await waitFor(() => {
      expect(mockRegisterProvider).toHaveBeenCalledWith('t1', {
        name: 'google',
        type: 'oidc',
        enabled: true,
        client_id: 'g-client',
        client_secret: '',
      });
      expect(mockListProviders.mock.calls.length).toBeGreaterThan(before);
    });
  });

  it('sends only the changed fields on edit and leaves out an empty client secret', async () => {
    render(IdentityProvidersView);
    await waitFor(() => screen.getByText('okta'));

    await fireEvent.click(screen.getByRole('button', { name: 'Edit provider okta' }));
    expect(screen.getByRole('dialog', { name: /Edit identity provider/i })).toBeInTheDocument();
    expect(screen.getByLabelText('Edit client secret')).toHaveValue('');

    await fireEvent.input(screen.getByLabelText('Edit name'), { target: { value: 'okta-prod' } });
    await fireEvent.click(screen.getByRole('button', { name: /^Save$/ }));

    await waitFor(() => {
      expect(mockUpdateProvider).toHaveBeenCalledWith('t1', 'p1', { name: 'okta-prod' });
    });
  });

  it('sends a new client secret on edit when one is typed', async () => {
    render(IdentityProvidersView);
    await waitFor(() => screen.getByText('okta'));

    await fireEvent.click(screen.getByRole('button', { name: 'Edit provider okta' }));
    await fireEvent.input(screen.getByLabelText('Edit client secret'), { target: { value: 's3cret' } });
    await fireEvent.click(screen.getByRole('button', { name: /^Save$/ }));

    await waitFor(() => {
      expect(mockUpdateProvider).toHaveBeenCalledWith('t1', 'p1', { client_secret: 's3cret' });
    });
  });

  it('toggles a provider with { enabled }', async () => {
    render(IdentityProvidersView);
    await waitFor(() => screen.getByText('corp-ldap'));

    await fireEvent.click(screen.getByLabelText('Enabled corp-ldap'));
    await waitFor(() => {
      expect(mockUpdateProvider).toHaveBeenCalledWith('t1', 'p2', { enabled: true });
    });
  });

  it('deletes a provider after the confirmation and reloads', async () => {
    render(IdentityProvidersView);
    await waitFor(() => screen.getByText('okta'));
    const before = mockListProviders.mock.calls.length;

    await fireEvent.click(screen.getByRole('button', { name: 'Delete provider okta' }));
    expect(screen.getByRole('dialog', { name: /Delete confirmation/i })).toBeInTheDocument();
    expect(mockDeleteProvider).not.toHaveBeenCalled();

    await fireEvent.click(screen.getByRole('button', { name: /^Delete$/ }));
    await waitFor(() => {
      expect(mockDeleteProvider).toHaveBeenCalledWith('t1', 'p1');
      expect(mockListProviders.mock.calls.length).toBeGreaterThan(before);
    });
  });

  it('shows a load error', async () => {
    mockListProviders.mockRejectedValue(new Error('IAM unavailable'));
    render(IdentityProvidersView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('IAM unavailable');
    });
  });
});
