# Attestation — Engineering

> **Status: NORMATIVE** · Last verified: 2026-10-01
> Build, test and operate. Feature folder: `docs/attestation/`.

## Tests

Every Verifier layer is tested with **real evidence** built by
`pkg/service/attestationtest`: an AK CA and an AK certificate carrying
tcg-cap-verifiedTPMRestricted, a COSE_Sign1 signed CoRIM, and a TPMS_ATTEST
quote signed by the AK — the go-xkms primitives run for real, never through a
double. `attestationtest.MemoryStore` is the in-memory `AttestationStore`.

| Scope | Where |
|---|---|
| Service (appraisal, TOFU refusal, divergence, revoked signer, tenancy, store failures) | `pkg/service/attestation_verifier_test.go` |
| gRPC server | `pkg/api/grpc/server_attestation_verifier_test.go` |
| REST server | `pkg/api/rest/handlers_attestation_verifier_test.go` |
| MCP server | `pkg/api/mcp/attestation_verifier_mcp_test.go` |
| SDK end to end over the embedded transport | `sdk/go/services_attestation_verifier_test.go` |
| SDK transports (what the server received) | `sdk/go/transport/{grpc,rest,mcp,unix}/…verifier…_test.go` |
| CLI (real SDK client, embedded transport, real Verifier) | `cmd/quicbaas/attestation_test.go` |
| Integration, every transport, live server | `test/integration/cli/attestation_verifier_test.go` |

```bash
make test-attestation
make coverage-attestation
make bench-attestation              # ~0.75 ms per appraisal on a 24-core host
make integration-test-attestation   # devcontainer
```

## Operating notes

- Install the `ak-root` endorsement for the CA that issues attestation-key
  certificates and a `reference-signer` for each reference-value provider
  before publishing reference values; until then `PutReferenceValues` refuses.
- Reference values are checked against the current endorsements at every
  appraisal: rotating a provider's CA means storing the new endorsement before
  deleting the old one.
- A CoRIM's validity window is enforced at both storage and appraisal time; an
  expired CoRIM stops verifying and the baseline check reports `absent`.
