# Phase F Integration Tests: Standalone + Cluster + Seal/Hardware

> **Status: REFERENCE** · Last verified: 2026-08-13
> Phase F end-to-end integration tests.

Phase F delivers end-to-end integration tests that validate the new architecture
(quicbaas on qrdb) with real services — no mocks, no stubs, no dead
reimplementation paths.

## Test suites

`make integration-test` runs every package under `test/integration/` plus the integration-tagged end-to-end tests in `cmd/quicbaasd` (the SDLC loop and webhook suites); `make integration-test-quicbaasd` runs that package alone.

### Standalone (no-Raft, in-process)

Location: `test/integration/standalone/` (`TestStandalone_*`). The cases need
no container, but each starts a real embedded `server.Server` backed by the
qrdb engine on PebbleDB in its own `t.TempDir()` data directory, so they run in
the integration tier, not the unit tier. `make integration-test-standalone`
(the generic `integration-test-%` rule) runs `./test/integration/standalone/...`
in the integration container; the `go test` line runs the same cases directly:

```
make integration-test-standalone
go test -count=1 -tags integration -run 'TestStandalone_' ./test/integration/standalone/
```

Covers:
- KV put/get/delete and prefix scan through the real qrdb engine
- Data persistence across a server restart (PebbleDB recovery)
- Secrets store/get/delete (KV-backed secrets manager)
- Scheduler job create / get (KV-backed persistence)
- Passphrase-sealed start: `TestStandalone_PassphraseSealedStartServesKV` —
  a server started under a passphrase barrier unseals and serves KV.
- At rest: `TestStandalone_AtRest_SentinelAbsentFromDisk` restarts a
  passphrase-sealed server so PebbleDB flushes its WAL into a table, then
  scans every file for a plaintext sentinel.

### Clustered (3-node Raft, docker-compose)

Location: `test/integration/cluster/`

Requires Docker + Docker Compose v2 and the cluster docker-compose stack:

```
make integration-test-cluster
```

Individual sub-targets:
```
make integration-test-cluster-failover     # leader-failover only
make integration-test-cluster-membership  # dynamic membership only
```

Environment variables consumed by the tests:

| Variable | Purpose |
|---|---|
| `QUICBAAS_NODE1_URL` | REST endpoint of node 1 (required) |
| `QUICBAAS_NODE2_URL` | REST endpoint of node 2 (required) |
| `QUICBAAS_NODE3_URL` | REST endpoint of node 3 (required) |
| `QUICBAAS_NODE4_URL` | REST endpoint of node 4 for dynamic-membership test |
| `QUICBAAS_NODE{1,2,3}_DATADIR` | Host paths to node data directories for at-rest check |

Covers:
- 3-node replication: write on node1, read from node2 and node3 after Raft log application.
- Leader failover: kill node1, confirm node2/node3 elect a new leader and continue serving.
- At-rest encryption across all nodes: sentinel absent from raw `.sst` files.
- Dynamic membership: node4 joins mid-operation, catches up, writes replicate back.

**Prerequisite:** The devcontainer / CI environment with Docker Compose.
The 3-node stack is defined in `test/integration/docker-compose.cluster.yml`.

### Seal / Hardware (cloud KMS + hardware HSM)

Location: `test/integration/seal/`

```
make integration-test-seal             # awskms (Floci) + Vault
```

For hardware strategies (TPM2, PKCS11, swtpm, SoftHSM) run inside the devcontainer
where those services are provisioned. The seal tests use the real quicbaas seal
factory (`pkg/server/seal_factories.go`) and inject strategies through
`XKMSCryptoOptions` → `server.New` as in production.

## Configuration: deployment_id

All nodes in a Raft cluster **must agree on the same `deployment_id`**.
Set it explicitly in the quicbaas YAML configuration:

```yaml
raft:
  node_id: 1
  deployment_id: 42      # must match on every node in the cluster
  raft_address: "node1:63101"
  join_addresses:
    - "1=node1:63101"
    - "2=node2:63101"
    - "3=node3:63101"
```

If `deployment_id` is omitted, `buildQRDBConfig` falls back to `node_id` for
backward-compatible single-node dev/test mode. **Do not rely on this fallback
in production**: nodes with different `node_id` values would derive different
`deployment_id` values and refuse to cluster.

Environment variable: `QUICBAAS_RAFT_DEPLOYMENT_ID`.

## Passphrase barrier auto-unseal

For `quicbaasd init --barrier-strategy=passphrase` and server startup with a
passphrase seal strategy, pass the passphrase at construction time:

```go
srv, err := server.NewServer(cfg,
    server.WithSealPassphrase([]byte("my-operator-passphrase")),
)
```

Without `WithSealPassphrase`, a passphrase-strategy node boots **sealed**
(`node.Client()` returns nil), and any call to `GetKVStore` panics. The
`quicbaasd init` flow now passes the passphrase via `WithSealPassphrase` so the
embedded server used for IAM bootstrap comes up fully operational.

For a daemon restart where the operator must supply the passphrase interactively,
omit `WithSealPassphrase` and use the barrier API (`barrier unseal`) after boot.
