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
 * After a registration that signs the user in, RegisterView marks the daemon
 * the UI talks to as connected: the saved override's daemon when there is one,
 * the page's own daemon otherwise. Registration itself stays on the page
 * daemon through getOriginSDKClient.
 */

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';

const {
  mockSetConnected,
  mockSetAuthenticated,
  mockRegisterBegin,
  mockRegisterFinish,
  mockActiveConnection,
  mockCredentialsCreate,
} = vi.hoisted(() => ({
  mockSetConnected: vi.fn(),
  mockSetAuthenticated: vi.fn(),
  mockRegisterBegin: vi.fn(),
  mockRegisterFinish: vi.fn(),
  mockActiveConnection: vi.fn(),
  mockCredentialsCreate: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeLogin: vi.fn(),
  routeDashboard: vi.fn(),
}));
vi.mock('../src/stores/connection.svelte.ts', () => ({
  setConnected: mockSetConnected,
}));
vi.mock('../src/stores/auth.svelte.ts', () => ({
  setAuthenticated: mockSetAuthenticated,
}));
vi.mock('../src/lib/webauthn.ts', () => ({
  parseCreationOptions: vi.fn(() => ({})),
  encodeAttestationResponse: vi.fn(() => ({ id: 'cred-1' })),
}));
vi.mock('../src/stores/sdkClient.ts', () => ({
  getOriginSDKClient: vi.fn(async () => ({
    auth: { registerBegin: mockRegisterBegin, registerFinish: mockRegisterFinish },
  })),
  activeConnection: mockActiveConnection,
}));

import RegisterView from '../src/views/orchestrator/RegisterView.svelte';

const REMOTE = 'https://remote.example:8443';

async function register(): Promise<void> {
  render(RegisterView);
  await fireEvent.input(screen.getByLabelText('Organization name'), { target: { value: 'Acme' } });
  await fireEvent.input(screen.getByLabelText('Display name'), { target: { value: 'Jane' } });
  const emailInput = screen.getByLabelText('Email address');
  await fireEvent.input(emailInput, { target: { value: 'jane@example.com' } });
  await fireEvent.keyDown(emailInput, { key: 'Enter' });
}

beforeEach(() => {
  vi.clearAllMocks();
  mockRegisterBegin.mockResolvedValue({
    session_id: 'sess-1',
    public_key_credential_creation_options: { publicKey: {} },
  });
  mockRegisterFinish.mockResolvedValue({
    status: 'active',
    access_token: 'tok-access',
    refresh_token: 'tok-refresh',
  });
  mockCredentialsCreate.mockResolvedValue({ id: 'cred-1' });
  Object.defineProperty(navigator, 'credentials', {
    configurable: true,
    value: { create: mockCredentialsCreate },
  });
});

describe('RegisterView connection state after registration', () => {
  it("marks the saved override's daemon connected", async () => {
    mockActiveConnection.mockReturnValue({ transport: 'rest', address: REMOTE, overridden: true });

    await register();

    await waitFor(() => expect(mockSetConnected).toHaveBeenCalledWith(REMOTE, 'rest'));
    expect(mockSetConnected).not.toHaveBeenCalledWith(window.location.origin, 'rest');
    expect(mockRegisterBegin).toHaveBeenCalledTimes(1);
    expect(mockSetAuthenticated).toHaveBeenCalledWith({
      access_token: 'tok-access',
      refresh_token: 'tok-refresh',
    });
  });

  it("marks the page's daemon connected without an override", async () => {
    mockActiveConnection.mockReturnValue({
      transport: 'rest',
      address: window.location.origin,
      overridden: false,
    });

    await register();

    await waitFor(() =>
      expect(mockSetConnected).toHaveBeenCalledWith(window.location.origin, 'rest'),
    );
  });
});
