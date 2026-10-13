### Fixed

- **MCP tools refuse whole-number arguments they cannot hold (#559).** A
  limit, offset, threshold, page size, version, dimension or similar
  argument that is negative, NaN, or too large for the request field was
  narrowed by a bare cast, wrapping to a wrong value. It is now refused as
  invalid input (`invalid <argument>`, `MCPInvalidInput`), with the `conv`
  range error as the cause. List page caps (KMS keys, DEKs, vector listing)
  still saturate at the cap, as documented. A KMS key version the response
  cannot hold is reported as an internal error instead of a wrong version.
  `conv.SafeFloat64ToUint64` is new. No lint suppression directive remains
  in `pkg/api/mcp`.
