### Added

- Go SDK: `Security().TrustedCA()` returns the re-exported go-xkms `CAService` (`TrustedCAService`), with go-xkms's own request and response types on the `quicbaas.TrustedCA` transport service; over REST it reaches the server's go-xkms CA routes under `/api/v1/xkms/ca/*` (#1330).
