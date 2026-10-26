/**
 * requireTLSForSOPIN throws SOPINRequiresTLSError when soPIN is nonempty and
 * endpoint is not https://. Every transport calls it before putting a PIN in
 * a request. The rule is the Go SDK's (sdk/go/transport/mcp): a plain
 * "https://" prefix check with no loopback exception. A call without a PIN
 * is never refused.
 */
export declare function requireTLSForSOPIN(transport: string, endpoint: string, soPIN?: string): void;
/**
 * requireTLSForSecret throws SOPINRequiresTLSError when hasSecret is true and
 * endpoint is not https://. It is the Go MCP transport's requireTLSForSecret:
 * a verb that carries any barrier secret (an SO or user PIN, an initialize or
 * unseal secret, a passphrase or a decrypted key share) calls it before the
 * request is built, so the secret never crosses a plaintext connection. The
 * error never contains the secret.
 */
export declare function requireTLSForSecret(transport: string, endpoint: string, hasSecret: boolean): void;
//# sourceMappingURL=so_pin.d.ts.map