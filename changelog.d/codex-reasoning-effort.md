### Added

- **`--codex-reasoning-effort`**, defaulting to `medium`. The codex backend
  previously ran at whatever the installed CLI defaulted to, which was stated
  nowhere. Code review is the one role whose output is a GATE: a shallower pass
  does not fail visibly, it approves what it should have caught. The depth a
  review reasons at is now a deployment decision an operator can read and
  change.
