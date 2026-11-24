### Changed

- **The TypeScript SDK's gRPC-Web transport refuses every verb with
  `GrpcWebNotImplementedError`** (#1690). The error is now a
  `TransportNotSupportedError` naming the family and verb, and projects, sdlc
  and accounts reject with it too. Its constructor now takes
  `(family, method, reason?)`; the package is alpha, so this breaking change
  ships without a deprecation period. The QUIC transport's refusals name
  WebTransport over HTTP/3 (draft-ietf-webtrans-http3-16) as the adopted path.
