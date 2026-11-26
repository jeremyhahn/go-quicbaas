### Added

- The gRPC and Unix SDK transports serve `Security().TrustedCA()` through the
  new `quicbaas.trustedca.v1.TrustedCAService`, which the server answers from
  the same go-xkms CA the REST `/api/v1/xkms/ca/*` routes and the MCP
  `xkms.ca.*` tools serve (#1330).
