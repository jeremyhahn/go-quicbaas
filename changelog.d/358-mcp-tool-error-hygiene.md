### Fixed

- **MCP tool errors no longer leak server-side failure text (go-quicbaas#358).**
  A tool call that failed for a server-side reason (storage, raft, internal)
  returned the full error text to the MCP client, including paths and wrapped
  store errors. MCP now applies the REST rule, from one shared classification
  (`errors.DescribesRequest`): an error that describes the request keeps its
  message, and any other coded error answers `the request could not be
  completed` with its cause logged server-side. The error code is unchanged.
- **MCP sealed-window and server-composing refusals are typed (go-quicbaas#358).**
  Both used to reach the SDK caller as `INTERNAL`. They now carry the MCP
  service-unavailable code, which the SDK returns as `UNAVAILABLE`, as REST
  (503) and gRPC (`Unavailable`) already did.
