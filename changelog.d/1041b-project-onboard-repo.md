### Added

- **A repository can be onboarded like an existing project (go-quicbaas#1041).**
  `quicbaas sdlc project onboard --repo <repo> --like <project>
  [--max-concurrency N] [--paused]` creates the repository's project from the
  template's (signing key `agent-<repo>-signing`, scope `service`), creates
  its webhook secret, binds it with the template's binding retargeted at the
  repository, provisions its webhook and labels, and checks its signing key,
  forge token, branch protection and workflows. Every step checks before it
  acts and is reported `created`, `unchanged` or `finding`, so a rerun after a
  repair picks up where the last run stopped; a missing forge token or SSH key
  is a finding, never imported. The command exits non-zero while any step is
  a finding. It is REST `POST /api/v1/sdlc/repos/{repo}/onboarding`, gRPC
  `SDLCService.OnboardRepo`, the MCP tool `sdlc_project_onboard` (all
  `sdlc:bind`), and `client.SDLC().OnboardRepo` in the Go SDK on every
  transport.
