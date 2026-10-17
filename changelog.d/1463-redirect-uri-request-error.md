### Fixed

- **An app update now checks its redirect URIs as registration does
  (go-quicbaas#1440).** `AppManager.UpdateAppInTenant` stored new redirect URIs
  unchecked, so updating an app could store a URI with a fragment, or one with
  a scheme registration refuses. Registration and update now share
  `validateRedirectURIs`, which runs before any field is applied, so a refused
  update returns the registration error and leaves the app unchanged.
- **A refused redirect URI is a request error on every transport
  (go-quicbaas#1463).** Registering or updating an app with a redirect URI that
  `validateRedirectURIs` refuses was answered as a server fault. The refusal is
  now the typed `iam.InvalidRedirectURIError`, carrying the URI and the reason;
  REST (and QUIC) answer it 400, gRPC (and Unix) `codes.InvalidArgument`, and
  MCP the invalid-input class, each naming the reason. A fragment refusal
  still carries `RedirectURIFragmentError` as its cause.
