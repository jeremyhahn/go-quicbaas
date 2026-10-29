### Fixed

- The node's TCP gRPC port now serves AgentCatalogService, SDLCService, RegistryService, FederationService and SessionService, which previously answered Unimplemented there while the Unix socket served them; they run behind the same verified client-certificate tenant binding as every other quicbaas service. IAMService, PEPService, MFAService and AuditService stay Unix-socket only until the authz ruling (#1696).
- FederationService and SessionService answer Unavailable until IAM initialisation completes, instead of serving, for the life of the process, a server built with no federation manager and an empty session store; once IAM is ready the TCP port and the Unix socket share one set of instances (#1696).
