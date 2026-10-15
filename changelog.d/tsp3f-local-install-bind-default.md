### Added

- **`quicbaas fleet deploy --local` installs a node on this host with no
  sshd.** The install primitives take `deploy.Target`; `*SSHClient` and the
  new `LocalTarget` (os/exec, atomic rename) implement it. A local deploy runs
  the same preflight minus the ssh, host-key and deploy-key checks, the same
  rendered unit or nohup launcher, and the same `--systemd auto|yes|no`.
  `--local` with `--ssh`, a spec `host`, or any ssh setting fails with
  `LocalSSHConflictError`. See `docs/agent-fleet/cli.md#local-install`.
- **`fleet stop`, `fleet start` and `fleet undeploy` take `--local`** and manage a `deploy --local` install with no ssh (`docs/agent-fleet/cli.md#lifecycle`).

### Changed

- **A deployed node's runtime binds its advertised host, not a wildcard.**
  `--bind` and a spec's `node_bind` default to the ssh host on port 9091, or
  under `--local` to the host of `raft.raft_address` (a wildcard there fails
  with `RaftAdvertisedHostError`). `--bind 0.0.0.0:9091` is the explicit
  opt-in. See `docs/agent-fleet/cli.md#bind-address`.
