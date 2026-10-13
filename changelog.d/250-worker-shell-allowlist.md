### Added

- **Host workers get a generated shell allowlist (go-quicbaas#250).** A new
  `orchestration.agent_shell_allow` list of command prefixes (default empty)
  is rendered at boot, with every `orchestration.verification_commands` entry
  as an exact rule, into `<orchestration.agent_config_dir>/settings.json` as
  Claude Code permission rules, so a worker spawned on the daemon host in
  `acceptEdits` mode can run `go`, `gofmt` or `make` instead of parking as
  needs_info. The file is generated: one that differs is overwritten
  atomically (`0600`) with a WARN naming the key, a write failure fails boot,
  and an empty or multi-line entry is refused at config load. Unset
  `agent_config_dir` writes nothing; container workers are unchanged.
