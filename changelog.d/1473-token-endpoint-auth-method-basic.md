### Fixed

- **The SDK can register and update an app with `client_secret_basic`
  (go-quicbaas#1473).** `RegisterAppRequest` and `Application` now always
  serialise `token_endpoint_auth_method`, so `client_secret_basic`, the
  enum's zero value, is no longer dropped from a register body or from a
  decoded app. `UpdateAppRequest.TokenEndpointAuthMethod` is a pointer: an
  update that names `client_secret_basic` moves the app back to it, and an
  update that omits the field leaves the method unchanged.
- **Register stores `token_endpoint_auth_method` on every transport
  (go-quicbaas#1473).** REST, gRPC, Unix, QUIC, MCP and the embedded
  transport now carry the method into the stored app, and Get reads it back.
  Register checks the method with the same validator as update: an
  unsupported method is refused with `ErrInvalidTokenEndpointAuthMethod`
  (400 on REST, `InvalidArgument` on gRPC). When the method is omitted the
  app is stored as `client_secret_basic` (RFC 7591 §2). Register has no
  certificate thumbprint field, so `self_signed_tls_client_auth` is refused
  with `ErrMTLSThumbprintRequired`; register with another method, then
  switch to it through update with the thumbprint.
- **Dynamic client registration stores the method it answers with
  (go-quicbaas#1473).** `POST /oauth2/register` passes `token_endpoint_auth_method` to register and reports the stored method, as RFC 7592 reads do; a method register refuses (including `none` and `private_key_jwt`, which it never supported) is `invalid_client_metadata` (400).
