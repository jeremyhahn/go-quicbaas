### Changed

- **Every Raft address is now configurable, and none is invented
  (go-quicbaas#185).** The new key `raft.listen_host`
  (`QUICBAAS_RAFT_LISTEN_HOST`, `--raft-listen-host`, default unset) sets
  the interface to bind when `raft.listen_address` is unset. In that case the
  bind address is `raft.listen_host` on the port of `raft.raft_address`.
  `raft.listen_address` (`QUICBAAS_RAFT_LISTEN_ADDRESS`, new flag
  `--raft-listen-address`) now defaults to unset. Any value set for it is used
  exactly as given, including `127.0.0.1:63000`, which was previously rewritten
  to `0.0.0.0:63000`. Precedence is flags > environment > file > defaults for
  every key.
- `--raft-address` sets only the advertised address. It no longer sets the
  bind address too, so a node started with `--raft-address 10.0.0.5:63001`
  binds `10.0.0.5:63001` unless `raft.listen_host` or `raft.listen_address`
  says otherwise.
- `--raft-port` keeps the configured hosts. It moves the port of the
  configured advertised address and of a configured `raft.listen_address`;
  it no longer resets them to `<hostname>` and `0.0.0.0`. If no advertised
  address is configured and the hostname cannot be read, `quicbaasd` refuses
  to start and asks for `--raft-address`; it no longer advertises
  `localhost`.
- A single-node clustered node advertises its configured `raft.raft_address`.
  An empty one refuses the boot instead of falling back to `127.0.0.1:63101`.
  See `docs/cluster/architecture.md#raft-addresses`.
