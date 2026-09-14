/**
 * LoginView.test.ts
 *
 * Verifies that LoginView:
 *   - Renders FIDO2 + PIV options with no password input.
 *   - Renders the organization as a TEXT FIELD, never a dropdown. The dropdown
 *     was fed by an anonymous tenant-listing endpoint that disclosed every
 *     tenant's namespace to unauthenticated callers; both are gone.
 *   - Prefills the organization from the last SUCCESSFUL login (localStorage)
 *     and defaults to 'system' for a first-time visitor.
 *   - Submits the typed organization id verbatim to the SDK.
 *   - Passkey click shows email field; submitting calls the SDK begin/finish
 *     methods and then navigator.credentials.get with the decoded challenge.
 *   - Renders the organization field on the PASSKEY step too, and submits what
 *     was typed there (#575: the field existed only on the chooser, so a
 *     first-time visitor signed in against the 'system' default with no way to
 *     correct it).
 *   - A challenge with no allowCredentials is reported instead of handed to
 *     navigator.credentials.get, and an assertion that never settles is cut off
 *     by the ceremony deadline. Both leave the form usable (#575).
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
import { tick } from 'svelte';

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const {
  mockRouteDashboard,
  mockSetConnected,
  mockSetAuthenticated,
  mockClearSessionEndReason,
  mockAuthStore,
  mockWebAuthnBeginLogin,
  mockWebAuthnFinishLogin,
  mockLoginPIV,
} = vi.hoisted(() => ({
  mockRouteDashboard: vi.fn(),
  mockSetConnected: vi.fn(),
  mockSetAuthenticated: vi.fn(),
  mockClearSessionEndReason: vi.fn(),
  // Mutable stand-in for the reactive auth store: a test sets endReason before
  // render to stage "the operator was bounced here" and reads what the view
  // does with it.
  mockAuthStore: { status: 'unauthenticated', endReason: '' },
  mockWebAuthnBeginLogin: vi.fn(),
  mockWebAuthnFinishLogin: vi.fn(),
  mockLoginPIV: vi.fn(),
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
  authStore: mockAuthStore,
  clearSessionEndReason: mockClearSessionEndReason,
}));

// The SDK client is the sole transport. Begin/finish/PIV are the only methods
// LoginView calls through client.auth.* — there is no tenant-listing call.
vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(async () => ({
    auth: {
      webAuthnBeginLogin: mockWebAuthnBeginLogin,
      webAuthnFinishLogin: mockWebAuthnFinishLogin,
      loginPIV: mockLoginPIV,
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

/**
 * The unified-ceremony login challenge webAuthnBeginLogin returns: ceremony +
 * relying-party session ids plus the go-webauthn assertion options envelope.
 */
function makeChallengeEnvelope(): unknown {
  return {
    session_id: 'ceremony-1',
    rp_session_id: 'rp-1',
    options: {
      publicKey: {
        challenge: 'abc123',
        timeout: 60000,
        // A login that names an account always carries that account's enrolled
        // credentials; a challenge without them is the #575 failure case and
        // has its own tests below.
        allowCredentials: [{ id: 'Y3JlZC1pZC1zdHVi', type: 'public-key' }],
      },
    },
  };
}

