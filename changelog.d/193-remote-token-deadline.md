### Fixed

- **A remote daemon call's token fetch honours the caller's context
  (go-quicbaas#193).** `RemoteACPBackend` took its bearer token from an
  `oauth2.TokenSource`, which has no context, so a slow token endpoint held
  Spawn, Prompt, Status, Close and every other remote RPC, and the REST or
  gRPC handler waiting on it, past the caller's deadline. The backend now takes
  a `RemoteTokenSource` whose `Token(ctx)` returns when the caller's context
  ends. The central's token source reuses a valid cached token and shares one
  in-flight exchange among concurrent callers, without holding a lock across
  the network call. It builds the request with `http.NewRequestWithContext`.
  A token that cannot be had now fails the call with `RemoteBackendTokenError`
  (`ErrRemoteBackendToken`). Before, the call was sent without a token.
