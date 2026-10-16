# Authorization — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-16
> Every verb and flag, with runnable examples. Feature folder: `docs/authorization/`.

Roles are managed with `quicbaas iam role`; there is no top-level
`quicbaas role`. Every verb and flag is in [iam/cli.md](../iam/cli.md#role-management).

| Task | Command |
|------|---------|
| Create a custom role | `quicbaas iam role create <id> --name <name> [--description <text>]` |
| Show, update, delete, list | `quicbaas iam role get <id>`, `update <id>`, `delete <id>`, `list` |
| Assign a role to a user | `quicbaas iam role assign <user-id> <role-id>` |
| Remove a role from a user | `quicbaas iam role revoke <user-id> <role-id>` |
| Converge built-in roles onto this build | `quicbaas iam role reconcile` |

All take `--tenant` and the `iam` flags `--output` and `--yes`. The CLI has no
verb to grant or revoke individual permissions on a role; ABAC rules are
managed with `quicbaas iam policy` and tested with `quicbaas iam policy test`.

Policy verbs (`cmd/quicbaas/iam_policy.go`): `apply`, `get`, `delete`, `list`,
`validate`, `test`. The policy document is go-qrdb's ABAC schema; see
[engineering.md](engineering.md).

## Permissions and roles

Permissions are go-qrdb's `resource:action` strings, plus the families
go-quicbaas contributes ([api.md](api.md#permissions-go-quicbaas-contributes-to-go-qrdb)).
Built-in roles, their permissions and which may be modified are go-qrdb's; see
[roles](../../../go-qrdb/docs/auth/roles.md) and
[built-in roles](../../../go-qrdb/docs/identity-rbac/architecture.md#built-in-roles).
