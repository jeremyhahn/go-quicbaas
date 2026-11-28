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
import { flushSync } from 'svelte';
import { APIError, TransportNotSupportedError } from '@quicbaas/sdk';
import { setUnauthenticated } from '../src/stores/auth.svelte.ts';

const { mockGetSDKClient, mockShowToast } = vi.hoisted(() => ({
  mockGetSDKClient: vi.fn(),
  mockShowToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: mockShowToast,
}));

import XKMSFIDO2Panel from '../src/components/security/XKMSFIDO2Panel.svelte';

// ---------------------------------------------------------------------------
// navigator.credentials mock
// ---------------------------------------------------------------------------

const mockCreate = vi.fn();
const mockGet = vi.fn();
const baseNavigator = window.navigator;

function setCredentials(present: boolean): void {
  Object.defineProperty(window, 'navigator', {
    writable: true,
    configurable: true,
    value: present
      ? { ...baseNavigator, credentials: { create: mockCreate, get: mockGet } }
      : { ...baseNavigator, credentials: undefined },
  });
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const enc = new TextEncoder();
const dec = new TextDecoder();

/** base64url without padding of a UTF-8 string. */
function b64url(text: string): string {
  return btoa(text).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

const CHALLENGE = 'fido2-challenge';
const TOKEN = 'secret-token-value-123';

const CREATION_OPTIONS = JSON.stringify({
  publicKey: {
    challenge: b64url(CHALLENGE),
    rp: { id: 'localhost', name: 'quicbaas' },
    user: { id: b64url('user-handle'), name: 'alice@example.com', displayName: 'Alice' },
    pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
  },
});

const REQUEST_OPTIONS = JSON.stringify({
  publicKey: {
    challenge: b64url(CHALLENGE),
    allowCredentials: [{ id: b64url('cred-1'), type: 'public-key' }],
  },
});

function attestation(): PublicKeyCredential {
  return {
    id: 'cred-1',
    rawId: enc.encode('cred-1'),
    type: 'public-key',
    response: {
      clientDataJSON: enc.encode('clientData'),
      attestationObject: enc.encode('attObj'),
      getTransports: () => ['usb'],
    },
    getClientExtensionResults: () => ({}),
    authenticatorAttachment: null,
  } as unknown as PublicKeyCredential;
}

function assertion(): PublicKeyCredential {
  return {
    id: 'cred-1',
    rawId: enc.encode('cred-1'),
    type: 'public-key',
    response: {
      clientDataJSON: enc.encode('clientData'),
      authenticatorData: enc.encode('authData'),
      signature: enc.encode('sig'),
      userHandle: null,
    },
    getClientExtensionResults: () => ({}),
    authenticatorAttachment: null,
  } as unknown as PublicKeyCredential;
}

type Fn = ReturnType<typeof vi.fn>;
let fido2: Record<string, Fn>;

function newFIDO2(): Record<string, Fn> {
  return {
    beginAddCredential: vi.fn().mockResolvedValue({ session_id: 'add-sess', options_json: CREATION_OPTIONS }),
    beginRegistration: vi.fn().mockResolvedValue({ session_id: 'reg-sess', options_json: CREATION_OPTIONS }),
    finishRegistration: vi.fn().mockResolvedValue({ success: true, credential_id: 'cred-1', message: 'registered' }),
    beginAuthentication: vi.fn().mockResolvedValue({ session_id: 'auth-sess', options_json: REQUEST_OPTIONS }),
    finishAuthentication: vi.fn().mockResolvedValue({ success: true, username: 'alice', message: 'verified' }),
  };
}

function toastText(): string {
  return JSON.stringify(mockShowToast.mock.calls);
}

function input(label: string, value: string): Promise<boolean> {
  return fireEvent.input(screen.getByLabelText(label), { target: { value } });
}

function click(name: string): Promise<boolean> {
  return fireEvent.click(screen.getByRole('button', { name }));
}

async function rendered(): Promise<void> {
  render(XKMSFIDO2Panel);
  await waitFor(() => expect(mockGetSDKClient).toHaveBeenCalled());
}

function noCalls(): void {
  for (const fn of Object.values(fido2)) expect(fn).not.toHaveBeenCalled();
}

describe('XKMSFIDO2Panel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setCredentials(true);
    mockCreate.mockResolvedValue(attestation());
    mockGet.mockResolvedValue(assertion());
    fido2 = newFIDO2();
    mockGetSDKClient.mockResolvedValue({ xkmsFido2: fido2 });
  });

  it('calls nothing on mount and renders the three actions', async () => {
    await rendered();
    noCalls();
    expect(mockCreate).not.toHaveBeenCalled();
    expect(mockGet).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: 'Add a passkey to my account' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Register passkey' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Test passkey' })).toBeInTheDocument();
  });

  it('adds a passkey to the caller: begin, create with the decoded challenge, finish', async () => {
    await rendered();
    await click('Add a passkey to my account');
    const result = await screen.findByTestId('fido2-add-result');
    expect(fido2.beginAddCredential).toHaveBeenCalledWith();
    expect(mockCreate).toHaveBeenCalledTimes(1);
    const arg = mockCreate.mock.calls[0][0] as { publicKey: PublicKeyCredentialCreationOptions };
    expect(dec.decode(arg.publicKey.challenge as ArrayBuffer)).toBe(CHALLENGE);
    expect(dec.decode(arg.publicKey.user.id as ArrayBuffer)).toBe('user-handle');
    expect(fido2.finishRegistration).toHaveBeenCalledTimes(1);
    const [req, tenant] = fido2.finishRegistration.mock.calls[0];
    expect(tenant).toBeUndefined();
    expect(req.session_id).toBe('add-sess');
    expect(JSON.parse(req.attestation_json)).toEqual({
      id: 'cred-1',
      rawId: b64url('cred-1'),
      type: 'public-key',
      response: {
        clientDataJSON: b64url('clientData'),
        attestationObject: b64url('attObj'),
        transports: ['usb'],
      },
      clientExtensionResults: {},
    });
    expect(result).toHaveTextContent('Yes');
    expect(result).toHaveTextContent('cred-1');
  });

  it('registers a passkey for a user with the email and display name and no tenant', async () => {
    await rendered();
    await input('Register email', 'alice@example.com');
    await input('Display name', 'Alice');
    await click('Register passkey');
    const result = await screen.findByTestId('fido2-register-result');
    expect(fido2.beginRegistration).toHaveBeenCalledWith({ email: 'alice@example.com', display_name: 'Alice' });
    expect(fido2.beginRegistration.mock.calls[0]).toHaveLength(1);
    expect(mockCreate).toHaveBeenCalledTimes(1);
    const [req] = fido2.finishRegistration.mock.calls[0];
    expect(req.session_id).toBe('reg-sess');
    expect(JSON.parse(req.attestation_json).response.attestationObject).toBe(b64url('attObj'));
    expect(result).toHaveTextContent('Yes');
    expect(result).toHaveTextContent('cred-1');
    expect(result).toHaveTextContent('registered');
  });

  it('refuses an empty email or display name without a call', async () => {
    await rendered();
    await click('Register passkey');
    expect(await screen.findByText('Email is required.')).toBeInTheDocument();
    await input('Register email', 'alice@example.com');
    await click('Register passkey');
    expect(await screen.findByText('Display name is required.')).toBeInTheDocument();
    await click('Test passkey');
    expect(await screen.findAllByText('Email is required.')).toHaveLength(1);
    noCalls();
    expect(mockCreate).not.toHaveBeenCalled();
  });

  it('tests a passkey: begin with the email, get, finish with session, email and assertion', async () => {
    await rendered();
    await input('Test email', 'alice@example.com');
    await click('Test passkey');
    const result = await screen.findByTestId('fido2-auth-result');
    expect(fido2.beginAuthentication).toHaveBeenCalledWith({ email: 'alice@example.com' });
    expect(mockGet).toHaveBeenCalledTimes(1);
    const arg = mockGet.mock.calls[0][0] as { publicKey: PublicKeyCredentialRequestOptions };
    expect(dec.decode(arg.publicKey.challenge as ArrayBuffer)).toBe(CHALLENGE);
    const [req] = fido2.finishAuthentication.mock.calls[0];
    expect(req.session_id).toBe('auth-sess');
    expect(req.email).toBe('alice@example.com');
    expect(JSON.parse(req.assertion_json)).toEqual({
      id: 'cred-1',
      rawId: b64url('cred-1'),
      type: 'public-key',
      response: {
        clientDataJSON: b64url('clientData'),
        authenticatorData: b64url('authData'),
        signature: b64url('sig'),
      },
      clientExtensionResults: {},
    });
    expect(result).toHaveTextContent('Yes');
    expect(result).toHaveTextContent('alice');
    expect(result).toHaveTextContent('verified');
  });

  it('never shows or toasts a token from a finish answer, and says one was issued', async () => {
    fido2.finishRegistration.mockResolvedValue({ success: true, credential_id: 'cred-1', token: TOKEN });
    fido2.finishAuthentication.mockResolvedValue({ success: true, username: 'alice', token: TOKEN });
    await rendered();
    await click('Add a passkey to my account');
    const add = await screen.findByTestId('fido2-add-result');
    expect(add).toHaveTextContent('A token was issued');
    await input('Test email', 'alice@example.com');
    await click('Test passkey');
    const auth = await screen.findByTestId('fido2-auth-result');
    expect(auth).toHaveTextContent('A token was issued');
    expect(document.body.textContent).not.toContain(TOKEN);
    expect(document.body.innerHTML).not.toContain(TOKEN);
    expect(toastText()).not.toContain(TOKEN);
  });

  it('shows an inline message and makes no finish call when create or get is cancelled or null', async () => {
    mockCreate.mockRejectedValueOnce(new DOMException('Operation not allowed', 'NotAllowedError'));
    mockCreate.mockResolvedValueOnce(null);
    mockGet.mockResolvedValueOnce(null);
    await rendered();
    await click('Add a passkey to my account');
    expect(await screen.findByText(/cancelled or not allowed/)).toBeInTheDocument();
    await input('Register email', 'alice@example.com');
    await input('Display name', 'Alice');
    await click('Register passkey');
    expect(await screen.findByText(/returned no passkey/)).toBeInTheDocument();
    await input('Test email', 'alice@example.com');
    await click('Test passkey');
    await waitFor(() => expect(screen.getAllByText(/returned no passkey/)).toHaveLength(2));
    expect(fido2.finishRegistration).not.toHaveBeenCalled();
    expect(fido2.finishAuthentication).not.toHaveBeenCalled();
  });

  it('shows an inline error and makes no browser call for options that are not JSON or have no challenge', async () => {
    fido2.beginAddCredential.mockResolvedValue({ session_id: 's', options_json: 'not json' });
    fido2.beginAuthentication.mockResolvedValue({ session_id: 's', options_json: JSON.stringify({ publicKey: {} }) });
    await rendered();
    await click('Add a passkey to my account');
    expect(await screen.findByText(/options are not JSON/)).toBeInTheDocument();
    await input('Test email', 'alice@example.com');
    await click('Test passkey');
    expect(await screen.findByText(/challenge missing/)).toBeInTheDocument();
    expect(mockCreate).not.toHaveBeenCalled();
    expect(mockGet).not.toHaveBeenCalled();
    expect(fido2.finishRegistration).not.toHaveBeenCalled();
    expect(fido2.finishAuthentication).not.toHaveBeenCalled();
  });

  it('shows the unsupported-browser message and makes no call without navigator.credentials', async () => {
    setCredentials(false);
    render(XKMSFIDO2Panel);
    expect(await screen.findByText('Passkeys are not supported in this browser.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Add a passkey to my account' })).not.toBeInTheDocument();
    noCalls();
  });

  it('shows a 403 from each begin inline', async () => {
    fido2.beginAddCredential.mockRejectedValue(new APIError(403, 'add denied'));
    fido2.beginRegistration.mockRejectedValue(new APIError(403, 'register denied'));
    fido2.beginAuthentication.mockRejectedValue(new APIError(403, 'auth denied'));
    await rendered();
    await click('Add a passkey to my account');
    expect(await screen.findByText('add denied')).toBeInTheDocument();
    await input('Register email', 'alice@example.com');
    await input('Display name', 'Alice');
    await click('Register passkey');
    expect(await screen.findByText('register denied')).toBeInTheDocument();
    await input('Test email', 'alice@example.com');
    await click('Test passkey');
    expect(await screen.findByText('auth denied')).toBeInTheDocument();
    expect(mockCreate).not.toHaveBeenCalled();
    expect(mockGet).not.toHaveBeenCalled();
  });

  it('shows a 403 from each finish inline', async () => {
    fido2.finishRegistration
      .mockRejectedValueOnce(new APIError(403, 'add finish denied'))
      .mockRejectedValueOnce(new APIError(403, 'register finish denied'));
    fido2.finishAuthentication.mockRejectedValue(new APIError(403, 'auth finish denied'));
    await rendered();
    await click('Add a passkey to my account');
    expect(await screen.findByText('add finish denied')).toBeInTheDocument();
    await input('Register email', 'alice@example.com');
    await input('Display name', 'Alice');
    await click('Register passkey');
    expect(await screen.findByText('register finish denied')).toBeInTheDocument();
    await input('Test email', 'alice@example.com');
    await click('Test passkey');
    expect(await screen.findByText('auth finish denied')).toBeInTheDocument();
  });

  it('clears the results on a new session and drops a late answer from the old one', async () => {
    await rendered();
    await click('Add a passkey to my account');
    await screen.findByTestId('fido2-add-result');
    let release: (v: unknown) => void = () => {};
    fido2.finishAuthentication.mockReturnValueOnce(new Promise((r) => { release = r; }));
    await input('Test email', 'alice@example.com');
    await click('Test passkey');
    await waitFor(() => expect(fido2.finishAuthentication).toHaveBeenCalled());
    setUnauthenticated();
    flushSync();
    await waitFor(() => expect(screen.queryByTestId('fido2-add-result')).not.toBeInTheDocument());
    release({ success: true, username: 'late-user' });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByTestId('fido2-auth-result')).not.toBeInTheDocument();
    expect(screen.queryByText(/late-user/)).not.toBeInTheDocument();
  });

  it('shows the not-available state and makes no call without xkmsFido2', async () => {
    mockGetSDKClient.mockResolvedValue({ xkmsFido2: null });
    render(XKMSFIDO2Panel);
    expect(await screen.findByTestId('xkms-fido2-not-available')).toBeInTheDocument();
    noCalls();
  });

  it('shows the not-available state when the client throws TransportNotSupportedError', async () => {
    fido2.beginAddCredential.mockRejectedValue(new TransportNotSupportedError('wails', 'xkmsFido2', 'web only'));
    await rendered();
    await click('Add a passkey to my account');
    expect(await screen.findByTestId('xkms-fido2-not-available')).toBeInTheDocument();
    expect(mockCreate).not.toHaveBeenCalled();
  });
});
