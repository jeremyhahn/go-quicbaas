### Changed

- **A node listens on its advertised Raft host by default
  (go-quicbaas#185).** With neither `raft.listen_address` nor
  `raft.listen_host` set, the Raft transport binds `raft.raft_address` itself,
  so `raft_address: node1:63001` binds `node1:63001`. No default binds
  `0.0.0.0` or `::` any more; `raft.listen_host` (for example `0.0.0.0`) and
  `raft.listen_address` (for example behind NAT) are the explicit opt-ins and
  are honoured as given. An empty or wildcard advertised host with no listen
  setting fails validation with `RaftAdvertisedHostError` instead of being
  bound. See `docs/cluster/architecture.md#raft-addresses`.
