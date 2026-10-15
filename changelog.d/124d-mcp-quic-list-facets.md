### Fixed

- **The Go SDK account list keeps its facets over MCP (go-quicbaas#124).**
  `account.Client.List` over the MCP transport now returns the facet counts,
  applied limit and offset, and next offset that the
  `quicbaas_agent_account_list` tool already sends. Before this fix it kept
  only the accounts and the total. The QUIC transport is the REST client
  over HTTP/3, so it already carried every list's facets.
- A cross-transport parity test now lists projects, accounts, tasks, jobs,
  fleet nodes, sessions and workflows over REST (which also covers the QUIC
  wire), gRPC, unix, MCP and embedded. For each list it checks that every
  transport returns the same facets, and that a list with no counted facets
  returns nil facets. One pair is not covered yet: workflows over gRPC and
  unix.
