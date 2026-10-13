### Fixed

- **The global REST rate limit now covers module routes (go-quicbaas#158).**
  The routes the unified API server serves through the quicbaas service
  module skipped the global limiter, so a client could exceed its per-IP
  limit by calling them. They now share the core router's limiter. Each
  client has one budget across the whole REST surface and gets the same 429
  on every route. Module routes resolve the client address with the same
  `server.trusted_proxies` rules as the core routes.
