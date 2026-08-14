/** CrossSignedRootPoolResponse is the active CA's cross-sign-aware root set, PEM-decoded. */
export interface CrossSignedRootPoolResponse {
    root_pems: string[];
}
/** AutoImportAIARequest carries a chain of PEM certificates whose AIA extensions are walked. */
export interface AutoImportAIARequest {
    /** chain_pems is a list of PEM-encoded certificates (client-side convenience input). */
    chain_pems: string[];
}
/** AutoImportAIAResponse reports how many chain certificates had their AIA processed. */
export interface AutoImportAIAResponse {
    imported: number;
}
/** PublishDANERequest carries a PEM certificate plus the DNS name/port TLSA records are published for. */
export interface PublishDANERequest {
    name: string;
    port: number;
    /** cert_pem is a single PEM-encoded certificate (client-side convenience input). */
    cert_pem: string;
}
/** PublishDANEResponse reports how many TLSA records were published. */
export interface PublishDANEResponse {
    published: number;
}
/**
 * TrustClient exposes trust composition operations (cross-sign root pool,
 * AIA auto-import, DANE publication). Maps to /api/v1/trust/* REST endpoints.
 */
export interface TrustClient {
    /** crossSignedRootPool returns the CA's cross-sign-aware root set. GET /api/v1/trust/cross-signed-roots */
    crossSignedRootPool(): Promise<CrossSignedRootPoolResponse>;
    /** autoImportAIA imports issuing CAs referenced by a chain's AIA extension. POST /api/v1/trust/auto-import-aia */
    autoImportAIA(req: AutoImportAIARequest): Promise<AutoImportAIAResponse>;
    /**
     * publishDANE derives and publishes DANE TLSA records for a certificate.
     * Returns a typed error when no DANE publisher is registered on the server.
     * POST /api/v1/trust/publish-dane
     */
    publishDANE(req: PublishDANERequest): Promise<PublishDANEResponse>;
}
//# sourceMappingURL=trust.d.ts.map