# Multi-Tenancy — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/multi-tenancy/`.

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

## Tenant provisioning

`TenantClient.Provision` creates a tenant and its first admin in one call. It
is the onboarding path when public registration is off. Server-side tool:
`quicbaas_iam_tenant_provision`.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `quicbaas.IAM.Tenant` / `Provision` | `POST /api/v1/tenants/{tenant_id}/provision` | yes | `IAMService/ProvisionTenant` | `quicbaas_iam_tenant_provision` | `quicbaas.IAM.Tenant/Provision` | `tenants:create` |

QUIC carries the REST row over HTTP/3, and Unix carries the gRPC column over
`AF_UNIX`. The `*provision*` tests under
`sdk/go/transport/{rest,quic,grpc,mcp,unix}` cover provisioning on every SDK
transport. Except for gRPC, which tests `invite` mode only, they cover both
enrollment modes.

Request and response fields:
[../iam/api.md](../iam/api.md#tenants) (**Provision Tenant**).

## Invite activation

A pending user, including an `invite`-mode tenant admin, activates with its
one-time token. These verbs are pre-auth: the token is the credential, so they
carry no permission and the gRPC methods are in the public method set. QUIC
carries the REST column over HTTP/3 and Unix the gRPC column; the `*invite*`
tests under `sdk/go/transport/{quic,unix}` pin both. There are no MCP invite
tools yet; they wait on a decision about public MCP tools.

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `quicbaas.IAM.Invite` / `GetInvite` | `GET /api/v1/iam/{tenant_id}/invites/{token}` | yes | `IAMService/GetInvite` | — | `quicbaas.IAM.Invite/GetInvite` | public |
| `quicbaas.IAM.Invite` / `ActivateFIDO2Begin` | `POST /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/begin` | yes | `IAMService/ActivateFIDO2Begin` | — | `quicbaas.IAM.Invite/ActivateFIDO2Begin` | public |
| `quicbaas.IAM.Invite` / `ActivateFIDO2Finish` | `POST /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/finish` | yes | `IAMService/ActivateFIDO2Finish` | — | `quicbaas.IAM.Invite/ActivateFIDO2Finish` | public |
| `quicbaas.IAM.Invite` / `ActivatePIV` | `POST /api/v1/iam/{tenant_id}/invites/{token}/activate/piv` | yes | `IAMService/ActivatePIV` | — | `quicbaas.IAM.Invite/ActivatePIV` | public |

Request, response and error fields:
[../iam/api.md](../iam/api.md#invite-activation).
