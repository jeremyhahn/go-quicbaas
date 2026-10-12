### Added

- **Every fleet node record carries `backends` on every transport
  (go-quicbaas#700).** It lists the primary backend first, then each
  capability, each name once, and is `[]`, never `null`. One renderer,
  `FleetNode.ServedBackends`, serves REST, QUIC, gRPC (`FleetNode.backends`,
  field 16), Unix, MCP, the embedded transport and both SDKs. The Go SDK's
  `ListFleetNodes` now reaches the MCP `quicbaas_fleet_list` tool.
- **The tasks listing can ask which task holds a source reservation
  (go-quicbaas#730).** The `source_kind` + `source_ref` filter is answered from the
  reservation index, on REST, QUIC, gRPC, Unix, embedded and both SDKs.
- **A prompt response carries a per-turn tool census beside `Usage`
  (go-quicbaas#799).** `PromptResponse.ToolUse` counts the tool calls the turn
  made, by tool name.
