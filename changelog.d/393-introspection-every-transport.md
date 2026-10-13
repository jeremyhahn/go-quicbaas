### Added

- **Token introspection on every transport (go-quicbaas#393).**
  `Security().Auth().IntrospectToken` now works over REST, QUIC, gRPC, the
  Unix socket, MCP and the embedded transport. Every transport returns the
  same `service.TokenIntrospection`, including `AttestLevel` and
  `AttestAAGUID`. An inactive token returns only `Active=false`, and a missing
  token is an invalid-input error. gRPC gains the `IntrospectToken` RPC and
  MCP the `quicbaas_introspecttoken` tool; both need an authenticated caller
  with `user_authfactors:login`. REST and QUIC call `POST /oauth2/introspect`,
  which still requires mTLS or client credentials.
