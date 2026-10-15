### Changed

- **A clustered control plane refuses a loopback fleet node address
  (go-quicbaas#1349).** On the node that hosts a co-located agent, a loopback
  address (`127.0.0.1`, `::1`, `localhost`) passed the reachability probe,
  but no other node of the cluster could dial it. Registration on a clustered
  control plane now refuses it with `422 agent_address_loopback_in_cluster`
  (`AgentAddressLoopbackInClusterError`), which names `--advertise`,
  `advertise_addr` and `QUICBAAS_ADVERTISE_ADDR` as the fix. A single-node
  control plane still accepts loopback, and every other address still goes
  through the probe.
- **The dev cluster's gRPC ports move to 9290-9292 (go-quicbaas#1349).** Node
  2 took 9091, the fleet node agent's default, so an agent could not run
  beside a dev cluster node. `devcluster.base_ports.grpc` now defaults to
  9290. The agent's port is unchanged.
