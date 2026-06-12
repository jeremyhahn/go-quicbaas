/**
 * LoginView.test.ts
 *
 * Verifies that LoginView:
 *   - Renders FIDO2 + PIV options with no password input.
 *   - Passkey click shows email field; submitting calls the SDK begin/finish
 *     methods and then navigator.credentials.get with the decoded challenge.
 *   - Smartcard click calls the SDK PIV login method.
 *   - On a successful finish (200 with tokens), the tokens are persisted AND
 *     the shared auth status flips to 'authenticated' (so MainApp's route guard
 *     does NOT bounce the user back to /login), then the router redirects to
 *     /dashboard.
 *   - A finish failure surfaces a real error alert instead of a silent reset.
 *
 * The component talks to the server exclusively through the SDK client
 * (client.auth.*), so these tests mock getSDKClient — not raw fetch. The
 * shared auth store is mocked so the status-flip assertion is deterministic
 * and isolated from localStorage timing.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const {
  mockRouteDashboard,
  mockSetConnected,
  mockSetAuthenticated,
  mockWebAuthnBeginLogin,
  mockWebAuthnFinishLogin,
  mockLoginPIV,
  mockListPublicTenants,
} = vi.hoisted(() => ({
  mockRouteDashboard: vi.fn(),
  mockSetConnected: vi.fn(),
  mockSetAuthenticated: vi.fn(),
  mockWebAuthnBeginLogin: vi.fn(),
  mockWebAuthnFinishLogin: vi.fn(),
  mockLoginPIV: vi.fn(),
  mockListPublicTenants: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeDashboard: mockRouteDashboard,
  navigateTo: vi.fn(),
}));

vi.mock('../src/stores/connection.svelte.ts', () => ({
  setConnected: mockSetConnected,
  setDisconnected: vi.fn(),
  setConnectionError: vi.fn(),
  connectionStore: { connected: false, address: '', transport: 'rest', error: '' },
}));

// The shared auth store is the unit under test for the bug fix: a successful
// finish must call setAuthenticated, which flips the in-tab status so the
// MainApp route guard leaves the user on the dashboard.
vi.mock('../src/stores/auth.svelte.ts', () => ({
  setAuthenticated: mockSetAuthenticated,
}));

// The SDK client is the sole transport. Begin/finish/PIV/listPublicTenants are
// the methods LoginView calls through client.auth.*.
vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(async () => ({
    auth: {
      webAuthnBeginLogin: mockWebAuthnBeginLogin,
      webAuthnFinishLogin: mockWebAuthnFinishLogin,
      loginPIV: mockLoginPIV,
      listPublicTenants: mockListPublicTenants,
    },
  })),
}));

// ---------------------------------------------------------------------------
// navigator.credentials mock
// ---------------------------------------------------------------------------

const mockCredentialsGet = vi.fn();

Object.defineProperty(window, 'navigator', {
  writable: true,
  configurable: true,
  value: {
    ...window.navigator,
    credentials: {
      get: mockCredentialsGet,
      create: vi.fn(),
    },
  },
});

// ---------------------------------------------------------------------------
// localStorage mock (reset between tests)
// ---------------------------------------------------------------------------

let localStore: Record<string, string> = {};
const localStorageMock = {
  getItem: (k: string) => localStore[k] ?? null,
  setItem: (k: string, v: string) => { localStore[k] = v; },
  removeItem: (k: string) => { delete localStore[k]; },
  clear: () => { localStore = {}; },
};
Object.defineProperty(window, 'localStorage', {
  writable: true,
  configurable: true,
  value: localStorageMock,
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a minimal PublicKeyCredential assertion stub. */
function makeCredentialStub(): PublicKeyCredential {
  const encoder = new TextEncoder();
  return {
    id: 'cred-id-stub',
    rawId: encoder.encode('cred-id-stub').buffer,
    type: 'public-key',
    response: {
      authenticatorData: encoder.encode('authData').buffer,
      clientDataJSON: encoder.encode('clientData').buffer,
      signature: encoder.encode('sig').buffer,
      userHandle: null,
    } as AuthenticatorAssertionResponse,
    getClientExtensionResults: () => ({}),
    authenticatorAttachment: null,
    toJSON: () => ({}),
  } as unknown as PublicKeyCredential;
}

/**
 * The EXACT flat shape the Go FinishLogin handler returns (see
 * pkg/api/rest/iam_webauthn.go FinishLogin): access_token / id_token /
 * refresh_token / token_type / expires_in. The SDK passes this through
 * verbatim as AuthTokenResponse.
 */
function makeFinishTokens(): Record<string, unknown> {
  return {
    access_token: 'tok-access',
    id_token: 'tok-id',
    refresh_token: 'tok-refresh',
    token_type: 'Bearer',
    expires_in: 3600,
  };
}

/** The go-webauthn assertion challenge envelope the begin endpoint returns. */
function makeChallengeEnvelope(): unknown {
  return { publicKey: { challenge: 'abc123', timeout: 60000 } };
}

