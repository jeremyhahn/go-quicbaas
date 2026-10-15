# Agent Fleet — API Reference

> **Status: NORMATIVE** · Last verified: 2026-10-11
> The fleet registry, enrollment, MCP and gRPC surfaces, checked against
> `pkg/api/rest/routes_*.go` and `api/proto`. Conflicts with this document are
> defects.

The tenant is always passed as the query parameter `?tenant_id=`; there is no
tenant header. Responses are JSON.

## REST

| Method and path | Permission | Handler |
|---|---|---|
| `GET /api/v1/fleet` | `orchestrator:status` | `HandleListFleetNodes` |
| `POST /api/v1/fleet` | `orchestrator:agent_register` | register a node |
| `GET /api/v1/fleet/{nodeID}` | `orchestrator:status` | get a node |
| `DELETE /api/v1/fleet/{nodeID}` | `orchestrator:close` | remove a node |
| `GET /api/v1/fleet/{nodeID}/backends/{backendName}/models` | `orchestrator:backend_list` | models a backend serves |
| `POST /api/v1/fleet/{nodeID}/disable` | `orchestrator:agent_remove` | disable and revoke credentials |
| `POST /api/v1/fleet/{nodeID}/enable` | `orchestrator:agent_register` | re-enable |
| `POST /api/v1/fleet/{nodeID}/heartbeat` | `orchestrator:agent_heartbeat` | `HandleFleetNodeHeartbeat` |
| `POST /api/v1/fleet/enroll` | mTLS client certificate | `HandleFleetEnroll` |

### List

`GET /api/v1/fleet?tenant_id=acme` filters on `host`, `node_type`, `backend`
(membership in the served backends) and `status`, plus the standard paging
parameters. The response is an envelope:

```json
{ "nodes": [ ... ], "count": 2, "total": 7 }
```

`count` is the number of records in this response; `total` is the number
matching the filter.

### Register

`POST /api/v1/fleet?tenant_id=acme`, body `RegisterFleetNodeRequest`:
`tenant_id`, `name`, `host`, `backend`, `model`, `node_type`, `capabilities`,
`work_dir`, `metadata`. `name`, `host` and `node_type` are required. The
response is `201 Created` with the node record.

### Node status

`registered`, `running`, `idle`, `offline`, `error`, `disabled`.

### Disable and enable

Both need `?tenant_id=` (400 without). Disable marks the node `disabled` and
revokes its OAuth2 client and certificate pin; disabling a disabled node
returns `FleetNodeAlreadyDisabledError`, and enabling a node that is not
disabled returns `FleetNodeNotDisabledError`. Disable and enable are on REST
and MCP only, with no CLI verb and no gRPC RPC.

### Heartbeat

`POST /api/v1/fleet/{nodeID}/heartbeat?tenant_id=acme`. The body is optional:
an empty body is a valid bare heartbeat (cap 64 KiB). When present it carries
`bind_addr`, `active_sessions`, `sandbox_policy`, the served `backends`, and
device telemetry (NVIDIA GPU, Intel NPU, RAPL package power). Served backends
let the record be corrected after registration. Response:

```json
{ "node_id": "agent-acme-agent-node-01", "heartbeat": true }
```

An unknown node with a valid peer certificate is re-published and accepted
rather than answered 404 ([architecture.md](architecture.md#liveness-heartbeat-and-recovery)).

### Enrollment

`POST /api/v1/fleet/enroll` is authenticated by the mTLS client certificate,
not a token. Body ≤ 64 KiB; `tenant_id` in the body must match the
certificate.

| Status | Error | Cause |
|---|---|---|
| 201 | | Enrolled |
| 400 | `invalid_request` | Malformed body |
| 401 | `invalid_client` | Certificate lacks OU `agents` or the ClientAuth EKU |
| 403 | `tenant_mismatch` | Body tenant differs from the certificate Organization or SPIFFE SAN |

Response fields: `client_id`, `client_secret` (plaintext, returned only
here), `agent_id`, `service_account_id`, `role`, `scopes`, `event_ingest_addr`
(the gRPC ingest URL, not the REST port) and `mtls_client_id`. Enrollment is
idempotent per tenant + CN: the `client_id` is stable and the secret rotates.

Granted scopes: `agent:runtime:spawn`, `agent:runtime:prompt`,
`agent:runtime:status`, `orchestrator:agent_heartbeat`, `orchestrator:hook`,
`agent_accounts:materialize`, `agent_accounts:read`, `agent_accounts:list`.
Token endpoints are `/oauth2/token` and `/oauth2/token-mtls`.

### Worker image

`GET /api/v1/sdlc/worker-image` and `POST /api/v1/sdlc/worker-image/refresh`;
see [worker-image.md](worker-image.md).

## MCP

| Tool | Permission |
|---|---|
| `quicbaas_fleet_register` | `orchestrator:agent_register` |
| `quicbaas_fleet_list` | `orchestrator:agent_list` |
| `quicbaas_fleet_status` | `orchestrator:agent_read` |
| `quicbaas_fleet_remove` | `orchestrator:agent_remove` |
| `quicbaas_fleet_heartbeat` | `orchestrator:agent_heartbeat` |
| `quicbaas_fleet_disable` | `orchestrator:agent_remove` |
| `quicbaas_fleet_enable` | `orchestrator:agent_register` |
| `project_grant_fleet_node`, `project_revoke_fleet_node` | project grants |

`quicbaas_fleet_list` takes the same `status`, `backend` (membership), `host`
and `node_type` filters as `GET /api/v1/fleet`, paged per
[List paging](../orchestration/api.md#list-paging-sorting-and-search).

## gRPC

`OrchestratorService` (`api/proto/orchestrator.proto`): `RegisterFleetNode`,
`GetFleetNode`, `ListFleetNodes`, `RemoveFleetNode`, `FleetNodeHeartbeat`,
`ListBackendModels`, and `PushEvents` (the node's event stream). `ProjectService`
(`api/proto/project.proto`): `GrantFleetNodeRole`, `RevokeFleetNodeRole`.

The node's own service, `quicbaas.fleet.runtime.v1.FleetRuntime`
(`api/proto/fleet_runtime.proto`), is served on `--bind` and called by the
control plane: `Enroll`, `Spawn`, `Prompt`, `Status`, `Close`, `Heartbeat`
(bidirectional stream), `Infer` (server stream), `ListModels`, `ExecSession`
and `SigningRelay`.

## See also

[cli.md](cli.md) · [engineering.md](engineering.md) · [user.md](user.md)
