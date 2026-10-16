### Fixed

- **`pkg/apimodule` compiles with only the `mcp` tag (go-quicbaas#1412).**
  `SurfaceComposingError` was declared in the `rest`-tagged
  `module_http.go` but also returned by the `mcp`-tagged `module_mcp.go`, so
  an MCP-only build failed with `undefined: SurfaceComposingError`. The type
  now lives in the untagged `pkg/apimodule/errors.go`; its fields, message
  and `ErrUnavailable` classification are unchanged.
