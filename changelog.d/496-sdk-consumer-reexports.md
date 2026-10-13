### Added

- **Go SDK re-exports for config validation and trust bootstrap
  (go-quicbaas#496, #519, #382, #440).** A consumer that may import only
  `go-quicbaas/sdk/go` now reaches these upstream primitives as pure aliases,
  instead of keeping a copy that drifts:
  - `quicbaas.ParseAdminFactors`, `AdminEnrollmentPlan`, `AdminFactor` and
    `UnsupportedAdminFactorError` (go-qrdb), to validate
    `auth.admin_factors` at config load.
  - `sdk/go/xkms/ssh/knownhosts.ValidateFingerprint` and
    `InvalidFingerprintError` (go-xkms), the OpenSSH `SHA256:` fingerprint
    check.
  - `sdk/go/xkms/trust/fetcher.DefaultBundlePath` (`/v1/ca/bootstrap`) and
    `DefaultTimeout` (go-xkms), the CA-bundle fetcher defaults.
