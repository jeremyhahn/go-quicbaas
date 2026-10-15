### Changed

- **The TPM simulator tests run in every unit pass (go-quicbaas#1346,
  `measured-launch`).** `tpm_simulator` moved from `TAG_GATE_EXEMPT` into
  `TEST_EXTRA_TAGS`, so `make test`, the per-package targets and the CI unit
  job compile and run the go-xkms quote-to-Verifier proof and the TPM2 seal
  tests. The simulator is C built through cgo; the unit pass needs a C
  compiler. `swtpm` and `pkcs11` stay exempt.

### Fixed

- **The `pkg/server` TPM2 seal simulator tests pass again.** go-xkms parents
  every object its TPM key backend seals or creates under the Platform SRK
  (0x81000002), which `TPM2.Provision` does not create, so
  `NewBackendWithTPM` failed with `TPM_RC_HANDLE`. `tpmsim.Provision` now
  creates a platform-policy-bound Platform SRK, as go-xkms does for a fresh
  simulator. The round trip no longer returns early on that error.
- **`TestBuildTPM2Strategy_FactoryDispatch_Simulator` closes its backend.**
  It held the process-wide simulator open, so the next simulator test blocked
  until the package timeout.
