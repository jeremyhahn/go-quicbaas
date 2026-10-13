### Added

- **Placement admits a spawn through fleet health and measured capacity
  (go-quicbaas#760, slice B).** A node that is unhealthy, unmeasured or
  stale is not placed on. A node never takes more jobs than its declared
  `max_concurrent_jobs`, however idle its sample looks. A node with no
  measured room for another job is passed over, counting the spawns placed
  since its last sample. What a job costs is recalibrated from the fleet's
  own samples once at least 4 jobs are measured. The embedded node is
  measured by the control plane's host. A refusal is
  `FleetCapacityRefusedError`, naming each node and why; REST answers 503 and
  gRPC `ResourceExhausted`, and the spawn never falls through to a local
  backend. See `docs/agent-fleet/user.md`.
