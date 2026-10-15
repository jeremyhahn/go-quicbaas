### Fixed

- **The production preset no longer advertises `0.0.0.0` as its Raft address
  (go-quicbaas#185).** `config.ProductionConfig` set `raft.raft_address` to
  `0.0.0.0:63000`, an address no peer can dial. It now carries only the port,
  `:63000`; the operator supplies the host with `raft.raft_address`
  (`--raft-address`), and until then validation refuses the config with
  `RaftAdvertisedHostError`, whose message names that key and flag. The
  validator now keeps that typed error as the cause of the
  `raft.listen_address` failure, so callers can match it with `errors.As`.
