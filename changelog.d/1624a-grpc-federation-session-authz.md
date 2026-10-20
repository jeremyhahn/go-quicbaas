### Fixed

- **gRPC FederationService and SessionService take the grants REST requires
  (go-quicbaas#1624).** Under `RequireAuthz` the FederationService provider
  and account-link RPCs (`CreateProvider`, `GetProvider`, `ListProviders`,
  `UpdateProvider`, `DeleteProvider`, `LinkAccount`, `GetUserLinks`,
  `UnlinkAccount`) and every SessionService RPC were unmapped, so the gRPC
  gate refused them for every caller. Each now carries a
  `(qrdb.v1.permission)` annotation and takes the same grant as its REST
  route: `tenants:create`, `tenants:read` or `tenants:delete` for the
  federation RPCs, `users:read` or `users:update` for the session RPCs.
  `BeginFederatedLogin` and `HandleFederatedCallback` stay public.
