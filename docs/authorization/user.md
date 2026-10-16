# Authorization — User Guide

> **Status: NORMATIVE** · Last verified: 2026-10-16
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/authorization/`.

Roles, permissions and the decision itself are go-qrdb's. Read
[go-qrdb roles](../../../go-qrdb/docs/auth/roles.md) for the built-in roles and
what each may do, and the
[ABAC decorator](../../../go-qrdb/docs/auth/abac.md) for how a policy changes a
role-based decision. A request is decided within the tenant named by the
caller's OU.

## 1. Give a user a role

```bash
quicbaas iam role list --tenant <tenant-id>
quicbaas iam role assign <user-id> <role-id> --tenant <tenant-id>
```

A custom role is created with `quicbaas iam role create`. Every role verb is in
[cli.md](cli.md) and [iam/cli.md](../iam/cli.md#role-management).

## 2. Write a tenant ABAC policy

A policy is a YAML document in go-qrdb's
[policy schema](../../../go-qrdb/docs/auth/abac.md#policy-schema).
The CLI applies it to the tenant named in `metadata.tenant`. Validate it
before applying it:

```bash
quicbaas iam policy validate -f policy.yaml
quicbaas iam policy apply -f policy.yaml
```

The server pushes every stored policy to go-qrdb on each change. If go-qrdb
rejects the new set, the apply fails and the previous policies stay in force.

Conditions see only the subject: its ID, tenant, roles and string-valued
attributes. Request context such as IP address or time of day is not passed,
so a condition on it never matches.

## 3. Check a decision

```bash
quicbaas iam policy test --tenant <tenant-id> \
  --subject '{"id":"<user-id>","roles":["<role-id>"]}' \
  --resource <resource> --action <action>
```

`quicbaas iam pep check` asks the same question of the policy enforcement
point; `quicbaas iam pep check-batch -f requests.json` asks many at once. Both
return go-qrdb's decision.

## 4. Find why a request was denied

The REST, QUIC, gRPC, Unix and MCP permission for every operation is listed in
[api.md](api.md#how-each-transport-reaches-the-decision); the REST table is
`pkg/api/rest/testdata/route_permissions.golden`. Compare the permission with
the caller's roles (`quicbaas iam role list`), then run step 3 with the same
subject.

## See also

- [API](api.md) · [CLI](cli.md) · [architecture](architecture.md) · [engineering](engineering.md)
- [IAM policy API](../iam/api.md#policy-api)
