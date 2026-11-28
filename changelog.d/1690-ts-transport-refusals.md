### Changed

- **The TypeScript SDK's gRPC-Web transport refuses every verb with
  `GrpcWebNotImplementedError`** (#1690). The error is now a
  `TransportNotSupportedError` naming the family and verb, and projects, sdlc
  and accounts reject with it too. Its constructor keeps its single-argument
  form `new GrpcWebNotImplementedError(method)`; a `family.verb` argument names
  that family and verb. The QUIC transport's refusals name WebTransport over
  HTTP/3 (draft-ietf-webtrans-http3-16) as the adopted path.
