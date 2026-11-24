### Added

- The web console's cluster pages reach every cluster membership, leadership, rebalance and migration operation the TypeScript SDK builds: the Nodes page adds a node to a shard, replaces a node, removes a node from a shard after a confirmation, and filters to nodes that lead no shard; the Cluster page shows the cluster leader, looks up one shard's leader, shows the rebalance plan, starts a rebalance with live progress, and starts a migration between two shards with live progress and a confirmed cancel (#2371).
