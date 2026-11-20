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

// ---------------------------------------------------------------------------
// xkms FIDO2 (WebAuthn passkey) domain types.
//
// Mirrors go-xkms pkg/api/transport/types.go. The options_json,
// attestation_json and assertion_json fields are JSON text strings that are
// passed through unparsed: the browser's WebAuthn API consumes and produces
// them. Paths: /api/v1/xkms/fido2/*.
// ---------------------------------------------------------------------------

/** Body of POST /api/v1/xkms/fido2/register/begin. */
export interface XKMSFIDO2BeginRegistrationRequest {
  email: string;
  display_name: string;
}

/** The challenge every begin call answers with. */
export interface XKMSFIDO2Challenge {
  session_id: string;
  /** WebAuthn creation or request options, as JSON text. */
  options_json: string;
}

/** Body of POST /api/v1/xkms/fido2/register/finish. */
export interface XKMSFIDO2FinishRegistrationRequest {
  session_id: string;
  /** The authenticator's attestation response, as JSON text. */
  attestation_json: string;
}

/** Answer of POST /api/v1/xkms/fido2/register/finish. */
export interface XKMSFIDO2FinishRegistrationResponse {
  success: boolean;
  credential_id: string;
  token?: string;
  message?: string;
}

/** Body of POST /api/v1/xkms/fido2/authenticate/begin. */
export interface XKMSFIDO2BeginAuthenticationRequest {
  email: string;
}

/** Body of POST /api/v1/xkms/fido2/authenticate/finish. */
export interface XKMSFIDO2FinishAuthenticationRequest {
  session_id: string;
  email: string;
  /** The authenticator's assertion response, as JSON text. */
  assertion_json: string;
}

/** Answer of POST /api/v1/xkms/fido2/authenticate/finish. */
export interface XKMSFIDO2FinishAuthenticationResponse {
  success: boolean;
  token?: string;
  username?: string;
  message?: string;
}

/**
 * XKMSFIDO2Client registers and authenticates FIDO2 passkeys. The optional
 * tenantId names the tenant; without it the server uses the caller's
 * session tenant.
 */
export interface XKMSFIDO2Client {
  beginRegistration(
    req: XKMSFIDO2BeginRegistrationRequest,
    tenantId?: string,
  ): Promise<XKMSFIDO2Challenge>;
  finishRegistration(
    req: XKMSFIDO2FinishRegistrationRequest,
    tenantId?: string,
  ): Promise<XKMSFIDO2FinishRegistrationResponse>;
  /** Begins adding another passkey for the caller, in the session's tenant. */
  beginAddCredential(): Promise<XKMSFIDO2Challenge>;
  beginAuthentication(
    req: XKMSFIDO2BeginAuthenticationRequest,
    tenantId?: string,
  ): Promise<XKMSFIDO2Challenge>;
  finishAuthentication(
    req: XKMSFIDO2FinishAuthenticationRequest,
    tenantId?: string,
  ): Promise<XKMSFIDO2FinishAuthenticationResponse>;
}
