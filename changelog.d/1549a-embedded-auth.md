### Fixed

- **The embedded transport routes the quicbaas.Auth federation, provider,
  session and discovery verbs** (#1549). The SDK auth client sent
  BeginFederatedLogin, HandleFederatedCallback, ListProviders, GetProvider,
  GetSession, ListSessions, RevokeSession, RevokeAllSessions, GetJWKS and
  GetOpenIDConfiguration, and the embedded transport answered each with
  `UnknownMethodError`. Each verb now reaches the same federation manager,
  session manager or OIDC provider as the REST and gRPC servers. The session
  verbs are self-scoped: if a subject is authenticated, another user's session
  is denied. ListSessions and RevokeAllSessions also refuse a call that has no
  authenticated subject. ListProviders and GetProvider refuse a tenant other
  than the one the call is bound to, and a typed-nil federation request is
  refused as an invalid request instead of panicking.
