### Added

- `quicbaas trusted-ca` (and `quicbaas trusted-ca tcg`) drive the go-xkms CA and TCG CA re-exports, `Security().TrustedCA()` and `Security().TCGCA()`, on every transport; `make integration-test-certificate-authority` runs the row's CLI suites (#1661).
