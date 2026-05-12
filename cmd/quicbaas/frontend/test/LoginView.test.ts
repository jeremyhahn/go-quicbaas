/**
 * LoginView.test.ts
 *
 * Verifies that LoginView:
 *   - Renders FIDO2 + PIV options with no password input.
 *   - Passkey click shows email field; submitting calls begin/finish endpoints
 *     and then navigator.credentials.get with the challenge.
 *   - Smartcard click POSTs to /api/v1/auth/login/piv and follows the PIV path.
 *   - On successful login, tokens are persisted and the router redirects to /dashboard.
 *   - Error states produce the correct alert messages.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const { mockRouteDashboard, mockSetConnected } = vi.hoisted(() => ({
  mockRouteDashboard: vi.fn(),
  mockSetConnected: vi.fn(),
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
// fetch mock
// ---------------------------------------------------------------------------

const mockFetch = vi.fn();
Object.defineProperty(window, 'fetch', {
  writable: true,
  configurable: true,
  value: mockFetch,
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

function makeJsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
    json: async () => body,
  } as unknown as Response;
}

function makeErrorResponse(status: number, text = ''): Response {
  return {
    ok: false,
    status,
    text: async () => text,
    json: async () => { throw new Error('not json'); },
  } as unknown as Response;
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
  });

  afterEach(() => {
    cleanup();
  });

  // --------------------------------------------------------------------------
  // 1. No password input — FIDO2 + PIV buttons present
  // --------------------------------------------------------------------------

  it('renders FIDO2 + PIV options, no password input', () => {
    render(LoginView);

    // No password fields at all.
    const passwordInputs = document.querySelectorAll('input[type="password"]');
    expect(passwordInputs.length).toBe(0);

    // Both method buttons are present.
    expect(screen.getByRole('button', { name: /sign in with passkey/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /sign in with smartcard/i })).toBeInTheDocument();

    // The brand heading is present.
    expect(screen.getByText('QuicBaaS')).toBeInTheDocument();
    expect(screen.getByText('Hardware-Backed Sign-In')).toBeInTheDocument();
  });

  // --------------------------------------------------------------------------
  // 2. Passkey flow: click → email field → begin → credentials.get → finish
  // --------------------------------------------------------------------------

  it('passkey click shows email field; begin and finish called in order', async () => {
    const challengeOptions = { challenge: 'abc123', timeout: 60000 };
    const tokens = { access_token: 'tok-access', refresh_token: 'tok-refresh' };
    const credStub = makeCredentialStub();

    // begin → challenge; finish → tokens
    mockFetch
      .mockResolvedValueOnce(makeJsonResponse(challengeOptions))
      .mockResolvedValueOnce(makeJsonResponse(tokens));

    mockCredentialsGet.mockResolvedValueOnce(credStub);

    render(LoginView);

    // Click passkey button to reveal email input.
    await fireEvent.click(screen.getByRole('button', { name: /sign in with passkey/i }));

    await waitFor(() => {
      expect(screen.getByLabelText(/email address/i)).toBeInTheDocument();
    });

    // Enter email and submit.
    await fireEvent.input(screen.getByLabelText(/email address/i), {
      target: { value: 'alice@example.com' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /authenticate with passkey/i }));

    await waitFor(() => {
      // Begin login called first.
      expect(mockFetch).toHaveBeenNthCalledWith(
        1,
        expect.stringContaining('/api/v1/auth/login/fido2/begin'),
        expect.objectContaining({ method: 'POST' }),
      );

      // navigator.credentials.get called with the challenge.
      expect(mockCredentialsGet).toHaveBeenCalledWith(
        expect.objectContaining({ publicKey: challengeOptions }),
      );

      // Finish login called with assertion.
      expect(mockFetch).toHaveBeenNthCalledWith(
        2,
        expect.stringContaining('/api/v1/auth/login/fido2/finish'),
        expect.objectContaining({ method: 'POST' }),
      );
    });
  });

  // --------------------------------------------------------------------------
  // 3. Smartcard click navigates to PIV endpoint via fetch
  // --------------------------------------------------------------------------

  it('smartcard click POSTs to /api/v1/auth/login/piv', async () => {
    const tokens = { access_token: 'piv-tok', refresh_token: 'piv-ref' };
    mockFetch.mockResolvedValueOnce(makeJsonResponse(tokens));

    render(LoginView);

    await fireEvent.click(screen.getByRole('button', { name: /sign in with smartcard/i }));

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/auth/login/piv'),
        expect.objectContaining({ method: 'GET', credentials: 'include' }),
      );
    });
  });

  // --------------------------------------------------------------------------
  // 4. Redirect to /dashboard on successful login (FIDO2 path)
  // --------------------------------------------------------------------------

  it('redirects to /dashboard on successful passkey login', async () => {
    const tokens = { access_token: 'at', refresh_token: 'rt', id_token: 'it' };
    const credStub = makeCredentialStub();

    mockFetch
      .mockResolvedValueOnce(makeJsonResponse({ challenge: 'ch' }))
      .mockResolvedValueOnce(makeJsonResponse(tokens));

    mockCredentialsGet.mockResolvedValueOnce(credStub);

    render(LoginView);

    await fireEvent.click(screen.getByRole('button', { name: /sign in with passkey/i }));
    await waitFor(() => screen.getByLabelText(/email address/i));

    await fireEvent.input(screen.getByLabelText(/email address/i), {
      target: { value: 'bob@example.com' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /authenticate with passkey/i }));

    await waitFor(() => {
      expect(window.localStorage.getItem('quicbaas_token')).toBe('at');
      expect(mockSetConnected).toHaveBeenCalled();
      expect(mockRouteDashboard).toHaveBeenCalled();
    });
  });

  // --------------------------------------------------------------------------
  // 5. Redirect to /dashboard on successful PIV login
  // --------------------------------------------------------------------------

  it('redirects to /dashboard on successful PIV login', async () => {
    const tokens = { access_token: 'piv-at', refresh_token: 'piv-rt' };
    mockFetch.mockResolvedValueOnce(makeJsonResponse(tokens));

    render(LoginView);

    await fireEvent.click(screen.getByRole('button', { name: /sign in with smartcard/i }));

    await waitFor(() => {
      expect(window.localStorage.getItem('quicbaas_token')).toBe('piv-at');
      expect(mockRouteDashboard).toHaveBeenCalled();
    });
  });

  // --------------------------------------------------------------------------
  // 6. Email not found → specific error message
  // --------------------------------------------------------------------------

  it('shows "email not found" error when server returns 404', async () => {
    mockFetch.mockResolvedValueOnce(makeErrorResponse(404));

    render(LoginView);

    await fireEvent.click(screen.getByRole('button', { name: /sign in with passkey/i }));
    await waitFor(() => screen.getByLabelText(/email address/i));

    await fireEvent.input(screen.getByLabelText(/email address/i), {
      target: { value: 'ghost@example.com' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /authenticate with passkey/i }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/couldn't find that email/i);
    });
  });

  // --------------------------------------------------------------------------
  // 7. WebAuthn assertion cancelled → declined/cancelled error
  // --------------------------------------------------------------------------

  it('shows declined/cancelled error when navigator.credentials.get is cancelled', async () => {
    mockFetch.mockResolvedValueOnce(makeJsonResponse({ challenge: 'ch' }));
    const notAllowed = new DOMException('Operation not allowed', 'NotAllowedError');
    mockCredentialsGet.mockRejectedValueOnce(notAllowed);

    render(LoginView);

    await fireEvent.click(screen.getByRole('button', { name: /sign in with passkey/i }));
    await waitFor(() => screen.getByLabelText(/email address/i));

    await fireEvent.input(screen.getByLabelText(/email address/i), {
      target: { value: 'alice@example.com' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /authenticate with passkey/i }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/declined or canceled/i);
    });
  });

  // --------------------------------------------------------------------------
  // 8. PIV cert not recognized → smartcard-specific error
  // --------------------------------------------------------------------------

  it('shows PIV not recognized error on 401 response', async () => {
    mockFetch.mockResolvedValueOnce(makeErrorResponse(401));

    render(LoginView);

    await fireEvent.click(screen.getByRole('button', { name: /sign in with smartcard/i }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/isn't recognized/i);
    });
  });

  // --------------------------------------------------------------------------
  // 9. Server unreachable → generic error
  // --------------------------------------------------------------------------

  it('shows generic server error when fetch throws TypeError', async () => {
    mockFetch.mockRejectedValueOnce(new TypeError('Failed to fetch'));

    render(LoginView);

    await fireEvent.click(screen.getByRole('button', { name: /sign in with passkey/i }));
    await waitFor(() => screen.getByLabelText(/email address/i));

    await fireEvent.input(screen.getByLabelText(/email address/i), {
      target: { value: 'alice@example.com' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /authenticate with passkey/i }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/can't reach the server/i);
    });
  });
});
