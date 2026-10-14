### Added

- **`sdk/go/tenantctx` re-exports `pkg/tenantctx` (go-quicbaas#1171).** A
  host that may import only the SDK binds and reads the request tenant as the
  server does: `WithTenant`, `FromContext` and `Target`. A context with no
  tenant, or a request naming another tenant, is still refused with
  `ErrForbidden`.
- **`sdk/go/xkms` re-exports `TPMPolicyPlatform` (go-quicbaas#1171)**, the
  name of the built-in TPM policy every scope has.
- **`sdk/go/xkms/acme` re-exports `ACMETPMModule` and `ACMEXKMSTPMAdapter`
  (go-quicbaas#1171)**, the types `NewACMEXKMSTPMAdapter` builds, so a
  consumer can hold and pass the TPM enrollment adapter without importing
  go-xkms.
