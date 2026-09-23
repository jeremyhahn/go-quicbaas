### Changed

- Software Factory spawns (delivery, review, triage) name their workspace —
  tenant, project, key — instead of bind-mounting a directory the daemon
  provisioned on its own host. The host that runs the session provisions it
  under its own projects root, writes the workspace spec and forge token beside
  it, and reports the directory, which the daemon records on the session. A
  fleet agent on another machine no longer refuses every sdlc or review spawn
  at its mount check (go-quicbaas#981).
- A job's later sessions — review rounds, a rolled-over worker, the respawn
  after a restart — are pinned to the host holding its checkout with strict
  host affinity (`JobStatus.workspace_pin`). A pinned agent that is gone, or no
  longer holds the checkout, is refused with a typed error
  (`WorkspaceAgentUnavailableError`, `WorkspaceStaleError`) and the attempt is
  parked for an operator, uncharged — never cloned afresh on another host.
- `quicbaas agent serve --projects-root` (default `<--work-dir>/projects`) is
  where an agent provisions job workspaces; its mount policy admits that root
  tenant-scoped, so it needs no `--allowed-mount-root` entry.
- A spawn that mounts the daemon's KMS signing socket (delivery, ssh
  checkouts) is placed only on an agent that proved it can reach that socket:
  the daemon marks its sockets root with a random identifier, a co-located
  agent finds it under its `--allowed-mount-root` and advertises
  `signing-agent:<id>` on registration and every heartbeat. With no such agent
  and no in-process backend the spawn is refused
  (`SigningAgentUnavailableError`) and parked, instead of refused at a remote
  agent's mount check.

### Security

- Provisioning a session workspace never follows a link out of the tenant's
  projects subtree: every filesystem operation goes through an `os.Root`
  opened at `<projects_root>/<tenant>`, workspace levels must be directories
  (`WorkspaceLinkError`), and modes are set through a verified handle rather
  than by name, so a link a tenant plants through its own mount — or swaps in
  mid-provision — cannot redirect a chmod, a directory or the forge token.

### Fixed

- A command the engine runs in a session's container — a review's or
  triage's checkout, a delivery's verification and publish — now runs on the
  fleet agent that holds that container, over a new authenticated,
  tenant-scoped `AgentRuntime.ExecSession` RPC (bounded output cut on a rune
  boundary and carried as bytes, so non-UTF-8 output survives; deadline),
  instead of on the daemon's own container runtime, which does not know it.
- The first workspace pin is stored strictly: a refused write closes the
  session and parks the job (`WorkspacePinPersistError`) rather than
  proceeding with a pin no durable record holds.

- An operation on an existing remote session (prompt, status, exec, close) is
  routed to the agent that holds it or fails with
  `SessionHostUnavailableError`; it no longer falls back to the local backend,
  whose session-not-found closed the record while the remote container ran on.
  Heartbeat freshness now decides only where a NEW session is placed.
- A job's workspace pin is persisted when its first spawn returns, not at the
  next state transition, so a crash during checkout cannot lose it.

- A pinned spawn whose agent resolved (heartbeat still fresh) and then never
  answered is `WorkspaceAgentUnavailableError` and parks, instead of an
  ordinary unreachable-agent requeue that let the next attempt clone afresh
  elsewhere.

- A workspace spec directory — the forge token, in plaintext — no longer
  outlives its session on disk: every host removes all of them at startup,
  before it accepts a spawn, since no session survives a restart; and the
  token's in-memory copy is zeroed once written or sent.

- The fleet resolver no longer dispatches to an agent whose last heartbeat is
  older than `orchestration.heartbeat_timeout`. A crashed agent kept its
  "running" status and received work until it was deregistered by hand; every
  review routed to it failed on a refused connection.

### Upgrade notes

- Upgrade every fleet agent with the server. An agent that predates named
  workspaces reports none; the server closes that session and refuses the
  spawn (`SessionWorkspaceUnsupportedError`) rather than let the checkout land
  inside a container layer.
- Jobs in flight at the upgrade respawn under the new layout, cloning afresh
  from the branch they pushed; nothing unpushed from before the upgrade is
  carried over.
- The KMS signing agent's socket is still bound on the daemon host, so delivery
  and ssh-remote checkouts run only on an agent co-located with the daemon (or
  its in-process backend) until go-quicbaas#1013. Upgrade the co-located agent
  with the server: an older one advertises no signing trait, and signing
  spawns would have nowhere to go.
