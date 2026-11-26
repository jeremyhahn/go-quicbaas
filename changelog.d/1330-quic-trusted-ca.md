### Added

- The QUIC SDK transport carries `Security().TrustedCA()` as REST over HTTP/3
  to the node's `/api/v1/xkms/ca/*` routes; a test pins every go-xkms
  `CAService` verb (#1330).
