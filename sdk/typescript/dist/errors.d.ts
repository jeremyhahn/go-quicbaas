export declare class QuicBaaSError extends Error {
    constructor(message: string);
}
export declare class ConnectionError extends QuicBaaSError {
    constructor(message: string);
}
export declare class NotConnectedError extends QuicBaaSError {
    constructor();
}
export declare class APIError extends QuicBaaSError {
    readonly status: number;
    constructor(status: number, message: string);
}
export declare class DeployNotSupportedError extends QuicBaaSError {
    constructor(transport: string);
}
export declare class TransportNotSupportedError extends QuicBaaSError {
    readonly transport: string;
    readonly family: string;
    readonly reason: string;
    /** method is the verb that was called, when the refusal is for one. */
    readonly method?: string;
    constructor(transport: string, family: string, reason: string, method?: string);
}
/**
 * SOPINRequiresTLSError is thrown before any request is sent when a call
 * carries a Security Officer PIN and the endpoint is not https://. It
 * mirrors the Go SDK's SOPINRequiresTLSError: the PIN never travels in
 * plaintext, loopback included.
 */
export declare class SOPINRequiresTLSError extends QuicBaaSError {
    readonly transport: string;
    constructor(transport: string);
}
/**
 * Raised before any call when a ceremony initialize request does not select
 * exactly one bootstrap administrator credential path: admin_csr_pem,
 * admin_cert_pem or admin_generate. The server refuses the same request
 * (go-qrdb NoAdminCredentialSelectedError); the client fails first so no SO
 * PIN leaves the process for a request that cannot succeed.
 */
export declare class CeremonyAdminSelectorError extends QuicBaaSError {
    /** The selectors the request set, in field order; empty when none. */
    readonly selected: string[];
    constructor(selected: string[]);
}
export declare class McpError extends QuicBaaSError {
    readonly tool: string;
    readonly code?: number;
    constructor(tool: string, message: string, code?: number);
}
export declare class SSHError extends QuicBaaSError {
    /** HTTP status code from the server response. */
    readonly status: number;
    /** Resource family: 'ssh-keys', 'known_hosts', 'authorized_key', etc. */
    readonly resource: string;
    constructor(status: number, message: string, resource?: string);
}
export declare class SSHNotFoundError extends SSHError {
    constructor(message: string, resource?: string);
}
export declare class SSHDuplicateError extends SSHError {
    constructor(message: string, resource?: string);
}
export declare class SSHInvalidError extends SSHError {
    constructor(message: string, resource?: string);
}
export declare class SSHUnavailableError extends SSHError {
    constructor(message: string);
}
export declare class SSHServerError extends SSHError {
    constructor(status: number, message: string, resource?: string);
}
export declare class SSHHostMismatchError extends SSHError {
    /** Hostname whose key did not match the stored pin. */
    readonly host: string;
    /** SHA256 fingerprint stored in the pin store. */
    readonly expected: string;
    /** SHA256 fingerprint presented by the connecting host. */
    readonly received: string;
    constructor(host: string, expected: string, received: string, message?: string);
}
//# sourceMappingURL=errors.d.ts.map