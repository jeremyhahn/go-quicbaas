### Added

- Web UI: a Nodes view under Cluster lists the cluster's members with their address, Raft address and role, plus version and uptime when the daemon reports them (shown as not reported otherwise), and the TypeScript SDK gains `cluster.listNodes()` over `GET /api/v1/cluster/nodes` (go-quicbaas#2579).
