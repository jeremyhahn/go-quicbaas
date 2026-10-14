### Fixed

- **A triage nudge that arrives while the project is already being triaged
  is no longer dropped (go-quicbaas#894).** Previously the nudge lost the
  project's triage lease and was discarded, so an issue opened after the run
  in flight had listed waited for the periodic sweep. The losing nudge now
  leaves one durable rerun mark per project in the tenant's SDLC store
  (`sdlc_triage_rerun`). The run takes the mark before it lists, and again
  after it cleans up; a mark taken at cleanup sends the same job round to
  list again, at most 3 extra times. Past that bound the mark stays for the
  next nudge or sweep. A mark that cannot be written is returned as
  `TriageRerunUnrecordedError`.
