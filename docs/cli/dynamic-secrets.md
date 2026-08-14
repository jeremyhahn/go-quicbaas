# quicbaas dynamic-secrets

Dynamic secrets management against the active QuicBaaS daemon.
Alias: `quicbaas dynsec`.

## Overview

A **role** is a template that a credential generator uses to mint short-lived
credentials. Every generated credential carries a **lease** that can be renewed
or revoked.

The verb tree wires directly to `Security().DynamicSecrets()`
(`sdk/go/client/dynamicsecrets`). The verb set mirrors that SDK client
interface one-for-one — four role verbs, one generation verb, three lease
verbs.

Source: [`cmd/quicbaas/dynamic_secrets.go`](../../cmd/quicbaas/dynamic_secrets.go).

## Verb summary

| Verb | Purpose |
|------|---------|
| `role create <name>` | Create a credential-generation role |
| `role get <name>` | Show a role |
| `role list` | List roles |
| `role delete <name>` | Remove a role |
| `generate <role>` | Generate a credential from a role |
| `lease renew <lease-id>` | Extend a credential lease |
| `lease revoke <lease-id>` | Revoke a credential lease immediately |
| `lease list` | List active leases |

## Flags

`dynamic-secrets` accepts `--output <json|yaml|table>` (default `table`) as a
persistent flag. Global flags (`--tenant`, `--transport`, `--address`,
`--timeout`, TLS flags) are inherited from the root command.

## Transport support

All eight verbs work on every transport — REST, gRPC, QUIC, MCP, Unix and
embedded. Each SDK transport carries a dedicated `quicbaas.DynamicSecrets`
handler because the leaf SDK types are plain Go structs that the generic
dispatch paths cannot marshal:

| Transport | Handler | Wire |
|-----------|---------|------|
| REST | [`sdk/go/transport/rest/dynamicsecrets_handler.go`](../../sdk/go/transport/rest/dynamicsecrets_handler.go) | `/api/v1/secrets/dynamic/*`; substitutes the role-name / lease-ID path parameters |
| gRPC | [`sdk/go/transport/grpc/dynamicsecrets_handler.go`](../../sdk/go/transport/grpc/dynamicsecrets_handler.go) | `quicbaas.dynamicsecrets.v1.DynamicSecretsService` |
| QUIC | [`sdk/go/transport/quic/dynamicsecrets_handler.go`](../../sdk/go/transport/quic/dynamicsecrets_handler.go) | top-level opcodes `0x64`-`0x6B` |
| MCP | [`sdk/go/transport/mcp/dynamicsecrets_handler.go`](../../sdk/go/transport/mcp/dynamicsecrets_handler.go) | the eight `quicbaas_dynamic_secrets_*` tools |
| Unix | delegates to the gRPC handler verbatim | gRPC over a domain socket |
| embedded | [`sdk/go/transport/embedded/handlers_dynamicsecrets.go`](../../sdk/go/transport/embedded/handlers_dynamicsecrets.go) | in-process service call |

The QUIC opcodes are top-level rather than `OpOrchestrator` sub-ops, so the
surface consumes none of the shared sub-op space. Client and server values are
pinned against each other by `TestDynamicSecretsOpcodeParity` in both
`pkg/api/quic` and `sdk/go/transport/quic`.

### Authorization

Every transport maps onto the same qrdb permission vocabulary, so a grant is
transport-independent:

| Verb | Permission |
|------|------------|
| `role create` | `dynamic_secrets:role_create` |
| `role get` | `dynamic_secrets:role_read` |
| `role list` | `dynamic_secrets:role_list` |
| `role delete` | `dynamic_secrets:role_delete` |
| `generate` | `dynamic_secrets:generate` |
| `lease renew` | `dynamic_secrets:lease_renew` |
| `lease revoke` | `dynamic_secrets:lease_revoke` |
| `lease list` | `dynamic_secrets:lease_list` |

Credential minting (`generate`) and the two lease-mutating verbs are
write-strength actions distinct from the read/list actions; grant them
deliberately.

## role create

```bash
quicbaas dynamic-secrets role create readonly-db \
  --generator-type postgres \
  --default-ttl 1h --max-ttl 24h \
  --config host=db.internal --config role=readonly
```

| Flag | Meaning |
|------|---------|
| `--generator-type` | Credential generator to use (required) |
| `--config` | Generator config as `key=value` (repeatable) |
| `--default-ttl` | Default credential lifetime (required, positive) |
| `--max-ttl` | Maximum credential lifetime (required, positive, ≥ `--default-ttl`) |

## role get / list / delete

```bash
quicbaas dynamic-secrets role get readonly-db --output json
quicbaas dynamic-secrets role list --prefix readonly-
quicbaas dynamic-secrets role delete readonly-db
```

Deleting a role does not revoke leases already minted from it; revoke those
separately with `lease revoke`.

## generate

```bash
quicbaas dynamic-secrets generate readonly-db
quicbaas dynamic-secrets generate readonly-db --ttl 30m --output json
quicbaas dynamic-secrets generate readonly-db --reveal
```

**Security.** `generate` returns live credential material. Table output
redacts every credential value unless `--reveal` is passed. `--output json`
and `--output yaml` always carry the full payload, because those formats exist
to be piped into a consumer — prefer them over leaving secrets in a terminal
scrollback.

## lease renew / revoke / list

```bash
quicbaas dynamic-secrets lease renew lease_abc123 --increment 1h
quicbaas dynamic-secrets lease revoke lease_abc123
quicbaas dynamic-secrets lease list --prefix database/
```

The server clamps a renewal to the role's max TTL.

## Errors

| Type | Condition |
|------|-----------|
| `DynamicSecretsArgRequiredError` | The positional name / role / lease-id argument is missing or blank |
| `DynamicSecretsFlagInvalidError` | A flag carries a missing, negative, or contradictory value |
| `DynamicSecretsConfigFormatError` | `--config` is not in `key=value` form |
