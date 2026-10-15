### Added

- **An MCP interop leg driven by an independent client (go-quicbaas#203).**
  `pkg/api/mcp/interop_official_client_test.go` serves the module tool
  surface over streamable HTTP on a loopback listener and drives it with the
  official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk`, test-only).
  It asserts the initialize handshake (negotiated protocol version, server
  info, tools capability), `tools/list` against the server's tool registry,
  a `tools/call` result, and a JSON-RPC error for an unknown tool. Until now
  every MCP test was mark3labs/mcp-go talking to itself. The interop claims
  audit and the MCP claim sentences in `README.md` and
  `docs/mcp-server/api.md` now say what this leg proves.
