export interface XKMSBackendCapabilities {
    Keys: boolean;
    HardwareBacked: boolean;
    Signing: boolean;
    Decryption: boolean;
    KeyRotation: boolean;
    SymmetricEncryption: boolean;
    Sealing: boolean;
    Import: boolean;
    Export: boolean;
    KeyAgreement: boolean;
    ECIES: boolean;
    Attestation: boolean;
    QuantumSigning: boolean;
    KeyEncapsulation: boolean;
}
export interface XKMSBackendInfo {
    id: string;
    type: string;
    hardware_backed: boolean;
    capabilities: XKMSBackendCapabilities;
}
export interface XKMSListBackendsResponse {
    backends: XKMSBackendInfo[];
    page?: number;
    page_size?: number;
    total?: number;
    has_more?: boolean;
}
export interface XKMSHealthResponse {
    status: string;
    version?: string;
    message?: string;
}
export interface XKMSSealCapabilityResponse {
    can_seal: boolean;
    backend: string;
    message?: string;
}
export interface XKMSSealRequest {
    backend: string;
    key_id: string;
    data: string;
    aad?: string;
    tenant_id?: string;
    tpm_policy?: string;
    password?: string;
}
export interface XKMSSealResponse {
    backend: string;
    ciphertext: string;
    nonce?: string;
    tag?: string;
    tpm_public?: string;
    tpm_private?: string;
    wrapped_dek?: string;
    key_id?: string;
    metadata?: Record<string, string>;
    key_version?: number;
}
export interface XKMSUnsealRequest {
    backend: string;
    key_id: string;
    ciphertext: string;
    nonce?: string;
    tag?: string;
    aad?: string;
    tenant_id?: string;
    tpm_public?: string;
    tpm_private?: string;
    wrapped_dek?: string;
    metadata?: Record<string, string>;
    password?: string;
    key_version?: number;
}
export interface XKMSUnsealResponse {
    plaintext: string;
}
export interface XKMSCertificateInfo {
    key_id: string;
    subject?: string;
    issuer?: string;
    not_before?: string;
    not_after?: string;
    serial_number?: string;
    certificate_pem?: string;
}
export interface XKMSListCertificatesResponse {
    certificates: XKMSCertificateInfo[];
    page?: number;
    page_size?: number;
    total?: number;
    has_more?: boolean;
}
export interface XKMSCertificateResponse {
    key_id: string;
    certificate_pem: string;
}
export interface XKMSClient {
    listBackends(): Promise<XKMSListBackendsResponse>;
    getBackend(id: string): Promise<XKMSBackendInfo>;
    health(): Promise<XKMSHealthResponse>;
    canSeal(backend?: string): Promise<XKMSSealCapabilityResponse>;
    seal(req: XKMSSealRequest): Promise<XKMSSealResponse>;
    unseal(req: XKMSUnsealRequest): Promise<XKMSUnsealResponse>;
    listCertificates(backend?: string): Promise<XKMSListCertificatesResponse>;
    getCertificate(keyID: string): Promise<XKMSCertificateResponse>;
    saveCertificate(keyID: string, certificatePEM: string): Promise<void>;
    deleteCertificate(keyID: string): Promise<void>;
}
//# sourceMappingURL=xkms.d.ts.map