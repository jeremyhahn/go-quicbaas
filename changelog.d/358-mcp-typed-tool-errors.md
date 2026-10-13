### Fixed

- **MCP tool errors keep their typed code (go-quicbaas#358).** A refused MCP
  tool call used to reach the SDK caller as an internal error, whatever the
  service said. For example, tenant provisioning refused as forbidden or
  already-existing, or a missing secret, all arrived as `INTERNAL`. The
  server now puts the MCP error code in the tool result's
  `structuredContent` (`{"code": N}`), and the SDK MCP transport returns the
  matching `sdk/go/errors` code. An error the server cannot classify is still
  internal. Tenant provisioning in `cert` mode is now also tested on gRPC.
