### Changed

- **IAM App Update applies every field it carries (go-quicbaas#1466).**
  `AppManager.UpdateAppInTenant` now applies `Scopes`,
  `TokenEndpointAuthMethod` and `MTLSCertThumbprint`; a new thumbprint
  replaces the old pin in the same write, so the old certificate stops
  binding at once, and clearing the pin of a `self_signed_tls_client_auth`
  client is refused. Redirect URIs and grant types are validated as on
  create. Fields `Application` does not store (description, contacts,
  TTLs, JWKS, metadata, enabled, PKCE and the rest) are refused with
  `AppFieldNotStoredError` instead of being silently dropped.

- **App Update carries every field over REST, gRPC and MCP (go-quicbaas#1466).**
  The REST handler and the MCP `quicbaas_iam_app_update` tool decode into
  `service.UpdateAppRequest`, whose `Options()` maps every field to the app
  manager. The gRPC `UpdateAppRequest` gains fields 8–23 (description,
  post-logout redirect URIs, scopes, token endpoint auth method, contacts,
  logo/policy/ToS/JWKS URIs, JWKS, the three token TTLs, metadata, enabled and
  the mTLS certificate thumbprint), and the Go SDK gRPC client sends them. A
  field the app cannot store comes back as invalid input on every transport:
  HTTP 400, gRPC `InvalidArgument`, MCP invalid input.

- **App Update over the embedded transport carries every field (go-quicbaas#1466).**
  The embedded handler decodes the SDK request the way the MCP tool does,
  so scopes, the auth method, a thumbprint re-bind and every other field
  take effect, and a field the app cannot store is refused. An embedded Get
  now returns the app's resource prefix, grant types and scopes.

- **Breaking (Go SDK): `iam.UpdateAppRequest.TokenEndpointAuthMethod` is now
  `*TokenEndpointAuthMethod` (go-quicbaas#1466).** Nil leaves the method
  unchanged; a pointer to `TokenEndpointAuthClientSecretBasic` selects
  `client_secret_basic`, which the zero-valued field could not express.

### Fixed

- **An unsupported grant type is invalid input (go-quicbaas#1466).** App
  create and update now return a coded `INVALID_INPUT` error for a grant
  such as `password`, so REST answers 400 and gRPC `InvalidArgument`
  instead of 500 / `Internal`.

### Security

- **App Update scopes pass the grantor gate (go-quicbaas#1466).** App Update
  accepts permission scopes only when the caller holds every scope it
  submits, including scopes the app already has, as resolved from the
  caller's qrdb effective permissions in the app's tenant. Otherwise the
  update is refused (HTTP 403, gRPC `PermissionDenied`, MCP permission
  denied) and the app is unchanged. An unresolvable caller is refused the
  same way; an update without scopes is unaffected.
