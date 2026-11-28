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
    beginRegistration(req: XKMSFIDO2BeginRegistrationRequest, tenantId?: string): Promise<XKMSFIDO2Challenge>;
    finishRegistration(req: XKMSFIDO2FinishRegistrationRequest, tenantId?: string): Promise<XKMSFIDO2FinishRegistrationResponse>;
    /** Begins adding another passkey for the caller, in the session's tenant. */
    beginAddCredential(): Promise<XKMSFIDO2Challenge>;
    beginAuthentication(req: XKMSFIDO2BeginAuthenticationRequest, tenantId?: string): Promise<XKMSFIDO2Challenge>;
    finishAuthentication(req: XKMSFIDO2FinishAuthenticationRequest, tenantId?: string): Promise<XKMSFIDO2FinishAuthenticationResponse>;
}
//# sourceMappingURL=xkms_fido2.d.ts.map