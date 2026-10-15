### Changed

- **A `sha1` measurement bank is refused at spec validation (go-quicbaas#1346,
  `measured-launch`).** The Verifier does not parse a SHA-1 PCR selection, so
  a launch measured into that bank could never be appraised.
  `measurement_bank` now accepts `sha256`, `sha384` or `sha512`, and `sha1` is
  an `InputValidationError`.
- **The Verifier computes the reference baseline in the quoted bank's hash.**
  It no longer uses the quote's signing hash, which can differ: a SHA-256 AK
  can sign a quote over the SHA-384 bank.
- **go-xkms `*tpm2.TPM2` is asserted to satisfy `HostTPM` at compile time.**
  Its IAK needs an enrolled certificate in `tpm2.Params.CertStore` before
  `IAKCertificate()` works, because go-xkms provisioning issues none.
- **Host appraisal replays the boot event log over every quoted register except
  the measurement PCR.** A measured launch on a host that supplied its event
  log was refused, because the runtime's extension is never in that log. The
  signed reference values judge the measurement PCR, and log events for it are
  ignored.
- **The runtime quotes only the reference PCRs in its IAK's bank.**
  `HostAppraiser.ReferencePCRs` takes the bank hash and returns the registers
  the signed reference values cover in that bank, not the union of banks.
  Reference values with none in the bank are a `ReferenceBankUncoveredError`
  before any quote. `HostTPM` gains `IAKAttributes`, which go-xkms
  `*tpm2.TPM2` already provides.

### Added

- **Real-producer tests for Tier 1 and Tier 2 (build tag `tpm_simulator`).**
  They drive a provisioned go-xkms TPM on the simulator through
  `TPMAttestingRuntime` and the Verifier. They cover a matching baseline, a
  changed PCR, a quote over another nonce, and an AK certificate that does
  not chain to the tenant root. `pkg/testutil/tpmsim` holds the shared
  simulator provisioning. `attestationtest.CertifyAttestationKey` certifies a
  key the caller already holds, such as a TPM's IAK.
