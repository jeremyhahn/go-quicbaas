# Authorization — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
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

## Permission Format

Permissions are specified in the format: `resource:action`

### Resources
- `keys` - Database keys/entities
- `indexes` - Secondary indexes
- `cluster` - Cluster management
- `users` - User management
- `backups` - Backup operations
- `metrics` - Metrics and monitoring
- `health` - Health checks
- `transactions` - Transaction operations
- `watch` - Watch/subscribe operations
- `*` - All resources

### Actions
- `read` - Read access
- `write` - Write/create access
- `delete` - Delete access
- `admin` - Administrative access
- `*` - All actions

## Notes

- Built-in roles cannot be modified or deleted
- Users can have multiple roles with combined permissions
- Custom roles allow fine-grained access control
- Permission changes take effect immediately
- Deleting a role revokes permissions from all users with that role
