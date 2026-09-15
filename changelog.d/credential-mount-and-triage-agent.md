### Fixed

- **A spawn whose credential directory did not mount is refused instead of
  started blind.** A pooled account's files must be keyed under
  `CredentialDirSpec.Rel` — `materializeAccountFiles` writes each relative path
  verbatim and the backend mounts `<account-home>/<Rel>`. Files stored at the
  root produced no mount, yet the backend still set the CLI's `HomeEnv`,
  pointing it at a container path nothing had mounted. codex answered
  `CODEX_HOME points to "/home/agent/.codex", but that path does not exist`,
  produced no turn, and the runner reported only `(9882 bytes of stderr
  withheld)`. Every review on the burn-in fleet failed that way for hours while
  the account read `active` and the credential resolved.
- **Agent-daemon backends log again.** `ACPBackend.SetLogger` was never called
  in `agent serve`, so every backend's logger was nil and its diagnostics — the
  evidence withheld from the error string — were discarded. That is why the
  fault above took hours to see rather than minutes.

### Added

- **A project may name its own triage agent** (`binding.triage`), falling back
  to `worker` when unset. Triage reads and judges and writes no code, so it can
  run on a cheaper or locally hosted model without changing who writes the
  code. The binding table says plainly when it is falling back.
