### Added

- **Fleet nodes report host metrics on every heartbeat, and the control plane
  judges fleet health from them (go-quicbaas#760, slice A).** The daemon
  samples CPU utilisation, load averages, memory and work-directory disk. Each
  value carries its provenance (`measured` or `derived`), and a value it could
  not read is sent as unread, never as zero. `FleetHealthJudge` in
  `pkg/orchestration` is fail-safe: a node with no sample, an unread reading it
  needs, an invalid sample, or a sample older than the fleet heartbeat timeout
  is unhealthy. A sample is aged by both the control plane's clock and the
  node's clock, and the older of the two ages counts. Admission does not
  consult health yet. See `docs/agent-fleet/user.md`.

### Fixed

- **The gRPC heartbeat no longer drops the node's observation.**
  `FleetNodeHeartbeatRequest` now carries `FleetNodeObservation` (backends,
  capacity, signing agents and relay, images, device telemetry, host metrics).
  `*_reported` flags keep an empty list distinct from an absent one. The Go
  SDK sends the observation on every transport. Its MCP heartbeat now reaches
  `quicbaas_fleet_heartbeat`; it used to call a tool name that does not exist.
