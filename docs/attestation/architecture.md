# Attestation — Architecture

> **Status: NORMATIVE** · Last verified: 2026-10-01
> Design, component boundaries and data flow. Feature folder: `docs/attestation/`.

## Roles (RFC 9334)

Handbook doc 00 (L1 amendment of 2026-10-01) splits attestation by role:

| Role | Owner | Here |
|---|---|---|
| Evidence primitives (quote, AK/EK chains, event log, TPM2_Certify, signed CoRIM) | go-xkms | consumed from `pkg/attestation/{evidence,refvalue}` |
| **Verifier** (appraisal, reference-value and endorsement store, attestation results) | **go-quicbaas** | `Security().Attestation()` — this feature |
| Attester, Relying Party, Reference-Value Provider | go-trusted-platform | consumer |

`Security().Attestation()` has three groups of verbs:

1. **Statement verification** — `Verify`, `VerifyChain`, `VerifyImageSignature`
   (client-side over `Verify`): a pure verifier over go-xkms's attestation
   verifier.
2. **Container attestation seam** — `AttestContainer` forwards to a registered
   `ContainerAttestor` (`WithContainerAttestor`).
3. **The Verifier** — endorsements, signed reference values and quote
   appraisal, below.

## The Verifier

### Endorsements

Trust anchors, stored per tenant:

- `ak-root` — a CA certificate attestation-key certificates must chain to.
- `reference-signer` — a CA certificate under which a CoRIM signer chains (via
  the COSE `x5chain` header), or a pinned public key the signature must verify
  under.

### Reference values

A reference-value provider publishes a **COSE_Sign1 signed CoRIM**. The
Verifier stores it only if it verifies against a stored `reference-signer`
(`refvalue.VerifySigned`). **There is no trust on first use**: a device's own
report never becomes its baseline, which is the defect go-trusted-platform's
`BaselineSource=tofu` path had (handbook 13 G10). The signature is checked again
at every appraisal, so deleting an endorsement withdraws every baseline it
signed.

### Appraisal (`AppraiseQuote`)

| Step | Primitive | Outcome on failure |
|---|---|---|
| AK certificate chains to an `ak-root` and carries tcg-cap-verifiedTPMRestricted | `evidence.VerifyAKCertificate` | forged (absent with no `ak-root`) |
| Quote: TPM-generated, signature under the AK, nonce in `extraData`, reported PCRs hash to the signed digest | `evidence.VerifyQuote` | forged (absent if not evaluable) |
| Event log (optional) replays to the quoted PCRs | `evidence.VerifyEventLog` | forged; skipped when not sent |
| Proven PCRs equal the signed reference values (any listed digest per register) | Verifier comparison over `Manifest.PCRDigests` | forged on divergence; absent on a set mismatch or no baseline |

The **attestation result** reports each check (`pass`, `forged`, `absent`,
`skipped`), `verified` (every check passed), `cryptographic_failure` (some
evidence was present and wrong — grounds for quarantine, as opposed to missing
evidence, which is grounds only for denying service), the baseline's identity
(`reference_values`, `manifest_id`, `signer`) and `baseline_source: signed`.

### Storage

Records are opaque blobs (certificates, keys, signed CoRIMs) in qrdb KV on the
platform shard under `__sys:attest-t/{tenant}/{endorsement|refval}/{name}`
(`__sys:attest/…` for the system tenant) — handbook data-access §3 case 3. The
tenant is the authenticated tenant from the request context (`pkg/tenantctx`):
no request field names one, and a request with none bound is refused
(`ErrForbidden`) rather than served as the system tenant. The system tenant is
reached only by binding it explicitly (`tenantctx.WithSystem`; the embedded
transport does so for an in-process caller that names no tenant). An embedder
supplies any `AttestationStore` with `WithAttestationStore`.
