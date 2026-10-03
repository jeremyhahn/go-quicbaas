# Attestation — API

> **Status: NORMATIVE** · Last verified: 2026-10-01
> The capability on every transport, with permissions. Feature folder: `docs/attestation/`.

Service: gRPC `quicbaas.attestation.v1.AttestationService` (also over the Unix
socket); REST `/api/v1/attestation/*` (also QUIC); MCP `quicbaas_attestation_*`;
embedded `quicbaas.Attestation/*`. SDK: `client.Security().Attestation()`.

| Verb | Permission | REST | MCP tool |
|---|---|---|---|
| Verify | `attestation:verify` | `POST /api/v1/attestation/verify` | `quicbaas_attestation_verify` |
| VerifyChain | `attestation:verify` | `POST /api/v1/attestation/verify-chain` | `quicbaas_attestation_verify_chain` |
| AttestContainer | `attestation:attest` | `POST /api/v1/attestation/attest-container` | `quicbaas_attestation_attest_container` |
| PutEndorsement | `attestation:provision` | `PUT /api/v1/attestation/endorsements/{name}` | `quicbaas_attestation_put_endorsement` |
| ListEndorsements | `attestation:verify` | `GET /api/v1/attestation/endorsements?kind=` | `quicbaas_attestation_list_endorsements` |
| DeleteEndorsement | `attestation:provision` | `DELETE /api/v1/attestation/endorsements/{name}` → 204 | `quicbaas_attestation_delete_endorsement` |
| PutReferenceValues | `attestation:provision` | `PUT /api/v1/attestation/reference-values/{name}` | `quicbaas_attestation_put_reference_values` |
| GetReferenceValues | `attestation:verify` | `GET /api/v1/attestation/reference-values/{name}` | `quicbaas_attestation_get_reference_values` |
| ListReferenceValues | `attestation:verify` | `GET /api/v1/attestation/reference-values` | `quicbaas_attestation_list_reference_values` |
| DeleteReferenceValues | `attestation:provision` | `DELETE /api/v1/attestation/reference-values/{name}` → 204 | `quicbaas_attestation_delete_reference_values` |
| AppraiseQuote | `attestation:verify` | `POST /api/v1/attestation/appraise-quote` | `quicbaas_attestation_appraise_quote` |

`attestation:provision` is held by Admin only; `attestation:verify` by Admin and
Operator. All three are self-scoped: every verb acts on the caller's own
tenant. No request field names a tenant, so no caller, the system tenant
included, reaches another tenant's endorsements or reference values. A request
with no authenticated tenant is refused with 403 / `PermissionDenied`.

## Bodies

REST and the embedded transport use JSON with byte fields base64-encoded:

```json
// PUT /api/v1/attestation/endorsements/ak-ca
{"kind": "ak-root", "certificate_pem": "<base64 of the PEM>"}
// PUT /api/v1/attestation/reference-values/golden
{"signed_corim": "<base64 COSE_Sign1>"}
// POST /api/v1/attestation/appraise-quote
{"ak_certificate_pem": "…", "quote": "…", "signature": "…", "nonce": "…",
 "pcrs": {"0": "…", "7": "…"}, "event_log": "…", "reference_values": "golden"}
// → AttestationResult
{"verified": true, "cryptographic_failure": false, "baseline_source": "signed",
 "reference_values": "golden", "manifest_id": "corim-1", "signer": "…",
 "checks": [{"name": "ak-certificate", "outcome": "pass"}, {"name": "quote", "outcome": "pass"},
            {"name": "event-log", "outcome": "skipped", "reason": "no event log in evidence"},
            {"name": "baseline", "outcome": "pass"}],
 "appraised_at": "2026-10-01T12:00:00Z"}
```

MCP carries PEM as text and binaries as base64; `pcrs` is an object of index
→ base64 digest. Evidence that fails appraisal is a successful call whose
result says `verified: false`; errors are reserved for unusable requests
(400 / `INVALID_ARGUMENT`), missing records (404 / `NOT_FOUND`) and an
unreachable store (503 / `UNAVAILABLE`).

## Go SDK

```go
att := client.Security().Attestation()
att.PutEndorsement(ctx, &quicbaas.AttestationPutEndorsementRequest{Name: "ak-ca", Kind: quicbaas.AttestationEndorsementAKRoot, CertificatePEM: caPEM})
att.PutEndorsement(ctx, &quicbaas.AttestationPutEndorsementRequest{Name: "rvp", Kind: quicbaas.AttestationEndorsementReferenceSigner, CertificatePEM: rvpCAPEM})
att.PutReferenceValues(ctx, "golden", signedCoRIM)
result, err := att.AppraiseQuote(ctx, &quicbaas.AttestationAppraiseQuoteRequest{
    AKCertificatePEM: akPEM, Quote: attest, Signature: sig, Nonce: nonce, PCRs: pcrs, ReferenceValues: "golden",
})
```

Types: `sdk/go/client/attestation` (`Endorsement`, `ReferenceValues`,
`AppraiseQuoteRequest`, `AttestationResult`, …), aliased in the root package as
`Attestation*`. Evidence primitives for an embedder that appraises locally:
`sdk/go/xkms/evidence`.
