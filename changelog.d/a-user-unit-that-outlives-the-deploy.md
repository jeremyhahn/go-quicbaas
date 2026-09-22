### Fixed

- **A `systemd --user` agent no longer dies with the deploy's SSH
  session.** A per-user systemd manager belongs to its user's login
  sessions unless that user lingers, so `agent deploy --systemd auto` on
  omega reported `Online At: 2026-09-22T01:53:08Z` for an agent that was
  killed the moment the deploy disconnected — every later probe started a
  fresh manager, enrolled, and was stopped ten seconds later, while the
  fleet registry still showed `running` from the last heartbeat and a
  review routed there failed with `connection refused` on port 9091. The
  installer now reads `loginctl show-user <user> -p Linger` after writing
  the unit and enables lingering over `sudo -n` before starting it,
  failing with `LingerNotEnabledError` — naming `sudo loginctl
  enable-linger <user>` — when it cannot. `--wait-online` also verifies
  after closing the SSH session, and requires the agent's own runtime
  port to accept a connection before reporting it online.