/** The same envelope with no credential to assert against (#575). */
function makeEmptyCredentialChallenge(allowCredentials: unknown[] | undefined): unknown {
  return {
    session_id: 'ceremony-1',
    rp_session_id: 'rp-1',
    options: {
      publicKey: {
        challenge: 'abc123',
        timeout: 60000,
        ...(allowCredentials === undefined ? {} : { allowCredentials }),
      },
    },
  };
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
    mockAuthStore.endReason = '';
  });

  afterEach(() => {
    cleanup();
  });

  /** Drive the passkey flow up to (and including) clicking Authenticate. */
  async function submitPasskey(email = 'alice@example.com', org?: string): Promise<void> {
    if (org !== undefined) {
      await fireEvent.input(screen.getByLabelText(/organization/i), { target: { value: org } });
    }
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

      // Finish login called with the in-flight challenge and the serialized
      // assertion (the ceremony carries the tenant scope server-side).
      expect(mockWebAuthnFinishLogin).toHaveBeenCalledTimes(1);
      expect(mockWebAuthnFinishLogin.mock.calls[0][0]).toMatchObject({
        session_id: 'ceremony-1',
        rp_session_id: 'rp-1',
      });
      expect(mockWebAuthnFinishLogin.mock.calls[0][1]).toMatchObject({
        id: 'cred-id-stub',
        type: 'public-key',
      });
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

  it('on finish failure: shows a generic error alert and does not authenticate or redirect', async () => {
    mockWebAuthnBeginLogin.mockResolvedValueOnce(makeChallengeEnvelope());
    mockCredentialsGet.mockResolvedValueOnce(makeCredentialStub());
    mockWebAuthnFinishLogin.mockRejectedValueOnce(
      makeStatusError(401, 'assertion verification failed'),
    );

    render(LoginView);
    await submitPasskey('carol@example.com');

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/sign-in failed/i);
    });
    // The server's internal reason must not be echoed to an anonymous caller.
    expect(screen.getByRole('alert')).not.toHaveTextContent(/assertion verification failed/i);
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
  // 8. A rejected begin must NOT reveal whether the org or the email exists.
  //
  // The server answers every identity-resolution failure with one uniform 401,
  // and the UI must not undo that by branching per status or echoing the
  // server's text. Otherwise the login screen becomes the tenant/user
  // enumeration oracle the deleted tenant-listing endpoint used to be.
  // --------------------------------------------------------------------------

  it('shows one generic message for every rejected begin, naming neither org nor email', async () => {
    const rejections = [
      makeStatusError(401, 'authentication failed'),
      makeStatusError(404, 'tenant "acme-corp" not found'),
      makeStatusError(404, 'user "ghost@example.com" not found'),
    ];

    const seen: string[] = [];
    for (const rejection of rejections) {
      mockWebAuthnBeginLogin.mockRejectedValueOnce(rejection);

      render(LoginView);
      await submitPasskey('ghost@example.com', 'acme-corp');

      const alert = await screen.findByRole('alert');
      const text = alert.textContent ?? '';
      seen.push(text);

      expect(text).not.toMatch(/acme-corp/i);
      expect(text).not.toMatch(/ghost@example\.com/i);
      expect(text).not.toMatch(/not found/i);
      expect(text).toMatch(/sign-in failed/i);

      cleanup();
    }

    // All three rejections produced the SAME message.
    expect(new Set(seen).size).toBe(1);
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

  it('shows a generic error alert when begin login rejects', async () => {
    mockWebAuthnBeginLogin.mockRejectedValueOnce(new Error('server boom'));

    render(LoginView);
    await submitPasskey('alice@example.com');

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/sign-in failed/i);
    });
    // The raw server text must not reach the user — it is an oracle surface.
    expect(screen.getByRole('alert')).not.toHaveTextContent(/server boom/i);
    expect(mockSetAuthenticated).not.toHaveBeenCalled();
  });

  // --------------------------------------------------------------------------
  // 12. THE SECURITY FIX — organization is a TEXT FIELD, not a dropdown.
  //
  // The dropdown could only be populated by an unauthenticated tenant-listing
  // endpoint, which handed every tenant id + name to anyone who loaded this
  // page. Both the dropdown and the endpoint are gone.
  // --------------------------------------------------------------------------

  it('renders the organization as a text input and no tenant dropdown', () => {
    render(LoginView);

    const org = screen.getByLabelText(/organization/i);
    expect(org).toBeInTheDocument();
    expect(org.tagName).toBe('INPUT');
    expect(org).toHaveAttribute('type', 'text');
    // Accessible + autofill-friendly.
    expect(org).toHaveAttribute('autocomplete', 'organization');

    // No <select> anywhere on the login screen.
    expect(document.querySelectorAll('select').length).toBe(0);
  });

  // --------------------------------------------------------------------------
  // 13. UX — first-time visitor gets the client-side 'system' default.
  // --------------------------------------------------------------------------

  it('defaults the organization to system when nothing is remembered', () => {
    render(LoginView);

    expect(screen.getByLabelText(/organization/i)).toHaveValue('system');
  });

  // --------------------------------------------------------------------------
  // 14. UX — the last SUCCESSFUL organization is prefilled on the next visit.
  // --------------------------------------------------------------------------

  it('prefills the organization from localStorage on a return visit', () => {
    localStore['quicbaas_login_tenant'] = 'acme-corp';

    render(LoginView);

    expect(screen.getByLabelText(/organization/i)).toHaveValue('acme-corp');
  });

  // --------------------------------------------------------------------------
  // 15. The TYPED organization is what gets submitted — not a stale prefill.
  // --------------------------------------------------------------------------

  it('submits the typed organization id to begin and finish', async () => {
    localStore['quicbaas_login_tenant'] = 'acme-corp';
    mockWebAuthnBeginLogin.mockResolvedValueOnce(makeChallengeEnvelope());
    mockCredentialsGet.mockResolvedValueOnce(makeCredentialStub());
    mockWebAuthnFinishLogin.mockResolvedValueOnce(makeFinishTokens());

    render(LoginView);
    await submitPasskey('alice@example.com', 'globex');

    await waitFor(() => {
      expect(mockWebAuthnBeginLogin).toHaveBeenCalledWith('globex', 'alice@example.com');
      expect(mockWebAuthnFinishLogin.mock.calls[0][0]).toMatchObject({
        session_id: 'ceremony-1',
      });
    });
  });

  // --------------------------------------------------------------------------
  // 16. Only a SUCCESSFUL login is remembered — a typo never sticks.
  // --------------------------------------------------------------------------

  it('remembers the organization after a successful login', async () => {
    mockWebAuthnBeginLogin.mockResolvedValueOnce(makeChallengeEnvelope());
    mockCredentialsGet.mockResolvedValueOnce(makeCredentialStub());
    mockWebAuthnFinishLogin.mockResolvedValueOnce(makeFinishTokens());

    render(LoginView);
    await submitPasskey('alice@example.com', 'globex');

    await waitFor(() => {
      expect(localStore['quicbaas_login_tenant']).toBe('globex');
    });
  });

  it('does NOT remember the organization when the login fails', async () => {
    mockWebAuthnBeginLogin.mockRejectedValueOnce(makeStatusError(401, 'authentication failed'));

    render(LoginView);
    await submitPasskey('alice@example.com', 'typo-org');

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument();
    });
    expect(localStore['quicbaas_login_tenant']).toBeUndefined();
  });

  // --------------------------------------------------------------------------
  // 17. An empty organization is rejected client-side before any network call.
  // --------------------------------------------------------------------------

  it('refuses to submit with an empty organization', async () => {
    render(LoginView);
    await submitPasskey('alice@example.com', '   ');

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/enter your organization/i);
    });
    expect(mockWebAuthnBeginLogin).not.toHaveBeenCalled();
  });

  // --------------------------------------------------------------------------
  // 18. A session that ended on its own explains itself here (#221).
  //
  // The operator was bounced to this page every 15 minutes of a supervised run
  // with nothing on screen to say why, which is indistinguishable from the
  // platform being broken. The notice is deliberately NOT an error alert: no
  // sign-in attempt failed, and dressing it as one would send the operator
  // hunting for a mistake they did not make.
  // --------------------------------------------------------------------------

  it('shows why the previous session ended', async () => {
    mockAuthStore.endReason = 'Your session could not be renewed (HTTP 401).';
    render(LoginView);

    const notice = await screen.findByRole('status');
    expect(notice).toHaveTextContent(/could not be renewed/i);
    // A status, not an alert: the chooser is idle, nothing has failed yet.
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('shows no notice when the operator signed out deliberately', () => {
    mockAuthStore.endReason = '';
    render(LoginView);

    expect(screen.queryByRole('status')).toBeNull();
  });

  it('drops the notice once a fresh sign-in begins', async () => {
    mockAuthStore.endReason = 'Your session could not be renewed (HTTP 401).';
    render(LoginView);
    await screen.findByRole('status');

    await fireEvent.click(screen.getByRole('button', { name: /sign in with passkey/i }));

    // The view asks the store to forget it: anything shown from here on is
    // about THIS attempt.
    expect(mockClearSessionEndReason).toHaveBeenCalledTimes(1);
  });

  it('does not let the stale notice mask an error from the new attempt', async () => {
    mockAuthStore.endReason = 'Your session could not be renewed (HTTP 401).';
    mockWebAuthnBeginLogin.mockRejectedValue(new Error('begin failed'));
    render(LoginView);

    await submitPasskey();

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument();
    });
    expect(screen.queryByRole('status')).toBeNull();
  });

  // --------------------------------------------------------------------------
  // 19. #575 — the organization is editable ON the passkey step.
  //
  // It used to live only on the chooser, so a visitor who went straight to the
  // passkey button signed in against the remembered value ('system' on a fresh
  // browser) with nowhere to correct it. The ceremony then came back with no
  // credentials and the form wedged.
  // --------------------------------------------------------------------------

  it('renders the organization field on the passkey step, prefilled from localStorage', async () => {
    localStore['quicbaas_login_tenant'] = 'acme-corp';

    render(LoginView);
    await fireEvent.click(screen.getByRole('button', { name: /sign in with passkey/i }));
    await waitFor(() => screen.getByLabelText(/email address/i));

    const org = screen.getByLabelText(/organization/i);
    expect(org).toBeInTheDocument();
    expect(org).toHaveValue('acme-corp');
  });

  it('sends the organization typed on the passkey step, not the remembered one', async () => {
    localStore['quicbaas_login_tenant'] = 'acme-corp';
    mockWebAuthnBeginLogin.mockResolvedValueOnce(makeChallengeEnvelope());
    mockCredentialsGet.mockResolvedValueOnce(makeCredentialStub());
    mockWebAuthnFinishLogin.mockResolvedValueOnce(makeFinishTokens());

    render(LoginView);
    // Straight to the passkey step WITHOUT touching the chooser field.
    await fireEvent.click(screen.getByRole('button', { name: /sign in with passkey/i }));
    await waitFor(() => screen.getByLabelText(/email address/i));

    await fireEvent.input(screen.getByLabelText(/organization/i), {
      target: { value: 'globex' },
    });
    await fireEvent.input(screen.getByLabelText(/email address/i), {
      target: { value: 'alice@example.com' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /authenticate with passkey/i }));

    await waitFor(() => {
      expect(mockWebAuthnBeginLogin).toHaveBeenCalledWith('globex', 'alice@example.com');
    });
    // A completed sign-in from the passkey step still earns the prefill slot.
    await waitFor(() => {
      expect(localStore['quicbaas_login_tenant']).toBe('globex');
    });
  });

  // --------------------------------------------------------------------------
  // 20. #575 — a challenge with no credential is reported, not handed to the
  // browser. Passing an empty allowCredentials asks the authenticator for a
  // DISCOVERABLE credential, which a key holding a non-resident one never
  // answers: no prompt, no error, and busy stuck true until a reload.
  // --------------------------------------------------------------------------

  for (const [label, allow] of [
    ['an empty allowCredentials list', [] as unknown[]],
    ['no allowCredentials member at all', undefined],
  ] as const) {
    it(`reports ${label} instead of calling credentials.get, and leaves the form usable`, async () => {
      mockWebAuthnBeginLogin.mockResolvedValueOnce(makeEmptyCredentialChallenge(allow));

      render(LoginView);
      await submitPasskey('alice@example.com', 'globex');

      await waitFor(() => {
        expect(screen.getByRole('alert')).toHaveTextContent(/no passkey is enrolled/i);
      });
      expect(mockCredentialsGet).not.toHaveBeenCalled();

      // The button is enabled again: the operator can fix the organization and
      // retry without reloading the page.
      expect(screen.getByRole('button', { name: /authenticate with passkey/i })).toBeEnabled();

      // Generic: it names neither the organization nor the email.
      const text = screen.getByRole('alert').textContent ?? '';
      expect(text).not.toMatch(/globex/i);
      expect(text).not.toMatch(/alice@example\.com/i);

      // Nothing was remembered and nothing was authenticated.
      expect(localStore['quicbaas_login_tenant']).toBeUndefined();
      expect(mockSetAuthenticated).not.toHaveBeenCalled();
    });
  }

  // --------------------------------------------------------------------------
  // 21. #575 — busy clears on the rejected ceremony, so the button comes back.
  // --------------------------------------------------------------------------

  it('re-enables the submit button after a rejected credentials.get', async () => {
    mockWebAuthnBeginLogin.mockResolvedValueOnce(makeChallengeEnvelope());
    mockCredentialsGet.mockRejectedValueOnce(
      new DOMException('Operation not allowed', 'NotAllowedError'),
    );

    render(LoginView);
    await submitPasskey('alice@example.com');

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/declined or canceled/i);
    });
    expect(screen.getByRole('button', { name: /authenticate with passkey/i })).toBeEnabled();
  });

  // --------------------------------------------------------------------------
  // 22. #575 — a ceremony that never settles is cut off by the deadline.
  //
  // This is the measured wedge: the authenticator never receives a
  // GetAssertion, credentials.get() stays pending forever, and without the
  // deadline the form stays disabled with no message until a reload. Fake
  // timers, so the test spends no real time waiting.
  // --------------------------------------------------------------------------

  it('times out a credentials.get that never settles and frees the form', async () => {
    vi.useFakeTimers();
    try {
      mockWebAuthnBeginLogin.mockResolvedValueOnce(makeChallengeEnvelope());
      mockCredentialsGet.mockReturnValueOnce(new Promise(() => {}));

      render(LoginView);
      await submitPasskey('alice@example.com');

      // Mid-ceremony: still working, button disabled, nothing to report yet.
      await tick();
      expect(screen.getByRole('button', { name: /authenticate with passkey/i })).toBeDisabled();
      expect(screen.queryByRole('alert')).toBeNull();

      await vi.advanceTimersByTimeAsync(60_000);
      await tick();

      expect(screen.getByRole('alert')).toHaveTextContent(/did not respond/i);
      expect(screen.getByRole('button', { name: /authenticate with passkey/i })).toBeEnabled();
      expect(mockWebAuthnFinishLogin).not.toHaveBeenCalled();
      expect(mockSetAuthenticated).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it('aborts the pending ceremony when the deadline fires', async () => {
    vi.useFakeTimers();
    try {
      mockWebAuthnBeginLogin.mockResolvedValueOnce(makeChallengeEnvelope());
      mockCredentialsGet.mockReturnValueOnce(new Promise(() => {}));

      render(LoginView);
      await submitPasskey('alice@example.com');
      await tick();

      const signal = (mockCredentialsGet.mock.calls[0][0] as { signal?: AbortSignal }).signal;
      expect(signal).toBeInstanceOf(AbortSignal);
      expect(signal?.aborted).toBe(false);

      await vi.advanceTimersByTimeAsync(60_000);

      // The browser is told to close its prompt, not just left running.
      expect(signal?.aborted).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });
});
