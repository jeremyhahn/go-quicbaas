### Changed

- **The TypeScript SDK's projects, sdlc and accounts clients now exist on every
  transport (go-quicbaas#809).** QUIC carries them as the REST dialect over
  HTTP/3. MCP carries them over the server's `project_*`, `sdlc_*` and
  `quicbaas_agent_account_*` tools. An argument a tool does not read is refused
  rather than dropped: `metadata`, `clear_description`, the `autonomous`
  project filter and the `status` account filter. gRPC-Web, Unix and Wails
  cannot carry these families yet. On those transports every verb rejects with
  the new `TransportNotSupportedError`, which names the transport, family,
  method and reason; the field is no longer `null`. Each reason is recorded in
  `docs/software-factory/api.md`. MCP tool failures surface as the new
  `McpError`.
