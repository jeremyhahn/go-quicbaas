### Fixed

- **"N online" counts only nodes that are online (go-quicbaas#851).** The
  dashboard's fleet tile counted nodes that were registered but had never
  started, and nodes that had stopped heartbeating. `fleetStore.onlineNodes` now
  counts a node only when it is `running` or `idle` and its `last_seen` is
  within the server's 90-second heartbeat timeout (`DefaultFleetHeartbeatTimeout`).
  A node with no `last_seen` is not counted. The spawn dialog uses the new
  `fleetStore.spawnableNodes`, so it still offers registered nodes.
