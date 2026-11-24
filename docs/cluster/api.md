# Cluster — API

> **Status: NORMATIVE** · Last verified: 2026-11-24
> The capability on every transport, with permissions. Feature folder: `docs/cluster/`.


## Transport matrix

Every row below is derived from source, not from prose. The columns mean:

- **SDK method** — the method name `sdk/go/client/<pkg>` passes to
  `transport.Request(ctx, serviceName, "<method>", req)`.
- **REST route (transport)** — where `sdk/go/transport/rest` sends it.
- **Served?** — whether that exact method+path pair is registered in the
  server's route table (`pkg/api/rest/*.go`). A **no** is a live SDK-to-server
  mismatch, not a documentation gap.
- **gRPC** — whether `sdk/go/transport/grpc` routes the service through a typed
  handler. **Unix inherits this column exactly**: the Unix transport is gRPC
  over `AF_UNIX` and delegates every `Request` to it
  (`sdk/go/transport/unix/transport.go:114`).
- **QUIC** is likewise not a separate column: `sdk/go/transport/quic` embeds
  `*rest.Transport` and swaps only the `http.Client` for an `http3.Transport`
  (`sdk/go/transport/quic/transport.go:86`), so every QUIC row is its REST row
  carried over HTTP/3. See the caveat in
  [../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md):
  **no component in this repository serves HTTP/3 today.**
- **MCP** — whether the SDK routes the service to an MCP tool; the registered
  server-side tool names are listed above the table.
- **Embedded** — the `service/method` key registered in
  `sdk/go/transport/embedded/transport.go`.
- **Permission** — `resource:action` from
  `pkg/api/rest/routes_permissions_generated.go`.

### `quicbaas.Cluster` — SDK package `sdk/go/client/cluster`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=yes; embedded routes registered: 7; MCP tools registered server-side: 0.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `AddNode` | `POST /api/v1/cluster/nodes` | yes | yes | yes | yes | `admin:add_member` |
| `GetRebalanceStatus` | `GET /api/v1/cluster/rebalance/status` | yes | yes | yes | yes | `rebalance:report` |
| `Nodes` | `POST /api/v1/cluster/nodes` *(default branch)* | **no** | yes | yes | MISSING | — |
| `Rebalance` | `POST /api/v1/cluster/rebalance` | **no** | yes | yes | yes | — |
| `RemoveNode` | `DELETE /api/v1/cluster/nodes/{nodeId}` | yes | yes | yes | yes | `admin:remove_member` |
| `Status` | `GET /api/v1/cluster/status` | yes | yes | yes | yes | `admin:status` |

`AddNode` takes the shard, the new replica's node ID and Raft address, and its
Raft member `identity` (`spki:<sha256-hex>`, as `qrdbd node-identity --cert`
prints it). The identity is optional when the cluster has already committed one
for the replica or its host, and required for a node no committed membership
names (go-qrdb v0.25 / go-quicraft v0.15 admit a member only with its
identity). The same field is `identity` on REST and gRPC (`AddNodeRequest`
field 4) and on the `quicbaas_add_node` MCP tool; node replace takes
`replacement_identity`.

### `quicbaas.Operations` — SDK package `sdk/go/client/operations`

Dedicated transport handlers: rest=no, grpc=yes, mcp=no; embedded routes registered: 5; MCP tools registered server-side: 0.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `GetMigrationProgress` | `GET /api/v1/operations/migrate/progress` | **no** | yes | MISSING | yes | — |
| `GetUpgradeStatus` | `GET /api/v1/operations/node/upgrade/status` | **no** | yes | MISSING | yes | — |
| `MigrateData` | `POST /api/v1/operations/migrate` | **no** | yes | MISSING | yes | — |
| `ReplaceNode` | `POST /api/v1/operations/node/replace` | **no** | yes | MISSING | yes | — |
| `UpgradeNode` | `POST /api/v1/operations/node/upgrade` | **no** | yes | MISSING | yes | — |

### go-qrdb re-exports — `sdk/go` `Client` accessors

These accessors return go-qrdb's own clients over the lazily built qrdb
sub-client, the one `SealStore` and `TenantMgmt` use. QuicBaaS adds no logic
between the caller and go-qrdb. The types are aliases declared in
`sdk/go/qrdb_cluster.go`.

| Accessor | go-qrdb type | REST / QUIC / MCP | gRPC / Unix | Embedded (`WithEmbeddedQRDB`) |
|---|---|---|---|---|
| `QRDBCluster()` | `*sdk.ClusterClient` (`QRDBClusterClient`) | yes without `WithAuthToken`; with a token, refused with InvalidInput (the transport cannot carry the bearer to go-qrdb) | yes; the `WithAuthToken` bearer rides each call as `authorization` metadata | yes; `AddReplica`, `RemoveReplica`, `Members` and `Leader` go to the Admin servicer |
| `QRDBMembership()` | `*sdk.Client` as `QRDBMembershipClient` | yes without `WithAuthToken`; with a token, refused with InvalidInput (the transport cannot carry the bearer to go-qrdb) | yes; the `WithAuthToken` bearer rides each call as `authorization` metadata, including the calls of the built manager, store, reconfigurer and advertiser | reconfigurer only. The membership store, manager and node advertiser need lease, watch and MVCC servicers, which `QRDBEmbeddedServicers` does not carry, so their calls fail with go-qrdb's `NilServiceError` |
| `QRDBRebalance()` | `transport.RebalanceClient` (`QRDBRebalanceClient`) | yes without `WithAuthToken`; with a token, refused with InvalidInput (the transport cannot carry the bearer to go-qrdb) | yes; the `WithAuthToken` bearer rides each call as `authorization` metadata | refused with `errors.QRDBRebalanceUnsupportedError` (`CodeUnimplemented`) |

- Refused with a token: go-qrdb v0.30.1-alpha's REST, QUIC and MCP transports
  take no auth option, so a `WithAuthToken` token there is refused with
  InvalidInput rather than silently dropped. This follows the
  `Search().QRDBTimeSeries` convention. Carrying the bearer there waits on a
  go-qrdb transport option, which needs a pin bump.
- Embedded: no token is forwarded. With `WithEmbeddedQRDB` the calls stay in
  process, so `WithAuthToken` neither rides them nor causes a refusal.
- `TransportEmbedded` without `WithEmbeddedQRDB` fails all three accessors with
  an InvalidInput error.
- Membership managers are built over a network transport, or on the node
  itself.




## Route ownership on a composed node

go-xkms v0.30's ServiceModule also registers `GET /api/v1/cluster/status` (and
`GET /api/v1/cluster/members`) on the same unified server. QuicBaaS owns
`/api/v1/cluster/status`: its handler reports every shard the node hosts in the
flat `ClusterStatusResponse` shape the quicbaas SDK and UI decode, whereas the
xkms handler reports a single shard wrapped as `{"status":{...}}`. The composer
states this in `pkg/apimodule/route_precedence.go` (`XKMSRouteExclusions`);
`/api/v1/cluster/members` has no quicbaas counterpart and is served by go-xkms.
