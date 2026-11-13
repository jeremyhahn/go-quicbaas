### Removed

- `sdlc.verification_commands` is gone (#628). The daemon loaded, defaulted
  and validated it but never read it, so setting it had no effect. A
  configuration that still sets it now fails to load and names its
  replacements: `orchestration.verification_commands` for the command
  allowlist, and the project binding's `verification` list for the commands
  a project runs.
