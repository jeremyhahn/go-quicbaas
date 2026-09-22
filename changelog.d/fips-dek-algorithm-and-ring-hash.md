### Fixed

- **Envelope field encryption no longer fails closed on a FIPS-activated
  host.** The AES-128 path built its AEAD with `cipher.NewGCM` and supplied
  its own nonce, which the Go Cryptographic Module refuses under
  `GODEBUG=fips140=only` ("use of GCM with arbitrary IVs is not allowed"), so
  every encrypted field failed on exactly the regulated hosts the FIPS build
  exists for. It now uses `cipher.NewGCMWithRandomNonce`, the approved
  construction, which prepends the same 12-byte nonce to the same
  ciphertext — fields written by either form interoperate and no migration is
  needed.

### Added

- **A FIPS-activated process refuses an unapproved DEK algorithm instead of
  using it.** ChaCha20-Poly1305 stays a data-encryption-key option, and FIPS
  activation is what takes it away (owner ruling 8): `kms.defaults.algorithm`
  is refused at startup, and `CreateDEK` / `RotateDEK` are refused at the
  call, both with a typed `*fips.AEADNotApprovedError` naming the operation,
  the algorithm and the approved set. Nothing is silently substituted — a
  deployment that asked for ChaCha20 and got AES without being told would hold
  a key its configuration does not describe.
- **SHA-256 is selectable for consistent-hash placement.** blake2b remains the
  ring default — placing a key decides which node owns it, which is not a
  cryptographic security function — and `hash.Select` now resolves a
  configured name ("blake2b", "sha256", "fnv1a") to its function, with a typed
  `*hash.UnknownAlgorithmError` for anything else so a ring is never built
  with a hash other than the one that was asked for.
- **`make test-fips-conformance`** runs login, envelope encryption, the DEK
  gate, placement hashing and config validation under `GODEBUG=fips140=only`,
  where the module refuses unapproved algorithms rather than deprioritizing
  them.
