### Fixed

- **The idle sweep reclaims ordinary job-less sessions (go-quicbaas#843).**
  A spawned session's durable record said `active` from spawn until it
  ended, and the sweep spares an active session, so a job-less session that
  was not kept was never reclaimed however long it sat quiet. The record now
  says `active` only while a turn runs: a session is spawned `idle`, each
  turn marks it `active` and marks it `idle` again when it ends, whether the
  turn succeeded, failed or was cancelled. A closed, errored or resumable
  record keeps its status. Session listings and status now report `idle`
  for an open session with no turn in flight.
