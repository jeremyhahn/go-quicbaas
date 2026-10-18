# Barrier — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/barrier/`.


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

### No `quicbaas.Seal` service

The former `quicbaas.Seal` SDK service, its `quicbaas_seal*` MCP tools and its
`/api/v1/seal/*` routes were a parallel copy of the barrier and are removed
(#1559). Every seal and barrier verb goes through `Security().Barrier()`, the
re-export of go-qrdb's `BarrierClient`.




## Server routes — the barrier and seal surfaces are two mounts, not one

`pkg/api/rest/routes.go` registers **two** families. They are not aliases of
each other and the difference matters when reading a deployment's access logs.

### `/api/v1/barrier` — the canonical StorageBarrier lifecycle

| Route | Handler |
|---|---|
| `GET /api/v1/barrier/status` | `HandleBarrierStatus` |
| `POST /api/v1/barrier/initialize` | `HandleBarrierInitialize` |
| `POST /api/v1/barrier/unseal` | `HandleBarrierUnseal` |
| `POST /api/v1/barrier/seal` | `HandleBarrierSeal` |
| `POST /api/v1/barrier/rekey` | `HandleBarrierRekey` |
| `POST /api/v1/barrier/root-token` | `HandleBarrierRootToken` |
| `POST /api/v1/barrier/enable-encryption` | `HandleBarrierEnableEncryption` |
| `POST /api/v1/barrier/shamir/initialize` | `HandleBarrierShamirInitialize` |
| `POST /api/v1/barrier/shamir/unseal-share` | `HandleBarrierShamirUnsealShare` |
| `POST /api/v1/barrier/shamir/unseal-shares` | `HandleBarrierShamirUnsealShares` |
| `GET /api/v1/barrier/recovery/status` | `HandleBarrierRecoveryStatus` |
| `POST /api/v1/barrier/recovery/generate` | `HandleBarrierRecoveryGenerate` |
| `POST /api/v1/barrier/recovery/recover` | `HandleBarrierRecoveryRecover` |
| `DELETE /api/v1/barrier/recovery` | `HandleBarrierRecoveryDelete` |

#### Request shapes — a credential is bytes, and the shapes are aliases

The go-qrdb SDK's REST transport marshals `go-qrdb/sdk/go/transport`'s own
`Barrier*Request` structs straight into the body these handlers decode, so
those structs **are** the wire format. The stack is pre-v1 with exactly one
(CLAUDE.md, "SDK Boundary"), so `pkg/api/rest/handlers_barrier.go` declares
its request shapes as **type aliases** of the go-qrdb ones rather than local
copies. A field renamed or retyped upstream is then a compile error here, not
a silent mis-decode.

That rule is written down because breaking it shipped a defect
(go-quicbaas#988). go-qrdb v0.19.0-alpha retyped every barrier credential from
`string` to wipeable `[]byte` — a Go string cannot be zeroized — and
`encoding/json` renders a `[]byte` as **base64**. The local mirrors here kept
decoding `secret` as a string, so `barrier unseal --transport rest` handed the
barrier the base64 TEXT of the passphrase and failed with
`quicraft/seal: unseal failed (strategy=passphrase): decryption failed`.

So, on every REST client including the TypeScript SDK:

| Field | Type | On the wire |
|---|---|---|
| `secret` (initialize, unseal, shamir/initialize) | `[]byte` | base64 |
| `passphrase` (enable-encryption, tenant barrier/init) | `[]byte` | base64 |
| `key` (tenant barrier/unseal) | `[]byte` | base64 |
| `share`, `shares`, `keys` | `string` | plain text — a Shamir share is already a printable encoding |

On every return path of every handler that carries a credential — the four
system shapes above and the three per-tenant ones (`barrier/init`,
`barrier/unseal`, `barrier/enable-encryption`) — **the slice the request field
finally holds is wiped**, not only on the successful path. `encoding/json`
populates earlier fields before it reports a later field's type error, so a
body with a good `secret` and a bad `threshold` leaves plaintext behind unless
the cleanup is registered before the decode rather than after it, which is what
`wipeCredential` is for.

The guarantee is deliberately narrow, and the boundary is worth knowing. Wiping
destroys that slice and every alias of it — the copy handed to the barrier
service, and the one that would be found under a stable name in a heap dump. It
does not reach an allocation `encoding/json` made and then dropped: a duplicate
key (`{"secret":"<a>","secret":null}`) decodes the first value and then
overwrites the field, and a malformed base64 value leaves whatever the decoder
wrote to its scratch buffer. Those are unreferenced and reclaimed by the
collector, but they are not zeroized, and no code in this package can zeroize
them. go-qrdb states the same limit for the encode side on
`BarrierInitializeRequest` — "serializing a value means copying it, and the
wire format owns that copy" — and deserializing is that trade in reverse.

**Decoding catches malformed base64 and nothing else — always encode.** A body
whose credential is not well-formed base64 is refused with `400` and the
typed `BarrierCredentialEncodingError` (`"barrier credential must be
base64-encoded bytes"`), which names no field and echoes no input, and the
request never reaches the barrier. Nothing about the refusal is logged, and the
raw request body, which holds the credential's base64 text, is wiped once it
has been decoded. That is the whole of the protection, and it is narrower
than "a raw passphrase is caught": a raw credential that happens to be valid
base64 (any run of the base64 alphabet whose length is a multiple of four —
`password` is one) decodes successfully into DIFFERENT bytes, and the barrier
is then keyed on those bytes with no complaint. An `init` done that way
succeeds and leaves a barrier whose passphrase is not the one the operator
typed. So a client must encode its credential rather than rely on being told
off; `TestBarrierWire_RawCredentialThatIsValidBase64DecodesToOtherBytes` pins
that limit so it is not mistaken for a guarantee.

#### gRPC, Unix and QUIC — a credential is a `bytes` field

The QuicBaaS-native `quicbaas.barrier.v1.Barrier` and
`quicbaas.tenant.v1.TenantBarrier` services declare every barrier credential as
`bytes` (go-quicbaas#962): `secret` on `BarrierInitializeRequest`,
`BarrierUnsealRequest` and `BarrierShamirInitializeRequest`, and `passphrase` on
`BarrierEnableEncryptionRequest`, `TenantBarrierInitRequest` and
`TenantBarrierEnableEncryptionRequest`. The handlers pass the decoded `[]byte`
straight to the barrier service and wipe it on return. proto3 encodes `bytes`
and `string` identically, so the frames did not change;
`TestBarrierCredentialFrames_StringAndBytesEncodeIdentically` in `api/proto`
proves it. What changed is the generated Go type (`[]byte`, not `string`) and
that a credential no longer has to be valid UTF-8. The Shamir share fields
(`shares`, `share`) are still `string`.

MCP is the one exception: a tool argument is a JSON string by definition of the
protocol, so the server converts it to bytes in one function (`credentialArg`)
and wipes the result after use.

`enable-encryption` on `/api/v1` is the one shape that is NOT an alias: the
migration is a QuicBaaS-native verb served on four QuicBaaS surfaces (REST
system, REST per-tenant, gRPC, MCP) that all name the target
`target_strategy`, and go-qrdb has no per-tenant counterpart, so renaming only
the REST copy would de-align it from its siblings. Its `passphrase` still
follows the byte rule above. The go-qrdb wire mount
`POST /v1/barrier/enable-encryption` is served by a separate handler,
`HandleBarrierWireEnableEncryption`, whose request IS the alias
`qrdbtransport.BarrierEnableEncryptionRequest` (`strategy`, body `tenant_id` as
the scope) and which answers `200 {status, scope}` — the shape go-qrdb's REST
transport decodes (go-quicbaas#990). Because the body selects the scope, the
mount is identity-gated at registration. The handler authorizes the selected
scope: `seal:enable_encryption` for the system barrier, or
`tenant_barrier:enable_encryption` against the named tenant. It answers `401`
with no identity, `403` on denial, and `503` when no scope authorizer is
available.

The TypeScript SDK calls this mount: `client.barrier.enableEncryption(req)`
sends `BarrierEnableEncryptionRequest` (base64-encoding `passphrase`; set
`tenant_id` for a tenant barrier) and resolves to
`BarrierEnableEncryptionResponse`. A refusal rejects with `APIError`. The
gRPC-Web, MCP and QUIC TS transports refuse the verb.

#### Seal requires the SO PIN when a PIN backend is configured

When the server composes a PIN backend (`WithPINBackend`) and an SO PIN is
set on it, `barrier seal` must carry the Security Officer PIN
(go-quicbaas#946). `BarrierService.BarrierSeal` verifies it once, before the
barrier is touched: a missing or wrong PIN is refused with a forbidden
`QBError` (`403` on REST, `PermissionDenied` on gRPC) and the barrier stays
unsealed. With no PIN backend, or none with an SO PIN set, seal is not gated,
and an empty request still seals.

| Surface | Where the PIN travels |
|---|---|
| REST `POST /api/v1/barrier/seal`, `POST /v1/barrier/seal` | body `{"so_pin": "..."}`; an empty body is still valid |
| gRPC / Unix `quicbaas.barrier.v1.Barrier/BarrierSeal` | `BarrierSealRequest.so_pin` |
| gRPC / Unix `qrdb.v1.QRDBService/BarrierSeal` (the Go SDK's route) | `x-qrdb-so-pin` metadata, else `BarrierSealRequest.so_pin` |
| MCP `qrdb_barrier_seal` | `so_pin` tool argument |
| Go SDK `Security().Barrier().BarrierSeal(ctx)` | bound to `ctx` with `WithSOPIN`; REST and MCP send it, over TLS only |
| CLI `quicbaas barrier seal` | `--so-pin-stdin`, then `QUICBAAS_BARRIER_SO_PIN`; `--so-pin` is accepted but visible in `ps` |
| TypeScript `barrier.seal({ so_pin })` | REST body and MCP tool argument, over `https://` only (`SOPINRequiresTLSError` otherwise) |

The go-qrdb SDK's gRPC and Unix transports call go-qrdb's own `qrdb.v1`
`BarrierSeal`, which quicbaasd registers with no PIN backend: the go-qrdb SDK
re-exports no register option that takes one. So quicbaasd's gRPC server,
which also serves the Unix socket, runs
`UnaryBarrierSealSOPINInterceptor` (`pkg/api/grpc`) on that one method. It
reads the PIN from the `x-qrdb-so-pin` metadata the SDK clients send, falling
back to the request's `so_pin`, and verifies it through
`service.VerifyBarrierSealSOPIN`, the same check the facade makes, so a refusal
is the same `PermissionDenied`.

quicbaasd serves no `qrdb.v1` seal over QUIC. Its QUIC is REST over HTTP/3,
whose seal is the facade route above. go-qrdb's native QUIC server, which
answers `barrier.Seal`, starts only with `Protocols.QUIC`, and the embedded
node never sets it (`TestBuildQRDBConfig_NoNativeQUICListener`). Were it
enabled, that server would verify `so_pin` itself against the PIN backend
quicbaasd passes the node with `WithPINBackend`.

The handlers and the interceptor wipe the PIN bytes on return. One limit
remains: qrdb's `PINBackend.VerifySOPIN` takes a `string`, which cannot be
wiped. The embedded transport has no SO gate by go-qrdb's design. The
gRPC-Web and QUIC TypeScript barrier clients do not implement any barrier
verb yet.

#### Who owns the mount on a composed node

`server.rest` no longer binds a listener of its own: `startRESTServer`
(`pkg/server/lifecycle_rest.go`) builds a non-listening `*rest.Server` and the
embedded go-qrdb node's unified server is the only REST listener. quicbaas's
routes reach it through the ServiceModule seam (`pkg/apimodule/module_http.go`
`RegisterHTTP`, fed by `rest.ModuleRoutes()`).

Two modules are mounted on that one server — quicbaas's and go-xkms's — and
both route tables carry `/api/v1/barrier/*`. go-qrdb's registrar gives a path
to the FIRST claimant and logs the loser at debug level, so precedence is
silent unless the composer states it. It is stated in
`pkg/apimodule/route_precedence.go`: the xkms module is wrapped in
`NewRouteExcludingModule(..., XKMSRouteExclusions)` before
`qrdbsdk.WithModule`, so quicbaas owns the canonical barrier mount.

That is not a preference. The barrier is go-qrdb's, and quicbaas's handlers
delegate to the go-qrdb SDK `BarrierClient` bound to the platform shard — the
same facade gRPC, unix, MCP, QUIC and embedded answer from. go-xkms's same-path
handlers operate xkms's OWN barrier, which its `pkg/api/rest/host.go`
`NewHostServer` documents as "xkmsd-only and stay disabled" on a host
composition. Left mounted, they answered
`503 {"error":"rest: barrier not configured"}` on a node whose barrier was
configured and unsealed (go-quicbaas#405) — a wrong answer, not a refusal.

`/api/v1/init/*` remains module-excluded: the xkms module carries its own
ceremony routes there and which module owns the composed node's init plane is
an open decision.

#### Timestamp rendering

Every barrier timestamp renders as RFC3339**Nano** (`barrierTimeFormat` in
`pkg/api/{rest,grpc,mcp}`), matching go-qrdb's `qrdb.v1` gRPC barrier surface
that the unix and gRPC transports reach. Rendering at second precision made
one barrier report `2026-08-25T04:40:05Z` over REST and
`2026-08-25T04:40:05.727835519Z` over unix — the same instant, but not the
byte-comparable answer an operator diffing two transports needs.

#### The recovery-status field is `has_keys` on REST

`GET /api/v1/barrier/recovery/status` answers `{"has_keys": true}`. The name
is not the one the gRPC and MCP surfaces use for the same bit, and that is
deliberate: the barrier is go-qrdb's, so each transport serves the wire format
go-qrdb's client for THAT transport reads.

| Transport | Field | Client that reads it |
|---|---|---|
| REST | `has_keys` | `go-qrdb/sdk/go/transport/rest` decoding `transport.BarrierHasRecoveryKeysResponse` |
| MCP | `has_recovery_keys` | `go-qrdb/sdk/go/transport/mcp`, which maps it onto `HasKeys` |
| gRPC | `has_recovery_keys` | the `BarrierRecoveryStatusResponse` protobuf field |

Emitting `has_recovery_keys` over REST made
`Security().Barrier().BarrierHasRecoveryKeys(...)` decode `false` for a
barrier that HAS recovery keys — a wrong answer on a disaster-recovery
precondition rather than an error, which is the worse failure mode
(go-quicbaas#991). The TypeScript SDK's
`BarrierHasRecoveryKeysResponse` mirrors the REST body and carries `has_keys`
for the same reason.

### `/api/v1/seal` — removed

The older `/api/v1/seal/*` route family is no longer mounted (#1559). The
`/api/v1/barrier/*` family above is the only REST path to the barrier
lifecycle, and the CLI's `barrier` group and `Security().Barrier()` target it.

### `/api/v1/init` — the ceremony surface

The initialisation ceremony is its own mount, documented in
[`../bootstrap/api.md`](../bootstrap/api.md): `init/status`,
`init/initialize`, `init/claim-cert/{begin,complete}`,
`init/officer-share/{begin,complete}`, `init/officers`, `init/sign-csr`,
`init/enroll-external-officer`.

### Per-tenant barriers

A tenant's own barrier is a different object with a different lifecycle and its
own CLI group. See [`../multi-tenancy/cli.md`](../multi-tenancy/cli.md).
