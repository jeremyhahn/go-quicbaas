### Fixed

- **REST responses refuse server numbers they cannot hold (#559).** A
  count, total, version, size, uptime or shard count that did not fit the
  REST response field was narrowed by a bare cast and reported as a wrong,
  often negative, value. It is now a sanitized internal error (500), with
  the `conv` range error kept as the internal cause. A vector cluster total that
  would wrap `uint64` is refused the same way. Cache size gauges still
  saturate at the field's maximum, as documented. A negative transaction
  compare version and a vector namespace dimension beyond `uint32` are
  refused as invalid input (400) instead of wrapping.
  `conv.SafeSecondsToDuration` is new.
- **MCP secrets watch and integer parameters are range checked (#559).**
  The secrets watch `timeout_seconds` and integer parameters given as
  floats are refused when out of range instead of wrapping. MCP and prompt
  record ids hash through the shared `id.HashString` helper instead of
  discarding the `hash.Hash.Write` result.
- No lint suppression directive remains in `pkg/api/rest`. The secrets
  watch REST client and the session events stream test now close
  response bodies where the close is visible, and a failed status check
  no longer leaks the body.
