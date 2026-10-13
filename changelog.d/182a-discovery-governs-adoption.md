### Fixed

- **A project's `discovery` mode now governs every SDLC trigger, not only
  review (go-quicbaas#182).** A verified webhook delivery to a `poll` project
  is acknowledged (`202`, `"ignored":"poll"`) and acts on nothing: no poller
  nudge, triage wake, ruling readmission or review. Previously only the review
  dispatch honoured the mode. The triage backstop skips `webhook` projects, as
  the review floor already did. A `webhook` binding with no
  `forge.webhook_secret_ref` is refused at bind time: every delivery would
  fail verification and the mode runs no sweep, so the project would discover
  nothing. `both` with no secret stays legal. The engine's issue poller still
  sweeps every project whatever its mode.
