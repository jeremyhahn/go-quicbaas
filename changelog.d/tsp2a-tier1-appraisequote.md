### Changed

- **Tier 1 host attestation is appraised by the Verifier (go-quicbaas#1346,
  `measured-launch`).** `TPMAttestingRuntime` no longer compares PCRs itself.
  It quotes the registers the named reference values cover, binds the PCR
  read to the signed quote and hands the evidence to the Verifier's
  `AppraiseQuote`. The Verifier checks the AK certificate against the tenant's
  `ak-root`, the quote signature and the PCRs against the signed reference
  values. The operator must store the `ak-root` and `reference-signer`
  endorsements and the reference values before an `attested` launch. Reference
  values for a `measured` launch must include the measurement PCR (16 by
  default), because the comparison is an equal set.
- **`NewTPMAttestingRuntime(base, tpm, appraiser, logger)` takes a
  `HostAppraiser`** (`service.NewHostAppraiser(store)`); a nil appraiser is an
  `InputValidationError`. `HostTPM` gains `IAKCertificate()`, which returns
  the certificate of the key that signs the quote.

### Removed

- **`ExpectedPCRs` / `expected_pcrs` is replaced by `ReferenceValues` /
  `reference_values`**, the name of the stored reference values, on
  `ContainerSpec`, the session spec, `AttestContainer` (REST, gRPC, MCP, SDK)
  and `SpawnSession`. There is no fallback. Proto fields
  `AttestContainerRequest` 7 and `SpawnSessionRequest` 25 are reserved.
- **`PCRMismatchError` is removed.** A host the Verifier does not verify
  fails with `HostAppraisalError`. It carries the failed checks and the PCR
  divergences, unwraps to `ErrHostNotVerified`, and also unwraps to
  `ErrPCRMismatch` when a PCR diverged.
