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

### `quicbaas.Seal` — SDK package `sdk/go/client/seal`

Dedicated transport handlers: rest=no, grpc=no, mcp=no; embedded routes registered: 0; MCP tools registered server-side: 8.

MCP tools: `quicbaas_seal`, `quicbaas_seal_delete_recovery_key`, `quicbaas_seal_generate_recovery_key`, `quicbaas_seal_generate_root_token`, `quicbaas_seal_init`, `quicbaas_seal_rekey`, `quicbaas_seal_status`, `quicbaas_unseal`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `Init` | `POST /api/v1/seal/init` | yes | MISSING | MISSING | MISSING | `tenant_barrier:init` |
| `Seal` | `POST /api/v1/seal/seal` | yes | MISSING | MISSING | MISSING | `tenant_barrier:seal` |
| `Status` | `GET /api/v1/seal/status` | yes | MISSING | MISSING | MISSING | `tenant_barrier:status` |
| `Unseal` | `POST /api/v1/seal/unseal` | yes | MISSING | MISSING | MISSING | `tenant_barrier:unseal` |




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

### `/api/v1/seal` — the older seal surface, still mounted

| Route | Handler |
|---|---|
| `GET /api/v1/seal/status` | `HandleSealStatus` |
| `POST /api/v1/seal/init` | `HandleSealInit` |
| `POST /api/v1/seal/unseal` | `HandleUnseal` |
| `POST /api/v1/seal/seal` | `HandleSeal` |
| `POST /api/v1/seal/rekey` | `HandleRekey` |
| `POST /api/v1/seal/rotate-root` | `HandleRotateRootKey` |
| `POST /api/v1/seal/root/generate` | `HandleGenerateRootToken` |
| `POST /api/v1/seal/recovery/generate` | `HandleGenerateRecoveryKey` |
| `DELETE /api/v1/seal/recovery` | `HandleDeleteRecoveryKey` |

The SDK's `quicbaas.Seal` service (`sdk/go/client/seal`) targets the `/seal`
family, which is why the generated matrix above shows those paths rather than
the `/barrier` ones. The **CLI's `barrier` group is the canonical surface**
(`cmd/quicbaas/main.go:196-198` calls `configureCanonicalSurface` for exactly
this reason), and it targets `/barrier`.

Both are live **on the server**, even though the CLI's `seal` alias tree was
removed: `cmd/quicbaas/canonical.go:39-45` records that `barrier` is now the
single canonical entry point and "the former `seal` alias tree has been
removed". The removal was on the client. A deployment restricting access to one
route family and not the other has restricted nothing. That is stated here because it is not visible from either
family on its own, and it is a real access-control consideration rather than a
documentation nicety.

### `/api/v1/init` — the ceremony surface

The initialisation ceremony is its own mount, documented in
[`../bootstrap/api.md`](../bootstrap/api.md): `init/status`,
`init/initialize`, `init/claim-cert/{begin,complete}`,
`init/officer-share/{begin,complete}`, `init/officers`, `init/sign-csr`,
`init/enroll-external-officer`.

### Per-tenant barriers

A tenant's own barrier is a different object with a different lifecycle and its
own CLI group. See [`../multi-tenancy/cli.md`](../multi-tenancy/cli.md).
