### Added

- The Go SDK re-exports go-xkms's `TCGCAService` as `Security().TCGCA()`
  (EK and AK certificates, TCG-CSR signing, device enrollment). The REST
  transport, and QUIC as REST over HTTP/3, carry it to the server's
  `/api/v1/xkms/ca/tcg/*` routes (#1330).

### Fixed

- `sdk/go/service` aliases `TrustedCAService`, which the SDK's alias-closure
  guard requires now that the server composes the go-xkms CA (#1330).