/** Build an APIError-like rejection carrying an HTTP status. */
function makeStatusError(status: number, message = 'error'): Error & { status: number } {
  const err = new Error(message) as Error & { status: number };
  err.status = status;
  return err;
}

// ---------------------------------------------------------------------------
// Import the component AFTER mocks are in place.
// ---------------------------------------------------------------------------

import LoginView from '../src/views/orchestrator/LoginView.svelte';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('LoginView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStore = {};
    // Default: public tenants list resolves so the chooser settles.
    mockListPublicTenants.mockResolvedValue([
      { id: 'system', name: 'system', display_name: 'System' },
    ]);
  });

  afterEach(() => {
    cleanup();
  });

  /** Drive the passkey flow up to (and including) clicking Authenticate. */
  async function submitPasskey(email = 'alice@example.com'): Promise<void> {
    await fireEvent.click(screen.getByRole('button', { name: /sign in with passkey/i }));
    await waitFor(() => screen.getByLabelText(/email address/i));
    await fireEvent.input(screen.getByLabelText(/email address/i), {
      target: { value: email },
    });
    await fireEvent.click(screen.getByRole('button', { name: /authenticate with passkey/i }));
  }

  // --------------------------------------------------------------------------
  // 1. No password input — FIDO2 + PIV buttons present
  // --------------------------------------------------------------------------

  it('renders FIDO2 + PIV options, no password input', () => {
    render(LoginView);

    const passwordInputs = document.querySelectorAll('input[type="password"]');
    expect(passwordInputs.length).toBe(0);

    expect(screen.getByRole('button', { name: /sign in with passkey/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /sign in with smartcard/i })).toBeInTheDocument();

    expect(screen.getByText('QuicBaaS')).toBeInTheDocument();
    expect(screen.getByText('Hardware-Backed Sign-In')).toBeInTheDocument();
  });

  // --------------------------------------------------------------------------
  // 2. Passkey flow: click → email → begin → credentials.get → finish
  // --------------------------------------------------------------------------

  it('passkey click shows email field; begin, credentials.get, and finish run in order', async () => {
    mockWebAuthnBeginLogin.mockResolvedValueOnce(makeChallengeEnvelope());
    mockCredentialsGet.mockResolvedValueOnce(makeCredentialStub());
    mockWebAuthnFinishLogin.mockResolvedValueOnce(makeFinishTokens());

    render(LoginView);
    await submitPasskey('alice@example.com');

    await waitFor(() => {
      // Begin login called first with tenant + email.
      expect(mockWebAuthnBeginLogin).toHaveBeenCalledWith('system', 'alice@example.com');

      // credentials.get called with an unwrapped publicKey whose challenge has
      // been base64url-decoded into an ArrayBuffer.
      expect(mockCredentialsGet).toHaveBeenCalledTimes(1);
      const getArg = mockCredentialsGet.mock.calls[0][0] as {
        publicKey: PublicKeyCredentialRequestOptions;
      };
      expect(getArg.publicKey.challenge).toBeInstanceOf(ArrayBuffer);

      // Finish login called with the serialized assertion.
      expect(mockWebAuthnFinishLogin).toHaveBeenCalledTimes(1);
      expect(mockWebAuthnFinishLogin.mock.calls[0][0]).toBe('system');
      expect(mockWebAuthnFinishLogin.mock.calls[0][1]).toBe('alice@example.com');
    });
  });

  // --------------------------------------------------------------------------
  // 3. Smartcard click calls the SDK PIV login method
  // --------------------------------------------------------------------------

  it('smartcard click calls client.auth.loginPIV', async () => {
    mockLoginPIV.mockResolvedValueOnce(makeFinishTokens());

    render(LoginView);
    await fireEvent.click(screen.getByRole('button', { name: /sign in with smartcard/i }));

    await waitFor(() => {
      expect(mockLoginPIV).toHaveBeenCalledTimes(1);
    });
  });

  // --------------------------------------------------------------------------
  // 4. THE BUG FIX — successful passkey finish flips auth status AND redirects.
  //
  // This is the regression guard for the reported bug: a 200 finish with tokens
  // must call setAuthenticated (so the in-tab auth status becomes
  // 'authenticated' and MainApp's route guard leaves the user on the
  // dashboard) AND route to /dashboard. Persisting tokens without flipping the
  // status was exactly the silent-reset defect.
  // --------------------------------------------------------------------------

  it('on successful passkey finish: flips auth status, connects, and redirects to dashboard', async () => {
    mockWebAuthnBeginLogin.mockResolvedValueOnce(makeChallengeEnvelope());
    mockCredentialsGet.mockResolvedValueOnce(makeCredentialStub());
    mockWebAuthnFinishLogin.mockResolvedValueOnce(makeFinishTokens());

    render(LoginView);
    await submitPasskey('bob@example.com');

    await waitFor(() => {
      // setAuthenticated is the fix — it both persists tokens and flips status.
      expect(mockSetAuthenticated).toHaveBeenCalledTimes(1);
      expect(mockSetAuthenticated).toHaveBeenCalledWith({
        access_token: 'tok-access',
        refresh_token: 'tok-refresh',
      });
      expect(mockSetConnected).toHaveBeenCalled();
      expect(mockRouteDashboard).toHaveBeenCalledTimes(1);
    });

    // No error alert on success.
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  // --------------------------------------------------------------------------
  // 5. Successful PIV finish flips auth status + redirects.
  // --------------------------------------------------------------------------

  it('on successful PIV finish: flips auth status and redirects to dashboard', async () => {
    mockLoginPIV.mockResolvedValueOnce({
      access_token: 'piv-at',
      refresh_token: 'piv-rt',
      token_type: 'Bearer',
      expires_in: 3600,
    });

    render(LoginView);
    await fireEvent.click(screen.getByRole('button', { name: /sign in with smartcard/i }));

    await waitFor(() => {
      expect(mockSetAuthenticated).toHaveBeenCalledWith({
        access_token: 'piv-at',
        refresh_token: 'piv-rt',
      });
      expect(mockRouteDashboard).toHaveBeenCalledTimes(1);
    });
  });

  // --------------------------------------------------------------------------
  // 6. THE OTHER HALF OF THE FIX — finish failure shows an error, not a reset.
  //
  // When finish rejects, the view must surface a real error alert and must NOT
  // flip auth status or navigate. (Before the fix, swallowed failures left the
  // user staring at the chooser with no feedback.)
  // --------------------------------------------------------------------------

  it('on finish failure: shows an error alert and does not authenticate or redirect', async () => {
    mockWebAuthnBeginLogin.mockResolvedValueOnce(makeChallengeEnvelope());
    mockCredentialsGet.mockResolvedValueOnce(makeCredentialStub());
    mockWebAuthnFinishLogin.mockRejectedValueOnce(
      makeStatusError(401, 'assertion verification failed'),
    );

    render(LoginView);
    await submitPasskey('carol@example.com');

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/assertion verification failed/i);
    });
    expect(mockSetAuthenticated).not.toHaveBeenCalled();
    expect(mockRouteDashboard).not.toHaveBeenCalled();
  });

  // --------------------------------------------------------------------------
  // 7. A 200 finish that returns NO token is treated as a failure, not success.
  // --------------------------------------------------------------------------

  it('on finish 200 with empty token: shows an error and does not redirect', async () => {
    mockWebAuthnBeginLogin.mockResolvedValueOnce(makeChallengeEnvelope());
    mockCredentialsGet.mockResolvedValueOnce(makeCredentialStub());
    mockWebAuthnFinishLogin.mockResolvedValueOnce({ access_token: '' });

    render(LoginView);
    await submitPasskey('dave@example.com');

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/did not return a session token/i);
    });
    expect(mockSetAuthenticated).not.toHaveBeenCalled();
    expect(mockRouteDashboard).not.toHaveBeenCalled();
  });

  // --------------------------------------------------------------------------
  // 8. Email not found → specific error message (begin returns 404)
  // --------------------------------------------------------------------------

  it('shows "email not found" error when begin returns 404', async () => {
    mockWebAuthnBeginLogin.mockRejectedValueOnce(makeStatusError(404, 'not found'));

    render(LoginView);
    await submitPasskey('ghost@example.com');

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/couldn't find that email/i);
    });
  });

  // --------------------------------------------------------------------------
  // 9. WebAuthn assertion cancelled → declined/cancelled error
  // --------------------------------------------------------------------------

  it('shows declined/cancelled error when navigator.credentials.get is cancelled', async () => {
    mockWebAuthnBeginLogin.mockResolvedValueOnce(makeChallengeEnvelope());
    mockCredentialsGet.mockRejectedValueOnce(
      new DOMException('Operation not allowed', 'NotAllowedError'),
    );

    render(LoginView);
    await submitPasskey('alice@example.com');

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/declined or canceled/i);
    });
  });

  // --------------------------------------------------------------------------
  // 10. PIV cert not recognized → smartcard-specific error
  // --------------------------------------------------------------------------

  it('shows PIV not recognized error on 401 response', async () => {
    mockLoginPIV.mockRejectedValueOnce(makeStatusError(401, 'unrecognized'));

    render(LoginView);
    await fireEvent.click(screen.getByRole('button', { name: /sign in with smartcard/i }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/isn't recognized/i);
    });
  });

  // --------------------------------------------------------------------------
  // 11. Begin rejection surfaces an error alert (no silent reset).
  // --------------------------------------------------------------------------

  it('shows an error alert when begin login rejects', async () => {
    mockWebAuthnBeginLogin.mockRejectedValueOnce(new Error('server boom'));

    render(LoginView);
    await submitPasskey('alice@example.com');

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/server boom/i);
    });
    expect(mockSetAuthenticated).not.toHaveBeenCalled();
  });
});
