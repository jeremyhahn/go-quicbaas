### Fixed

- The embedded transport routes `quicbaas.Orchestrator/GetSessionHistory` and
  the `quicbaas.Orchestrator/SubscribeSessionEvents` stream to the session
  event log the gRPC transport reads, in the tenant the call is bound to; a
  request naming another tenant is refused, and a session with no metadata
  and no logged event is not found (#1565).
