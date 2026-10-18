### Fixed

- **The embedded transport routes the quicbaas.IAM Federation verbs** (#1556).
  The SDK IAM client sends `CreateProvider`, `GetProvider`, `UpdateProvider`,
  `DeleteProvider` and `ListProviders` to `quicbaas.IAM.Federation`, and the
  embedded transport answered each with `UnknownMethodError`. Each verb now
  calls the `iam.FederationManager` that the REST and gRPC servers call. As on
  gRPC, a request naming a tenant other than the bound one is refused before
  the manager is reached. The client secret is never returned.
