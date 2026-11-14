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
import type { IAMApplication } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockTenants = [
  { id: 't1', name: 'Tenant One' },
  { id: 't2', name: 'Tenant Two' },
];

const webApp: IAMApplication = {
  client_id: 'client-web',
  name: 'web-portal',
  tenant_id: 't1',
  redirect_uris: ['https://web.example.com/cb'],
  grant_types: ['authorization_code', 'refresh_token'],
  response_types: ['code'],
  created: '2026-11-01T10:00:00Z',
  updated: '2026-11-02T10:00:00Z',
};

const batchApp: IAMApplication = {
  client_id: 'client-batch',
  name: 'batch-job',
  tenant_id: 't2',
  redirect_uris: [],
  grant_types: ['client_credentials'],
  response_types: [],
  created: '2026-11-01T10:00:00Z',
  updated: '2026-11-03T10:00:00Z',
};

const {
  mockListTenants,
  mockListApps,
  mockRegisterApp,
  mockUpdateApp,
  mockDeleteApp,
  mockRotateAppSecret,
  mockGetSDKClient,
} = vi.hoisted(() => ({
  mockListTenants: vi.fn(),
  mockListApps: vi.fn(),
  mockRegisterApp: vi.fn(),
  mockUpdateApp: vi.fn(),
  mockDeleteApp: vi.fn(),
  mockRotateAppSecret: vi.fn(),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import OAuthAppsView from '../src/views/security/OAuthAppsView.svelte';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('OAuthAppsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListTenants.mockResolvedValue({ tenants: mockTenants });
    mockListApps.mockImplementation((tenantID: string) =>
      Promise.resolve(
        tenantID === 't1'
          ? { applications: [webApp], limit: 50, offset: 0 }
          : { apps: [batchApp], limit: 50, offset: 0 },
      ),
    );
    mockRegisterApp.mockResolvedValue({ ...webApp, client_id: 'client-new', name: 'new-app', client_secret: 's3cr3t-new' });
    mockUpdateApp.mockResolvedValue(webApp);
    mockDeleteApp.mockResolvedValue(undefined);
    mockRotateAppSecret.mockResolvedValue({ ...webApp, client_secret: 's3cr3t-rotated' });
    mockGetSDKClient.mockResolvedValue({
      iam: {
        listTenants: mockListTenants,
        listApps: mockListApps,
        registerApp: mockRegisterApp,
        updateApp: mockUpdateApp,
        deleteApp: mockDeleteApp,
        rotateAppSecret: mockRotateAppSecret,
      },
    });
  });

  it('lists the applications of the selected tenant', async () => {
    render(OAuthAppsView);
    await waitFor(() => {
      expect(screen.getByText('web-portal')).toBeInTheDocument();
    });
    expect(mockListApps).toHaveBeenCalledWith('t1');
    expect(screen.getByText('client-web')).toBeInTheDocument();
    expect(screen.getByText('authorization_code, refresh_token')).toBeInTheDocument();
  });

  it('reloads when the tenant changes, reading apps as a fallback', async () => {
    render(OAuthAppsView);
    await waitFor(() => screen.getByText('web-portal'));
    await fireEvent.change(screen.getByTestId('oauth-apps-tenant-select'), { target: { value: 't2' } });
    await waitFor(() => {
      expect(mockListApps).toHaveBeenCalledWith('t2');
      expect(screen.getByText('batch-job')).toBeInTheDocument();
    });
  });

  it('registers an application and shows its secret once', async () => {
    render(OAuthAppsView);
    await waitFor(() => screen.getByText('web-portal'));
    const before = mockListApps.mock.calls.length;

    await fireEvent.input(screen.getByLabelText('Application name'), { target: { value: 'new-app' } });
    await fireEvent.input(screen.getByLabelText('Redirect URIs'), {
      target: { value: 'https://a.example.com/cb\n\n https://b.example.com/cb ' },
    });
    await fireEvent.click(screen.getByLabelText('client_credentials'));
    await fireEvent.change(screen.getByLabelText('Token endpoint auth method'), {
      target: { value: 'private_key_jwt' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /Register Application/i }));

    await waitFor(() => {
      expect(mockRegisterApp).toHaveBeenCalledWith('t1', {
        name: 'new-app',
        redirect_uris: ['https://a.example.com/cb', 'https://b.example.com/cb'],
        grant_types: ['authorization_code', 'client_credentials'],
        pkce_required: true,
        token_endpoint_auth_method: 'private_key_jwt',
      });
    });
    const dialog = await screen.findByRole('dialog', { name: /Client secret/i });
    expect(dialog).toHaveTextContent('s3cr3t-new');
    expect(dialog).toHaveTextContent(/cannot be shown again/i);
    expect(mockListApps.mock.calls.length).toBeGreaterThan(before);

    await fireEvent.click(screen.getByRole('button', { name: /^Done$/ }));
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: /Client secret/i })).not.toBeInTheDocument();
    });
    expect(screen.queryByText('s3cr3t-new')).not.toBeInTheDocument();
  });

  it('edits an application sending only the changed fields', async () => {
    render(OAuthAppsView);
    await waitFor(() => screen.getByText('web-portal'));

    await fireEvent.click(screen.getByRole('button', { name: /Edit web-portal/i }));
    expect(screen.getByRole('dialog', { name: /Edit application/i })).toBeInTheDocument();
    await fireEvent.input(screen.getByLabelText('Edit name'), { target: { value: 'web-portal-2' } });
    await fireEvent.click(screen.getByRole('button', { name: /^Save$/ }));

    await waitFor(() => {
      expect(mockUpdateApp).toHaveBeenCalledWith('t1', 'client-web', { name: 'web-portal-2' });
    });
  });

  it('rotates the secret after the confirmation and shows it', async () => {
    render(OAuthAppsView);
    await waitFor(() => screen.getByText('web-portal'));
    const before = mockListApps.mock.calls.length;

    await fireEvent.click(screen.getByRole('button', { name: /Rotate secret of web-portal/i }));
    expect(screen.getByRole('dialog', { name: /Confirmation/i })).toBeInTheDocument();
    expect(mockRotateAppSecret).not.toHaveBeenCalled();

    await fireEvent.click(screen.getByRole('button', { name: /^Rotate$/ }));
    await waitFor(() => {
      expect(mockRotateAppSecret).toHaveBeenCalledWith('t1', 'client-web');
      expect(mockListApps.mock.calls.length).toBeGreaterThan(before);
    });
    const dialog = await screen.findByRole('dialog', { name: /Client secret/i });
    expect(dialog).toHaveTextContent('s3cr3t-rotated');
  });

  it('deletes an application after the confirmation and reloads', async () => {
    render(OAuthAppsView);
    await waitFor(() => screen.getByText('web-portal'));
    const before = mockListApps.mock.calls.length;

    await fireEvent.click(screen.getByRole('button', { name: /^Delete web-portal$/i }));
    expect(screen.getByRole('dialog', { name: /Confirmation/i })).toBeInTheDocument();
    expect(mockDeleteApp).not.toHaveBeenCalled();

    await fireEvent.click(screen.getByRole('button', { name: /^Delete$/ }));
    await waitFor(() => {
      expect(mockDeleteApp).toHaveBeenCalledWith('t1', 'client-web');
      expect(mockListApps.mock.calls.length).toBeGreaterThan(before);
    });
  });

  it('shows a load error', async () => {
    mockListApps.mockRejectedValue(new Error('IAM unavailable'));
    render(OAuthAppsView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('IAM unavailable');
    });
  });
});
