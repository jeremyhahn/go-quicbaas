# Agentic Projects — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/agentic-projects/`.

Manage agentic **projects** — the unit that binds a workspace, a git
repository, per-agent permissions, and resource quotas together.

Projects are tenant-scoped. Every subcommand operates inside the tenant
named by the global `--tenant` flag (default: `system`).

The command group goes through the SDK `Project()` client, so it is the
CLI counterpart of the REST routes under `/api/v1/projects` and of the
project views in the desktop/web GUI.

## Subcommands

| Command | Description |
|---------|-------------|
| `create` | Create a project |
| `list` | List every project in the tenant |
| `get [projectID]` | Show one project by ID, or by name with `--name` |
| `update <projectID>` | Update a project (only the flags you pass are applied) |
| `delete <projectID>` | Delete a project |
| `grant-role <projectID>` | Set an agent's role in the project |
| `revoke-role <projectID>` | Remove an agent's access (sets role `denied`) |

`--output json|yaml|table` is available on the whole group (default
`table`).

## quicbaas project create

| Flag | Description |
|------|-------------|
| `--name <name>` | Project name, unique within the tenant (**required**) |
| `--description <text>` | Human-readable description |
| `--workspace-agent <id>` | Agent that owns the workspace |
| `--workspace-path <path>` | Workspace path on the agent host |
| `--host-affinity <host>` | Pin project operations to a preferred agent host |
| `--default-lead-agent <id>` | Default lead agent for workflow runs |
| `--git-repo <url>` | Remote git repository URL |
| `--git-branch <branch>` | Default git branch |
| `--token-budget <n>` | Max LLM token budget (0 = unlimited) |
| `--gpu-minutes <n>` | Max GPU compute budget in minutes (0 = unlimited) |
| `--max-concurrency <n>` | Max concurrent workflow runs (0 = unlimited) |
| `--metadata k=v` | Caller-defined annotations (repeatable) |

```bash
quicbaas project create --name web-api \
  --description "Public API service" \
  --git-repo https://github.com/acme/web-api --git-branch main \
  --token-budget 1000000
```

## quicbaas project update

Accepts every `create` flag plus:

| Flag | Description |
|------|-------------|
| `--clear-description` | Remove the description (an empty `--description` leaves it unchanged) |
| `--git-sha <sha>` | Record the latest observed commit SHA |

Fields you do not pass keep their current value.

```bash
quicbaas project update proj_abc123 --token-budget 500000 --gpu-minutes 120
quicbaas project update proj_abc123 --clear-description
```

## quicbaas project get

```bash
quicbaas project get proj_abc123
quicbaas project get --name web-api --output yaml
```

Passing neither a project ID nor `--name` is an error — the command will
not issue a request it cannot scope.

## Agent roles

| Role | Meaning |
|------|---------|
| `lead` | Full control over the project and its workflows |
| `worker` | Can execute tasks, cannot change project configuration |
| `observer` | Read-only |
| `denied` | Explicitly prohibited (equivalent to `revoke-role`) |

```bash
quicbaas project grant-role proj_abc123 --agent agent_abc --role lead
quicbaas project revoke-role proj_abc123 --agent agent_abc
```

An unrecognized `--role` is rejected locally before any request is sent.

## Transport support

`project` routes through the SDK like every other command group, but the
Project service is currently implemented only on the **REST** and
**embedded** transports. Run project commands with
`--transport rest` (or `--transport embedded`); gRPC, QUIC, Unix, and MCP
have no Project service handler yet.

```bash
quicbaas --transport rest --address https://quicbaas.example.com:8443 project list
```
