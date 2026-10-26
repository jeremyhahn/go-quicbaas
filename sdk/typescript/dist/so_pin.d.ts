/**
 * requireTLSForSOPIN throws SOPINRequiresTLSError when soPIN is nonempty and
 * endpoint is not https://. Every transport calls it before putting a PIN in
 * a request. The rule is the Go SDK's (sdk/go/transport/mcp): a plain
 * "https://" prefix check with no loopback exception. A call without a PIN
 * is never refused.
 */
export declare function requireTLSForSOPIN(transport: string, endpoint: string, soPIN?: string): void;
//# sourceMappingURL=so_pin.d.ts.map