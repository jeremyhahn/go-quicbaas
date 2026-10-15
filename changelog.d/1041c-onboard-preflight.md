### Added

- **Onboarding can gate autonomy on a green preflight (go-quicbaas#1041).**
  `quicbaas sdlc project onboard --preflight` binds the project paused, then
  runs the binding's verification commands once as a `preflight` job: on the
  default branch, in the worker image, through the `container_command` guard,
  with no agent turn and nothing run on the daemon. Autonomy switches on only
  when every command passes; otherwise the `preflight` step is a finding
  naming each failing command and its failing lines, or the command the
  guard refused. With `--paused` a green preflight leaves autonomy off. A
  rerun on an autonomous project runs nothing. It is the `preflight` field
  on REST, gRPC (`OnboardRepoRequest.preflight`), MCP
  (`sdlc_project_onboard`) and `RepoOnboardRequest.Preflight` in the Go SDK
  on every transport.

### Changed

- **A verification command the allowlist refuses no longer fails an
  onboarding.** `project onboard` reports it as a `binding` finding with its
  remedy, and the steps after it wait.
