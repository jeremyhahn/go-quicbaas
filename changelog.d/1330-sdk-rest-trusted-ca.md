### Added

- Go SDK REST transport: the `quicbaas.TrustedCA` service (the re-exported go-xkms `CAService`: `GetCABundle`, `GetCACertificate`, `SignCSR`, `IssueCertificate`, `RevokeCertificate`, `GenerateCRL`, `IsRevoked`) reaches the server's go-xkms CA routes under `/api/v1/xkms/ca/*` with go-xkms's own request and response types, instead of the unserved `/api/v1/TrustedCA/{method}` (#1330).
