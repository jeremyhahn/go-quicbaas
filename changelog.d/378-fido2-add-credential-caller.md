### Added

- **FIDO2 `BeginAddCredential` adds a passkey to the caller's own account.**
  It takes no request: the owner is the authenticated caller, looked up in its
  own tenant's user store by username for mTLS and by the verified `uid`
  claim for a quicbaas-issued bearer token. A token's `sub` (the login's email
  or username) never names an account by ID, so an all-digit username cannot
  reach the account whose ID it spells. A caller with no account in its tenant
  is refused. Served on REST
  (`POST /api/v1/xkms/fido2/credentials/begin`), gRPC, MCP, QUIC, Unix and
  embedded, and used by `AdminFactors().Enroll` for fido2 factors
  (go-quicbaas#378).

- **`AdminFactors().Enroll` enrolls piv on the caller's own card slot.** The
  request's `PIVGenerator` holds the slot key and `PIVCertStore` the slot's
  certificate; `Enroll` never generates a key of its own, and a piv plan
  missing either is refused before any state changes
  (`AdminFactorUnavailableError`). The CSR is built with go-xkms
  `CreateCSRWithKey`, and the issued certificate is installed in the slot
  before the factor is persisted, so the card presents the certificate the
  factor is bound to (go-quicbaas#378).

- **`AdminFactors().Enroll` binds the request's `TenantID` for every factor.**
  PIV certificate issuance and the piv factor write act in the requested
  tenant even from an unbound context, as the passkey ceremony does; a context
  already bound to a different tenant is refused with a conflict before
  anything is issued or stored (go-quicbaas#378).

### Fixed

- **REST and QUIC `FIDO2().FinishRegistration` report the credential ID.** The
  REST transport finished registration on the IAM WebAuthn route and filled
  `CredentialID` with the user ID; it now posts to
  `POST /api/v1/xkms/fido2/register/finish`, the FIDO2 service gRPC, MCP, Unix
  and embedded already used, and returns the attested credential's ID
  (go-quicbaas#378).
