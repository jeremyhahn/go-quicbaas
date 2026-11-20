/** The role an endorsement plays in appraisal. */
export type EndorsementKind = 'reference-signer' | 'ak-root';
/** A trust anchor the attestation verifier accepts. */
export interface Endorsement {
    name: string;
    kind: EndorsementKind;
    certificate_pem?: string;
    public_key_pem?: string;
    /** RFC 3339 timestamp. */
    created_at: string;
}
/** Body of PUT /api/v1/attestation/endorsements/{name}. */
export interface PutEndorsementRequest {
    kind: EndorsementKind;
    certificate_pem?: string;
    public_key_pem?: string;
}
/** Response of GET /api/v1/attestation/endorsements. */
export interface ListEndorsementsResponse {
    endorsements: Endorsement[];
}
/** A stored, signed CoRIM reference-value manifest. */
export interface ReferenceValues {
    name: string;
    manifest_id: string;
    profile?: string;
    signer?: string;
    /** RFC 3339 timestamp. */
    not_before?: string;
    /** RFC 3339 timestamp. */
    not_after?: string;
    pcr_indices: number[];
    /** RFC 3339 timestamp. */
    stored_at: string;
    /** Base64 of the signed CoRIM; omitted from list responses. */
    signed_corim?: string;
}
/** Body of PUT /api/v1/attestation/reference-values/{name}. */
export interface PutReferenceValuesRequest {
    /** Base64 of the signed CoRIM. */
    signed_corim: string;
}
/** Response of GET /api/v1/attestation/reference-values. */
export interface ListReferenceValuesResponse {
    reference_values: ReferenceValues[];
}
/** Manages the attestation verifier's endorsements and reference values. */
export interface AttestationClient {
    /** Creates or replaces the named endorsement. */
    putEndorsement(name: string, req: PutEndorsementRequest): Promise<Endorsement>;
    /** Lists endorsements, optionally only those of one kind. */
    listEndorsements(kind?: EndorsementKind): Promise<ListEndorsementsResponse>;
    /** Deletes the named endorsement. */
    deleteEndorsement(name: string): Promise<void>;
    /** Stores the signed CoRIM (base64) under the given name. */
    putReferenceValues(name: string, signedCoRIM: string): Promise<ReferenceValues>;
    /** Returns the named reference values, with the signed CoRIM. */
    getReferenceValues(name: string): Promise<ReferenceValues>;
    /** Lists reference values, without the signed CoRIM bodies. */
    listReferenceValues(): Promise<ListReferenceValuesResponse>;
    /** Deletes the named reference values. */
    deleteReferenceValues(name: string): Promise<void>;
}
//# sourceMappingURL=attestation.d.ts.map