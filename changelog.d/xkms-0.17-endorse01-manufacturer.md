### Changed

- **go-xkms v0.17.0-alpha.** It carries the endorse-01 fix (go-xkms#634): an
  EK certificate issued for a TPM with no manufacturer EK certificate now
  records the TPM's tcg-at-tpmManufacturer code where a later enroll-01
  reads it. Before this, every IDevID enrollment of such a TPM failed with
  `500 Failed to sign TCG-CSR-IDEVID`. The endorse-01 client asserts the
  code from `TPM_PT_MANUFACTURER`; the server validates it and holds it to
  any code the account's EAB binds. The release also brings the go-xkms
  compat cleanups and the protected-memory batches (go-xkms#587).

### Fixed

- **The cluster CA passphrase is sealed at the composition root.** go-xkms
  v0.17 holds `clustercasdk.KeyProtection.Passphrase` as a `*secure.Secret`.
  `Server.clusterCAKeyProtection` now seals `ca_key_passphrase` into an
  enclave, and when protected memory is refused it fails the cluster CA
  (`ClusterCAPassphraseError`) instead of running with passphrase
  protection off.
- **The FIDO2 MCP tools take the login key as `email`.** go-xkms v0.17
  removed the `username` wire alias (go-xkms#628), so the SDK's MCP
  transport, which forwards the go-xkms request unchanged, sends only
  `email`. `xkms.fido2.beginRegistration`, `beginAuthentication` and
  `finishAuthentication` required `username` and would have refused every
  SDK call. Their schemas and handlers now use `email`, and a regression
  test drives them with SDK-serialized requests. **Breaking** for any other
  MCP client that still sends `username`.
