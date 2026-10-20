### Fixed

- The gRPC `FederationService/BeginFederatedLogin` and `HandleFederatedCallback` RPCs are public, matching the REST `/api/v1/auth/providers/{tenant_id}/{provider_name}/{login,callback}` routes, so an anonymous caller can begin a federated login (#1621).
- The embedded transport returns transaction results in request order and refuses a branch that names the same operation twice, so a failed operation's index is its request position (#1621).
- The in-memory federation provider store lists providers in ID order, so offset paging neither skips nor repeats a provider (#1621).
